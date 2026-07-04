package executor

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type opencodeGoCacheControlPart struct {
	role  string
	value string
}

func opencodeGoPreserveClaudeCacheControl(translated, source []byte) []byte {
	controls := opencodeGoCollectClaudeCacheControls(source)
	if len(controls) == 0 || !gjson.ValidBytes(translated) {
		return translated
	}

	out := translated
	controlIndex := 0
	messages := gjson.GetBytes(out, "messages")
	if !messages.IsArray() {
		return out
	}
	messages.ForEach(func(messageIndex, message gjson.Result) bool {
		if controlIndex >= len(controls) {
			return false
		}
		role := message.Get("role").String()
		content := message.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(contentIndex, part gjson.Result) bool {
			if controlIndex >= len(controls) {
				return false
			}
			if role != controls[controlIndex].role {
				return true
			}
			if part.Get("cache_control").Exists() || !opencodeGoChatContentPartSupportsCacheControl(part) {
				return true
			}
			path := fmt.Sprintf("messages.%d.content.%d.cache_control", messageIndex.Int(), contentIndex.Int())
			updated, err := sjson.SetRawBytes(out, path, []byte(controls[controlIndex].value))
			if err == nil {
				out = updated
				controlIndex++
			}
			return true
		})
		return true
	})
	return out
}

func opencodeGoCollectClaudeCacheControls(source []byte) []opencodeGoCacheControlPart {
	if !gjson.ValidBytes(source) {
		return nil
	}
	controls := make([]opencodeGoCacheControlPart, 0, 4)
	system := gjson.GetBytes(source, "system")
	if system.IsArray() {
		system.ForEach(func(_, part gjson.Result) bool {
			opencodeGoAppendClaudeCacheControl(&controls, "system", part)
			return true
		})
	}
	messages := gjson.GetBytes(source, "messages")
	if messages.IsArray() {
		messages.ForEach(func(_, message gjson.Result) bool {
			role := message.Get("role").String()
			content := message.Get("content")
			if !content.IsArray() {
				return true
			}
			content.ForEach(func(_, part gjson.Result) bool {
				opencodeGoAppendClaudeCacheControl(&controls, role, part)
				return true
			})
			return true
		})
	}
	return controls
}

func opencodeGoAppendClaudeCacheControl(controls *[]opencodeGoCacheControlPart, role string, part gjson.Result) {
	cacheControl := part.Get("cache_control")
	if role == "" || !cacheControl.Exists() || !opencodeGoClaudePartMapsToChatContent(part) {
		return
	}
	*controls = append(*controls, opencodeGoCacheControlPart{
		role:  role,
		value: cacheControl.Raw,
	})
}

func opencodeGoClaudePartMapsToChatContent(part gjson.Result) bool {
	switch part.Get("type").String() {
	case "text":
		return part.Get("text").String() != ""
	case "image":
		return part.Get("source").Exists() || part.Get("url").String() != ""
	default:
		return false
	}
}

func opencodeGoChatContentPartSupportsCacheControl(part gjson.Result) bool {
	switch part.Get("type").String() {
	case "text", "image_url":
		return true
	default:
		return false
	}
}
