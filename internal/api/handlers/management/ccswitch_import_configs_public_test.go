package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestPublicCcSwitchImportConfigsFiltersByAPIKeyPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPermissionProfilesTestDB(t)

	if err := usage.ReplaceAllCcSwitchImportConfigs([]usage.CcSwitchImportConfigRow{
		{
			ID:                   "claude-1",
			ClientType:           "claude",
			ProviderName:         "Claude Code A",
			DefaultModel:         "kimi-k2.6",
			AllowedChannelGroups: []string{"claude-code"},
			EndpointPath:         "/anthropic",
			UsageAutoInterval:    30,
			APIKeyField:          "ANTHROPIC_AUTH_TOKEN",
		},
		{
			ID:                   "claude-2",
			ClientType:           "claude",
			ProviderName:         "Claude Code B",
			DefaultModel:         "kimi-k2.6",
			AllowedChannelGroups: []string{"claude-code"},
			EndpointPath:         "/anthropic",
			UsageAutoInterval:    30,
			APIKeyField:          "ANTHROPIC_AUTH_TOKEN",
		},
		{
			ID:                   "codex-1",
			ClientType:           "codex",
			ProviderName:         "Codex A",
			DefaultModel:         "gpt-5.2",
			AllowedChannelGroups: []string{"codex"},
			EndpointPath:         "/openai",
			UsageAutoInterval:    30,
		},
	}); err != nil {
		t.Fatalf("ReplaceAllCcSwitchImportConfigs: %v", err)
	}

	const apiKey = "sk-nyw1boql0aetf5o7dl2srrf9ciftgd95"
	if err := usage.ReplaceAllAPIKeys([]usage.APIKeyRow{
		{
			Key:                  apiKey,
			Name:                 "test",
			AllowedChannelGroups: []string{"claude-code"},
		},
	}); err != nil {
		t.Fatalf("ReplaceAllAPIKeys: %v", err)
	}

	body := []byte(`{"api_key":"` + apiKey + `"}`)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/public/ccswitch-import-configs", bytes.NewReader(body))

	h := NewHandler(&config.Config{}, "", nil)
	h.GetPublicCcSwitchImportConfigs(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Items []usage.CcSwitchImportConfigRow `json:"items"`
		Found bool                            `json:"found"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !got.Found {
		t.Fatalf("found = false, want true")
	}
	if len(got.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(got.Items))
	}
	if got.Items[0].ClientType != "claude" || got.Items[1].ClientType != "claude" {
		t.Fatalf("items = %#v, want claude only", got.Items)
	}
}

func TestPublicCcSwitchImportConfigsReturnsEmptyWhenKeyUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPermissionProfilesTestDB(t)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/public/ccswitch-import-configs", bytes.NewReader([]byte(`{"api_key":"sk-unknown"}`)))

	h := NewHandler(&config.Config{}, "", nil)
	h.GetPublicCcSwitchImportConfigs(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Items []usage.CcSwitchImportConfigRow `json:"items"`
		Found bool                            `json:"found"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Found {
		t.Fatalf("found = true, want false")
	}
	if len(got.Items) != 0 {
		t.Fatalf("items len = %d, want 0", len(got.Items))
	}
}
