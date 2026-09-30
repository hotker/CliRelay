package executor

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/codexclientver"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// Presenting the compiled-in 0.149.1 identity is what ChatGPT rejects with
// "not supported when using Codex with a ChatGPT account". The manifest's own
// minimal_client_version for gpt-6.1-sol (0.153.0) is still rejected. A
// current stable CLI version is accepted, so the request has to present that.
func TestCodexRequestPresentsACurrentClient(t *testing.T) {
	codexclientver.ResetForTest()
	t.Cleanup(codexclientver.ResetForTest)
	codexclientver.NoteMinimum("gpt-6.1-sol", "0.153.0")

	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(withCodexRequestModel(req.Context(), "gpt-6.1-sol"))
	applyCodexHeaders(req, &config.Config{}, nil, "token", false)

	if got := req.Header.Get("Version"); got != "0.159.1" {
		t.Fatalf("Version = %q, want the current client floor 0.159.1", got)
	}
	if got := req.Header.Get("User-Agent"); got != "codex_cli_rs/0.159.1 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9" {
		t.Fatalf("User-Agent = %q", got)
	}
}

func TestManifestRecordsMinimalClientVersion(t *testing.T) {
	codexclientver.ResetForTest()
	t.Cleanup(codexclientver.ResetForTest)
	body := []byte(`{"models":[{"slug":"gpt-6.1-sol","minimal_client_version":"0.153.0"}]}`)
	if _, _, ok := parseCodexModelList(body, 1); !ok {
		t.Fatal("manifest did not parse")
	}
	restore := codexclientver.SetOfficialForTest("0.140.0")
	t.Cleanup(restore)
	if got := codexclientver.Required("gpt-6.1-sol"); got != "0.153.0" {
		t.Fatalf("required = %q, want the manifest minimum above an older official client", got)
	}
}
