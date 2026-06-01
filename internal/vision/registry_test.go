package vision

import (
	"context"
	"strings"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type fakeAnalyzer struct {
	resp AnalyzeResponse
	err  error
}

func (f fakeAnalyzer) Analyze(context.Context, AnalyzeRequest) (AnalyzeResponse, error) {
	return f.resp, f.err
}

func (f fakeAnalyzer) Name() string { return "fake" }

func TestComputeHash(t *testing.T) {
	h1 := ComputeHash("same-data")
	h2 := ComputeHash("same-data")
	h3 := ComputeHash("different-data")

	if h1 != h2 {
		t.Fatal("same data should produce same hash")
	}
	if h1 == h3 {
		t.Fatal("different data should produce different hash")
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d", len(h1))
	}
}

func TestSessionStoreGetOrCreateEntry(t *testing.T) {
	s := &SessionStore{entries: make(map[ImageHash]*ImageEntry)}
	h1 := ComputeHash("img1")
	h2 := ComputeHash("img2")

	e1 := s.GetOrCreateEntry(h1, 1, 10)
	if e1.StableOrdinal != 1 {
		t.Fatalf("expected ordinal 1, got %d", e1.StableOrdinal)
	}

	// Same hash returns existing entry
	e1again := s.GetOrCreateEntry(h1, 1, 10)
	if e1again != e1 {
		t.Fatal("expected same entry for same hash")
	}

	e2 := s.GetOrCreateEntry(h2, 2, 10)
	if e2.StableOrdinal != 2 {
		t.Fatalf("expected ordinal 2, got %d", e2.StableOrdinal)
	}

	all := s.AllEntries()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
}

func TestSessionStoreUpdateEntry(t *testing.T) {
	s := &SessionStore{entries: make(map[ImageHash]*ImageEntry)}
	h := ComputeHash("test")
	s.GetOrCreateEntry(h, 1, 10)

	s.UpdateEntry(h, func(e *ImageEntry) {
		e.Summary.Summary = "test summary"
		e.Revision = 1
	})

	e := s.GetEntry(h)
	if e.Summary.Summary != "test summary" {
		t.Fatalf("summary = %q, want test summary", e.Summary.Summary)
	}
	if e.Revision != 2 {
		t.Fatalf("revision = %d, want 2 (fn sets 1, then ++)", e.Revision)
	}
}

func TestLRUEviction(t *testing.T) {
	s := &SessionStore{entries: make(map[ImageHash]*ImageEntry)}
	// Create 5 entries with max 3
	h := make([]ImageHash, 5)
	for i := 0; i < 5; i++ {
		h[i] = ComputeHash(string(rune('0' + i)))
		s.GetOrCreateEntry(h[i], i+1, 3)
	}

	// Only most recent 3 should survive
	all := s.AllEntries()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries (LRU evicted 2), got %d", len(all))
	}

	// After evicting, ordinals should re-distribute
	for _, e := range all {
		if e.StableOrdinal < 3 {
			t.Fatalf("entry with ordinal %d should have been evicted", e.StableOrdinal)
		}
	}
}

func TestGlobalRegistrySessionGC(t *testing.T) {
	r := newGlobalRegistry(GlobalConfig{
		MaxSessions:          100,
		MaxEntriesPerSession: 10,
		SessionTTL:           10 * time.Millisecond,
	})

	s1 := r.GetOrCreateSession("session-1")
	s2 := r.GetOrCreateSession("session-2")
	if r.GetSession("session-1") == nil {
		t.Fatal("expected session-1 to exist")
	}

	// Touch session-1 to keep it alive
	_ = s1
	_ = s2

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)
	r.gc()

	if r.GetSession("session-1") != nil {
		t.Fatal("expected session-1 to be GC'd after TTL")
	}
}

func TestGlobalEvictOldestSession(t *testing.T) {
	r := newGlobalRegistry(GlobalConfig{
		MaxSessions: 2,
	})
	r.GetOrCreateSession("sess-a")
	r.GetOrCreateSession("sess-b")

	// Create C, which should evict the oldest (A)
	r.GetOrCreateSession("sess-c")

	sessions, _ := r.Stats()
	if sessions != 2 {
		t.Fatalf("expected 2 sessions after eviction, got %d", sessions)
	}

	// Only B and C should exist
	if r.GetSession("sess-a") != nil {
		t.Fatal("expected sess-a to be evicted")
	}
	if r.GetSession("sess-b") == nil {
		t.Fatal("expected sess-b to exist")
	}
}

func TestWalkPayloadMessages(t *testing.T) {
	payload := []byte(`{
		"model": "deepseek-v4-flash",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hello"}]},
			{"role": "assistant", "content": "hi"},
			{"role": "user", "content": [{"type": "text", "text": "what is this?"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}]}
		]
	}`)

	walk := WalkPayload(payload)
	if len(walk.Parts) != 1 {
		t.Fatalf("expected 1 image part, got %d", len(walk.Parts))
	}
	if !walk.Parts[0].IsCurrent {
		t.Fatal("expected image to be current (last user message)")
	}
	if walk.Parts[0].Data != "iVBORw0KGgo=" {
		t.Fatalf("data = %q, want iVBORw0KGgo=", walk.Parts[0].Data)
	}
}

func TestWalkPayloadResponses(t *testing.T) {
	payload := []byte(`{
		"model": "deepseek-v4-flash",
		"input": [
			{"role": "user", "content": [{"type": "input_text", "text": "hello"}]},
			{"role": "assistant", "content": [{"type": "output_text", "text": "hi"}]},
			{"role": "user", "content": [{"type": "input_image", "image_url": "data:image/png;base64,iVBOR="}]}
		]
	}`)

	walk := WalkPayload(payload)
	if len(walk.Parts) != 1 {
		t.Fatalf("expected 1 image part, got %d", len(walk.Parts))
	}
	if !walk.Parts[0].IsCurrent {
		t.Fatal("expected image to be current")
	}
	if walk.Parts[0].Data != "iVBOR=" {
		t.Fatalf("data = %q, want iVBOR=", walk.Parts[0].Data)
	}
}

func TestWalkPayloadHistorical(t *testing.T) {
	payload := []byte(`{
		"model": "deepseek-v4-flash",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hello"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,oldImage1="}}]},
			{"role": "assistant", "content": "ok"},
			{"role": "user", "content": "what about that image?"}
		]
	}`)

	walk := WalkPayload(payload)
	if len(walk.Parts) != 1 {
		t.Fatalf("expected 1 image part, got %d", len(walk.Parts))
	}
	if walk.Parts[0].IsCurrent {
		t.Fatal("expected image to be historical (not in last user message)")
	}
	if !walk.HistoricalOnly {
		t.Fatal("expected HistoricalOnly to be true")
	}
}

func TestReplaceImagePart(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc="}}]}]}`)
	ip := ImagePart{ArrayName: "messages", MsgIdx: 0, PartIdx: 0}

	modified, err := ReplaceImagePart(payload, ip, "[Image #1: test]")
	if err != nil {
		t.Fatalf("ReplaceImagePart failed: %v", err)
	}

	// Verify the part was replaced with text
	walk := WalkPayload(modified)
	if len(walk.Parts) != 0 {
		t.Fatal("expected no image parts after replacement")
	}
}

func TestInjectRegistryNote(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"what is this?"}]}]}`)

	modified, err := InjectRegistryNote(payload, "[Image Registry]\nImage #1: test description")
	if err != nil {
		t.Fatalf("InjectRegistryNote failed: %v", err)
	}

	walk := WalkPayload(modified)
	if len(walk.Parts) != 0 {
		t.Fatal("note injection should not add image parts")
	}
}

func TestDetectIntent(t *testing.T) {
	tests := []struct {
		msg   string
		count int
		want  Intent
	}{
		{"好的", 2, IntentNone},
		{"继续", 2, IntentNone},
		{"Image #1 里面有什么", 3, IntentFollowUp},
		{"第一张图里那个按钮是什么", 3, IntentFollowUp},
		{"右边那个弹窗是什么", 2, IntentFollowUp},
		{"第一张和第二张有什么差异", 3, IntentFollowUp},
		{"", 2, IntentNone},
	}

	for _, tt := range tests {
		got := DetectIntent(tt.msg, tt.count)
		if got != tt.want {
			t.Errorf("DetectIntent(%q, %d) = %d, want %d", tt.msg, tt.count, got, tt.want)
		}
	}
}

func TestAmbiguityDetection(t *testing.T) {
	// Multiple historical images + vague question = ambiguous
	msg := "还有什么细节"
	intent := DetectIntent(msg, 3)
	if intent != IntentAmbiguous {
		t.Fatalf("expected IntentAmbiguous for '还有什么细节' with 3 images, got %d", intent)
	}

	// Same question but only 2 images = follow-up (can be specific enough)
	intent = DetectIntent(msg, 2)
	if intent != IntentFollowUp {
		t.Fatalf("expected IntentFollowUp for '还有什么细节' with 2 images, got %d", intent)
	}
}

func TestExtractImageReference(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"Image #1 是什么", 1},
		{"看看 image #2 吧", 2},
		{"第一张图里有什么", 1},
		{"第三张图里的按钮", 3},
		{"随便看看", 0},
		{"没有引用", 0},
	}

	for _, tt := range tests {
		got := ExtractImageReference(tt.msg)
		if got != tt.want {
			t.Errorf("ExtractImageReference(%q) = %d, want %d", tt.msg, got, tt.want)
		}
	}
}

func TestBuildShortPlaceholder(t *testing.T) {
	e := &ImageEntry{StableOrdinal: 2}
	p := BuildShortPlaceholder(e)
	if p != "[Image #2 from previous turn]" {
		t.Fatalf("placeholder = %q, want [Image #2 from previous turn]", p)
	}
}

func TestBuildRegistryNote(t *testing.T) {
	entries := []*ImageEntry{
		{StableOrdinal: 1, Summary: ImageSummary{Summary: "UI design screenshot"}},
		{StableOrdinal: 2, Summary: ImageSummary{Summary: "Error log terminal"}},
	}

	note := BuildRegistryNote(entries, 0)
	if note == "" {
		t.Fatal("expected non-empty registry note")
	}
	if len(note) < 50 {
		t.Fatalf("note too short: %q", note)
	}
}

func TestBuildRegistryNoteTargeted(t *testing.T) {
	entries := []*ImageEntry{
		{StableOrdinal: 1, Summary: ImageSummary{Summary: "UI design"}},
		{StableOrdinal: 2, Summary: ImageSummary{Summary: "Error log"}},
	}

	// Target ordinal 2
	note := BuildRegistryNote(entries, 2)
	if len(note) > 250 {
		t.Fatalf("targeted note too long: got %d chars", len(note))
	}
}

func TestBuildAmbiguityNote(t *testing.T) {
	entries := []*ImageEntry{
		{StableOrdinal: 1, Summary: ImageSummary{}},
		{StableOrdinal: 2, Summary: ImageSummary{}},
		{StableOrdinal: 3, Summary: ImageSummary{}},
	}

	note := BuildAmbiguityNote(entries)
	if len(note) < 60 {
		t.Fatalf("expected ambiguity note to mention 3 images, got %q", note)
	}
}

func TestSessionKeyResolution(t *testing.T) {
	// No session key available
	auth := &cliproxyauth.Auth{ID: "test-auth", Attributes: map[string]string{}}
	opts := cliproxyexecutor.Options{Metadata: map[string]any{}}

	key, ok := ResolveSessionKey(opts, auth)
	if ok {
		t.Fatal("expected false when no session key available")
	}
	if key != "" {
		t.Fatal("expected empty key when no session available")
	}
}

func TestComputeImageHash(t *testing.T) {
	h1 := ComputeHash("data")
	h2 := ComputeHash("data2")
	if h1 == h2 {
		t.Fatal("different data should produce different hashes")
	}
}

func TestProcessorHistoricalReanalysisKeepsReachableState(t *testing.T) {
	registry := newGlobalRegistry(DefaultGlobalConfig())
	processor := &Processor{
		registry: registry,
		analyzer: fakeAnalyzer{resp: AnalyzeResponse{
			Summary: ImageSummary{
				Summary:  "reanalyzed",
				OCRHints: []string{"ocr2"},
			},
		}},
		maxEntries: DefaultGlobalConfig().MaxEntriesPerSession,
	}

	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]},{"role":"assistant","content":"ok"},{"role":"user","content":"第一张图里有什么？"}]}`)

	res, err := processor.Process(context.Background(), payload, SessionKey("sess-1"), 1)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if !strings.Contains(res.RegistryNote, "Image #1: reanalyzed") {
		t.Fatalf("registry note = %q, want reanalyzed summary", res.RegistryNote)
	}
	if strings.Contains(res.RegistryNote, "未出现在当前请求") {
		t.Fatalf("registry note should not claim image is unavailable: %q", res.RegistryNote)
	}

	entries := registry.GetSession("sess-1").AllEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].CurrentPayloadReachable {
		t.Fatal("historical image present in payload should be marked reachable")
	}
}

func TestProcessorWithoutSessionAssignsDistinctEphemeralOrdinals(t *testing.T) {
	processor := &Processor{
		registry:   newGlobalRegistry(DefaultGlobalConfig()),
		analyzer:   fakeAnalyzer{},
		maxEntries: DefaultGlobalConfig().MaxEntriesPerSession,
	}

	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},{"role":"assistant","content":"ok"},{"role":"user","content":[{"type":"text","text":"b"},{"type":"image_url","image_url":{"url":"data:image/png;base64,BBBB"}}]},{"role":"assistant","content":"ok2"},{"role":"user","content":"继续"}]}`)

	res, err := processor.Process(context.Background(), payload, "", 1)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	out := string(res.Payload)
	if strings.Count(out, "[Image #1 from previous turn]") != 1 {
		t.Fatalf("payload should contain exactly one Image #1 placeholder: %s", out)
	}
	if strings.Count(out, "[Image #2 from previous turn]") != 1 {
		t.Fatalf("payload should contain exactly one Image #2 placeholder: %s", out)
	}
}

func TestProcessorDuplicateCurrentImageDoesNotAdvanceOrdinal(t *testing.T) {
	registry := newGlobalRegistry(DefaultGlobalConfig())
	processor := &Processor{
		registry:   registry,
		analyzer:   fakeAnalyzer{},
		maxEntries: DefaultGlobalConfig().MaxEntriesPerSession,
	}

	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`)

	if _, err := processor.Process(context.Background(), payload, SessionKey("sess-dup"), 1); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), payload, SessionKey("sess-dup"), 2); err != nil {
		t.Fatalf("second Process() error = %v", err)
	}

	store := registry.GetSession("sess-dup")
	if store == nil {
		t.Fatal("expected session store to exist")
	}
	if store.nextOrdinal != 1 {
		t.Fatalf("nextOrdinal = %d, want 1 for repeated same image", store.nextOrdinal)
	}
	if len(store.AllEntries()) != 1 {
		t.Fatalf("expected 1 entry for repeated same image, got %d", len(store.AllEntries()))
	}
}

func TestProcessorResetsReachabilityPerRequest(t *testing.T) {
	registry := newGlobalRegistry(DefaultGlobalConfig())
	processor := &Processor{
		registry:   registry,
		analyzer:   fakeAnalyzer{},
		maxEntries: DefaultGlobalConfig().MaxEntriesPerSession,
	}

	payload1 := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`)
	payload2 := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"second"},{"type":"image_url","image_url":{"url":"data:image/png;base64,BBBB"}}]}]}`)
	payload3 := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"first again"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},{"role":"assistant","content":"ok"},{"role":"user","content":"还有什么细节？"}]}`)

	if _, err := processor.Process(context.Background(), payload1, SessionKey("sess-reset"), 1); err != nil {
		t.Fatalf("payload1 Process() error = %v", err)
	}
	if _, err := processor.Process(context.Background(), payload2, SessionKey("sess-reset"), 2); err != nil {
		t.Fatalf("payload2 Process() error = %v", err)
	}
	res, err := processor.Process(context.Background(), payload3, SessionKey("sess-reset"), 3)
	if err != nil {
		t.Fatalf("payload3 Process() error = %v", err)
	}

	if strings.Count(res.RegistryNote, "未出现在当前请求") != 1 {
		t.Fatalf("registry note should mark exactly one older image unavailable: %q", res.RegistryNote)
	}
}
