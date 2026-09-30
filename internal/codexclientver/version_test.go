package codexclientver

import (
	"context"
	"testing"
	"time"
)

func TestNormalizeReleaseTag(t *testing.T) {
	if got := normalizeVersion("rust-v0.159.1"); got != "0.159.1" {
		t.Fatalf("tag = %q", got)
	}
	if got := normalizeVersion("rust-v0.161.0-alpha.2"); got != "" {
		t.Fatalf("prerelease tag = %q, want empty", got)
	}
}

func TestRequiredUsesTheNewerOfOfficialAndModelMinimum(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	NoteMinimum("gpt-6.1-sol", "0.153.0")
	restore := SetOfficialForTest("0.159.1")
	t.Cleanup(restore)
	if got := Required("gpt-6.1-sol"); got != "0.159.1" {
		t.Fatalf("required = %q, want official 0.159.1", got)
	}
	NoteMinimum("gpt-6.1-sol", "0.170.0")
	if got := Required("gpt-6.1-sol"); got != "0.170.0" {
		t.Fatalf("required = %q, want the manifest minimum", got)
	}
}

func TestRefreshKeepsANewerCachedVersion(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	restore := SetFetcherForTest(func(context.Context) (string, error) {
		return "rust-v0.140.0", nil
	})
	t.Cleanup(restore)
	SetOfficialForTest("0.159.1")
	// SetOfficialForTest marks the cache fresh, so force another fetch.
	state.mu.Lock()
	state.fetchedAt = state.fetchedAt.Add(-refreshEvery - time.Second)
	state.mu.Unlock()
	Refresh(context.Background())
	if got := Official(); got != "0.159.1" {
		t.Fatalf("official = %q, a stale feed must not roll the client backwards", got)
	}
}
