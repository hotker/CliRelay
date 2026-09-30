// Package codexclientver answers which Codex CLI version a ChatGPT request
// must present.
//
// ChatGPT stamps every manifest entry with minimal_client_version. A call whose
// Version header or User-Agent is older than that minimum is rejected with
// "not supported when using Codex with a ChatGPT account", even though the
// same account's manifest lists the model. The version therefore cannot be a
// constant compiled into this binary: the official CLI moves, and each new
// model brings its own minimum. Callers present max(official release, the
// model's own minimum).
package codexclientver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// releaseURL is the official Codex CLI release feed. "latest" excludes
	// prereleases, which is the client a normal install is running.
	releaseURL = "https://api.github.com/repos/openai/codex/releases/latest"
	// refreshEvery bounds how often a process asks GitHub. Discovery and
	// request handling share the cached answer.
	refreshEvery = 30 * time.Minute
	// emergencyFloor is presented until the release feed answers. ChatGPT
	// rejected gpt-6.1-sol for 0.149.1 and for the manifest's own
	// minimal_client_version of 0.153.0, and accepted 0.159.1, which was the
	// stable CLI the day that was checked. The feed replaces this as soon as
	// it answers with a version that is not older.
	emergencyFloor = "0.159.1"
)

// releaseFetcher returns the tag name of the newest stable Codex CLI release.
type releaseFetcher func(ctx context.Context) (string, error)

type memory struct {
	mu        sync.Mutex
	official  string
	fetchedAt time.Time
	mins      map[string]string
}

var (
	state         memory
	fetchRelease  releaseFetcher = fetchGitHubLatest
	releaseClient                = &http.Client{Timeout: 8 * time.Second}
)

// Official returns the newest stable Codex CLI version resolved so far.
// Empty means the release feed has not answered in this process.
func Official() string {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.official
}

// Required is the version a request for model must present. It is the newer
// of the official CLI and that model's manifest minimum. An empty model uses
// only the official version. Empty means nothing has been resolved yet, and
// the caller should leave the identity it already built alone.
func Required(model string) string {
	state.mu.Lock()
	base := state.official
	minimum := state.mins[strings.ToLower(strings.TrimSpace(model))]
	state.mu.Unlock()
	if base == "" {
		base = emergencyFloor
	}
	return newer(base, minimum)
}

// NoteMinimum records the manifest minimum for a model. A later, lower value
// does not replace a higher one: a partial manifest must not relax the gate.
func NoteMinimum(model, version string) {
	model = strings.ToLower(strings.TrimSpace(model))
	version = normalizeVersion(version)
	if model == "" || version == "" {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mins == nil {
		state.mins = map[string]string{}
	}
	if prev := state.mins[model]; prev != "" && Compare(version, prev) < 0 {
		return
	}
	state.mins[model] = version
}

// Refresh updates the cached official version when the feed answers with one
// that is not older than what this process already trusts. A failed or empty
// answer leaves the cache as it is.
func Refresh(ctx context.Context) {
	state.mu.Lock()
	if state.official != "" && time.Since(state.fetchedAt) < refreshEvery {
		state.mu.Unlock()
		return
	}
	fetch := fetchRelease
	state.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := fetch(ctx)
	version := normalizeVersion(tag)
	if err != nil || version == "" {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.official != "" && Compare(version, state.official) < 0 {
		state.fetchedAt = time.Now()
		return
	}
	state.official = version
	state.fetchedAt = time.Now()
}

func fetchGitHubLatest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clirelay")
	resp, err := releaseClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errStatus(resp.StatusCode)
	}
	var payload struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.Prerelease {
		return "", nil
	}
	return payload.TagName, nil
}

type errStatus int

func (e errStatus) Error() string {
	return "codex client version: release feed status " + strconv.Itoa(int(e))
}

// Compare reports the numeric order of two dotted versions: -1 if a<b, 0 if
// equal, 1 if a>b. Non-numeric suffixes are ignored.
func Compare(a, b string) int {
	ap := strings.Split(normalizeVersion(a), ".")
	bp := strings.Split(normalizeVersion(b), ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		ai, bi := versionPart(ap, i), versionPart(bp, i)
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

func newer(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" || Compare(a, b) >= 0 {
		return a
	}
	return b
}

// normalizeVersion accepts the shapes the release feed and the manifest use:
// "rust-v0.159.1", "v0.159.1", "0.159.1". Anything that is not a dotted
// number, including alphas, is discarded.
func normalizeVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "alpha") || strings.Contains(lower, "beta") || strings.Contains(lower, "rc") {
		return ""
	}
	if i := strings.LastIndex(lower, "v"); i >= 0 {
		raw = raw[i+1:]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, c := range raw {
		if (c < '0' || c > '9') && c != '.' {
			return ""
		}
	}
	return raw
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

// SetOfficialForTest pins the cached official version and returns a restore func.
func SetOfficialForTest(version string) func() {
	state.mu.Lock()
	prev, at := state.official, state.fetchedAt
	state.official = normalizeVersion(version)
	state.fetchedAt = time.Now()
	state.mu.Unlock()
	return func() {
		state.mu.Lock()
		state.official, state.fetchedAt = prev, at
		state.mu.Unlock()
	}
}

// SetFetcherForTest replaces the release feed and returns a restore func.
func SetFetcherForTest(fetch releaseFetcher) func() {
	state.mu.Lock()
	prev := fetchRelease
	fetchRelease = fetch
	state.fetchedAt = time.Time{}
	state.mu.Unlock()
	return func() {
		state.mu.Lock()
		fetchRelease = prev
		state.mu.Unlock()
	}
}

// ResetForTest clears the cache.
func ResetForTest() {
	state.mu.Lock()
	state.official = ""
	state.fetchedAt = time.Time{}
	state.mins = nil
	state.mu.Unlock()
}
