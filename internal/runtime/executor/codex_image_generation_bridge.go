package executor

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	codexResponsesLiteHeader   = "X-OpenAI-Internal-Codex-Responses-Lite"
	codexResponsesLiteMetadata = "client_metadata.ws_request_header_x_openai_internal_codex_responses_lite"
)

var (
	imageGenToolJSON      = []byte(`{"type":"image_generation","output_format":"png"}`)
	imageGenToolArrayJSON = []byte(`[{"type":"image_generation","output_format":"png"}]`)
)

// maybeEnsureCodexImageGenerationTool injects the Responses-native
// image_generation tool when the Codex OAuth account has the bridge enabled.
//
// Codex Desktop must keep the local image_gen extension path (Images API +
// generated_images disk save). Hosted image_generation returns base64-only
// image_generation_call items that Desktop does not render, so Desktop
// requests never inject and any hosted tool is stripped.
func maybeEnsureCodexImageGenerationTool(body []byte, auth *cliproxyauth.Auth, baseModel string, headers http.Header) []byte {
	if isCodexDesktopClient(headers) {
		return stripHostedImageGenerationTools(body)
	}
	if !codexImageGenerationBridgeEnabled(auth) {
		return body
	}
	return ensureCodexImageGenerationTool(body, baseModel, auth, headers)
}

func isCodexDesktopClient(headers http.Header) bool {
	if headers == nil {
		return false
	}
	originator := strings.ToLower(strings.TrimSpace(headers.Get("Originator")))
	if strings.Contains(originator, "codex desktop") || strings.Contains(originator, "codex_desktop") {
		return true
	}
	ua := strings.ToLower(strings.TrimSpace(headers.Get("User-Agent")))
	return strings.Contains(ua, "codex desktop")
}

// stripHostedImageGenerationTools removes Responses-native image_generation tools
// so the model falls back to the client's local image_gen namespace when present.
func stripHostedImageGenerationTools(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() {
		kept := make([]any, 0, len(tools.Array()))
		removed := false
		for _, tool := range tools.Array() {
			if strings.TrimSpace(tool.Get("type").String()) == "image_generation" {
				removed = true
				continue
			}
			kept = append(kept, tool.Value())
		}
		if removed {
			if len(kept) == 0 {
				body, _ = sjson.DeleteBytes(body, "tools")
			} else {
				body, _ = sjson.SetBytes(body, "tools", kept)
			}
		}
	}
	choiceType := strings.TrimSpace(gjson.GetBytes(body, "tool_choice.type").String())
	if choiceType == "image_generation" {
		body, _ = sjson.SetBytes(body, "tool_choice", "auto")
	}
	return body
}

func codexImageGenerationBridgeEnabled(auth *cliproxyauth.Auth) bool {
	if auth == nil || auth.Metadata == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	enabled, _ := auth.Metadata["codex_image_generation_bridge"].(bool)
	return enabled
}

func ensureCodexImageGenerationTool(body []byte, baseModel string, auth *cliproxyauth.Auth, headers http.Header) []byte {
	if isCodexResponsesLiteRequest(body, headers) {
		return body
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(baseModel)), "spark") {
		return body
	}
	if isCodexFreePlanAuth(auth) {
		return body
	}

	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() {
		body, _ = sjson.SetRawBytes(body, "tools", imageGenToolArrayJSON)
		return body
	}
	for _, tool := range tools.Array() {
		if tool.Get("type").String() == "image_generation" || isImageGenerationFunctionTool(tool) {
			return body
		}
	}
	body, _ = sjson.SetRawBytes(body, "tools.-1", imageGenToolJSON)
	return body
}

func isImageGenerationFunctionTool(tool gjson.Result) bool {
	switch tool.Get("type").String() {
	case "function":
		return tool.Get("name").String() == "image_gen.imagegen"
	case "namespace":
		if tool.Get("name").String() != "image_gen" {
			return false
		}
		nested := tool.Get("tools")
		if !nested.IsArray() {
			return false
		}
		for _, nestedTool := range nested.Array() {
			if nestedTool.Get("type").String() == "function" && nestedTool.Get("name").String() == "imagegen" {
				return true
			}
		}
	}
	return false
}

func isCodexResponsesLiteRequest(body []byte, headers http.Header) bool {
	if headers != nil && strings.EqualFold(strings.TrimSpace(headers.Get(codexResponsesLiteHeader)), "true") {
		return true
	}
	value := gjson.GetBytes(body, codexResponsesLiteMetadata)
	if !value.Exists() {
		return false
	}
	return value.Type == gjson.True || (value.Type == gjson.String && strings.EqualFold(strings.TrimSpace(value.String()), "true"))
}

func isCodexFreePlanAuth(auth *cliproxyauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["plan_type"]), "free")
}

// normalizeCodexImageGenerationCallStatus upgrades image_generation_call items that already
// carry a finished image payload but remain status=generating. Codex Desktop skips rendering
// and local persistence unless status is completed.
func normalizeCodexImageGenerationCallStatus(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	hadSSEPrefix := bytes.HasPrefix(payload, dataTag)
	body := payload
	if hadSSEPrefix {
		body = bytes.TrimSpace(payload[len(dataTag):])
	}
	if !gjson.ValidBytes(body) {
		return payload
	}

	eventType := gjson.GetBytes(body, "type").String()
	switch eventType {
	case "response.output_item.done", "response.output_item.added":
		if !shouldCompleteImageGenerationCall(gjson.GetBytes(body, "item")) {
			return payload
		}
		updated, err := sjson.SetBytes(body, "item.status", "completed")
		if err != nil {
			return payload
		}
		return maybeWrapSSEData(hadSSEPrefix, updated)
	case "response.completed", "response.done":
		output := gjson.GetBytes(body, "response.output")
		if !output.IsArray() {
			return payload
		}
		updated := body
		changed := false
		for index, item := range output.Array() {
			if !shouldCompleteImageGenerationCall(item) {
				continue
			}
			path := "response.output." + strconv.Itoa(index) + ".status"
			next, err := sjson.SetBytes(updated, path, "completed")
			if err != nil {
				return payload
			}
			updated = next
			changed = true
		}
		if !changed {
			return payload
		}
		return maybeWrapSSEData(hadSSEPrefix, updated)
	default:
		return payload
	}
}

func shouldCompleteImageGenerationCall(item gjson.Result) bool {
	if !item.Exists() || !item.IsObject() {
		return false
	}
	if strings.TrimSpace(item.Get("type").String()) != "image_generation_call" {
		return false
	}
	if strings.TrimSpace(item.Get("result").String()) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.Get("status").String())) {
	case "", "generating", "in_progress", "incomplete":
		return true
	default:
		return false
	}
}

func maybeWrapSSEData(hadSSEPrefix bool, body []byte) []byte {
	if !hadSSEPrefix {
		return body
	}
	out := make([]byte, 0, len(dataTag)+1+len(body))
	out = append(out, dataTag...)
	out = append(out, ' ')
	out = append(out, body...)
	return out
}
