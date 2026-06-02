package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestParseOpenCodeGoUsageHTML(t *testing.T) {
	html := `<div data-slot="usage">
		<div>Rolling Usage</div><span>3%</span><span>Resets in 31 minutes</span>
		<div>Weekly Usage</div><span>1%</span><span>Resets in 5 days 16 hours</span>
		<div>Monthly Usage</div><span>0%</span><span>Resets in 29 days 0 hours</span>
	</div>`

	items := parseOpenCodeGoUsageHTML(html)
	if len(items) != 3 {
		t.Fatalf("usage item count = %d, want 3: %+v", len(items), items)
	}
	if items[0].Type != "rolling" || items[0].Percentage != 3 || items[0].ResetsIn != "31 minutes" {
		t.Fatalf("rolling item = %+v", items[0])
	}
	if items[1].Type != "weekly" || items[1].Percentage != 1 || items[1].ResetsIn != "5 days 16 hours" {
		t.Fatalf("weekly item = %+v", items[1])
	}
	if items[2].Type != "monthly" || items[2].Percentage != 0 || items[2].ResetsIn != "29 days 0 hours" {
		t.Fatalf("monthly item = %+v", items[2])
	}
}

func TestNormalizeOpenCodeGoAuthCookie(t *testing.T) {
	tests := map[string]string{
		"token":                        "token",
		" auth=abc123; oc_locale=en ":  "abc123",
		"Cookie: foo=bar; auth=abc; z": "abc",
		"foo=bar; session=not-an-auth": "",
		"token=with-padding":           "token=with-padding",
	}
	for input, want := range tests {
		if got := normalizeOpenCodeGoAuthCookie(input); got != want {
			t.Fatalf("normalizeOpenCodeGoAuthCookie(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQueryOpenCodeGoUsageFetchesDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspace/wrk_test/go" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Cookie"); got != "auth=token; oc_locale=en" {
			t.Fatalf("cookie = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<div data-slot="usage">
			Rolling Usage <strong>12%</strong> Resets in 2 hours
			Weekly Usage <strong>34%</strong> Resets in 4 days
			Monthly Usage <strong>56%</strong> Resets in 20 days
		</div>`))
	}))
	defer upstream.Close()

	prevBaseURL := openCodeGoConsoleBaseURL
	openCodeGoConsoleBaseURL = upstream.URL
	defer func() { openCodeGoConsoleBaseURL = prevBaseURL }()

	h := &Handler{cfg: &config.Config{
		OpenCodeGoKey: []config.OpenCodeGoKey{{
			APIKey:      "sk-go",
			Name:        "OpenCode Go",
			WorkspaceID: "wrk_test",
			AuthCookie:  "auth=token; oc_locale=zh-CN",
		}},
	}}

	body := []byte(`{"index":0}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	var decoded struct {
		WorkspaceID string                `json:"workspace_id"`
		Usage       []openCodeGoUsageItem `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.WorkspaceID != "wrk_test" || len(decoded.Usage) != 3 || decoded.Usage[2].Percentage != 56 {
		t.Fatalf("response = %+v", decoded)
	}
}

func TestQueryOpenCodeGoUsageReportsExpiredCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`Continue with GitHub Continue with Google`))
	}))
	defer upstream.Close()

	prevBaseURL := openCodeGoConsoleBaseURL
	openCodeGoConsoleBaseURL = upstream.URL
	defer func() { openCodeGoConsoleBaseURL = prevBaseURL }()

	h := &Handler{cfg: &config.Config{}}
	body := []byte(`{"workspace-id":"wrk_test","auth-cookie":"token"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/opencode-go-api-key/usage", bytes.NewReader(body))

	h.QueryOpenCodeGoUsage(c)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("invalid or expired")) {
		t.Fatalf("body = %s", w.Body.String())
	}
}
