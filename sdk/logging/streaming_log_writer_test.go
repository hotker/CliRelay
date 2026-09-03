package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteChunkAsyncCountsDroppedChunksWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	writer := &FileStreamingLogWriter{
		chunkChan: make(chan []byte, 1),
	}
	writer.chunkChan <- []byte("queued")
	writer.WriteChunkAsync([]byte("dropped"))
	if got := writer.droppedChunks.Load(); got != 1 {
		t.Fatalf("droppedChunks = %d, want 1", got)
	}
}

func TestClosePersistsTruncationNoteForDroppedChunks(t *testing.T) {
	t.Parallel()

	logsDir := t.TempDir()
	bodyFile, err := os.CreateTemp(logsDir, "response-body-*.tmp")
	if err != nil {
		t.Fatalf("create temp body: %v", err)
	}
	if _, err := bodyFile.WriteString("partial-body"); err != nil {
		t.Fatalf("write temp body: %v", err)
	}

	logPath := filepath.Join(logsDir, "request.log")
	writer := &FileStreamingLogWriter{
		logFilePath:      logPath,
		url:              "/v1/responses",
		method:           "POST",
		timestamp:        time.Unix(0, 0).UTC(),
		requestHeaders:   map[string][]string{},
		responseBodyPath: bodyFile.Name(),
		responseBodyFile: bodyFile,
		chunkChan:        make(chan []byte, 1),
		closeChan:        make(chan struct{}),
		errorChan:        make(chan error, 1),
		responseStatus:   200,
		statusWritten:    true,
	}
	go writer.asyncWriter()
	writer.NoteDroppedChunks(3)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	text := string(content)
	if !strings.Contains(text, "partial-body") {
		t.Fatalf("log missing kept body:\n%s", text)
	}
	want := StreamingLogTruncationNote(3)
	if !strings.Contains(text, strings.TrimSpace(want)) {
		t.Fatalf("log missing truncation note %q:\n%s", strings.TrimSpace(want), text)
	}
}
