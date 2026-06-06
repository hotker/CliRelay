package claude

import (
	"testing"
	"time"

	internalclaude "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/claude"
)

func TestMetadataFromTokenStorageIncludesRuntimeTokens(t *testing.T) {
	expired := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	lastRefresh := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)

	meta := MetadataFromTokenStorage(&internalclaude.ClaudeTokenStorage{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Email:        "claude@example.com",
		Expire:       expired,
		LastRefresh:  lastRefresh,
	})

	for key, want := range map[string]string{
		"type":          "claude",
		"access_token":  "access-token",
		"refresh_token": "refresh-token",
		"email":         "claude@example.com",
		"expired":       expired,
		"last_refresh":  lastRefresh,
	} {
		if got, _ := meta[key].(string); got != want {
			t.Fatalf("metadata[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestMetadataFromTokenStorageHandlesNilStorage(t *testing.T) {
	meta := MetadataFromTokenStorage(nil)
	if got, _ := meta["type"].(string); got != "claude" {
		t.Fatalf("metadata[type] = %q, want claude", got)
	}
	if len(meta) != 1 {
		t.Fatalf("metadata = %#v, want only type", meta)
	}
}
