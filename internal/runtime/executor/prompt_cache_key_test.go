package executor

import (
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

func TestApplySessionPromptCacheKeyScopesMetadataSession(t *testing.T) {
	auth := &cliproxyauth.Auth{ID: "ollama-cloud:apikey:a"}
	opts := cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.SessionStickyMetadataKey: "header:x-session-id:session-1",
	}}
	payload := applySessionPromptCacheKey(
		[]byte(`{"model":"glm-5.2","input":"hi"}`),
		nil,
		auth,
		"glm-5.2",
		opts,
	)
	key := gjson.GetBytes(payload, "prompt_cache_key").String()
	if key == "" {
		t.Fatalf("prompt_cache_key missing: %s", payload)
	}
	if strings.Contains(key, "session-1") {
		t.Fatalf("prompt_cache_key leaked raw session: %q", key)
	}
}

func TestApplySessionPromptCacheKeyKeepsExplicitKey(t *testing.T) {
	opts := cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.SessionStickyMetadataKey: "header:x-session-id:session-1",
	}}
	payload := applySessionPromptCacheKey(
		[]byte(`{"model":"glm-5.2","input":"hi","prompt_cache_key":"client-key"}`),
		nil,
		&cliproxyauth.Auth{ID: "auth-a"},
		"glm-5.2",
		opts,
	)
	if got := gjson.GetBytes(payload, "prompt_cache_key").String(); got != "client-key" {
		t.Fatalf("prompt_cache_key = %q, want client-key; payload=%s", got, payload)
	}
}
