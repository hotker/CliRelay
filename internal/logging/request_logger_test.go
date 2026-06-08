package logging

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeForFilenameNormalizesUnsafeCharacters(t *testing.T) {
	t.Parallel()

	logger := &FileRequestLogger{}
	got := logger.sanitizeForFilename("/v1/responses: latest?test")
	if got != "v1-responses-latest-test" {
		t.Fatalf("sanitizeForFilename = %q, want %q", got, "v1-responses-latest-test")
	}
}

func TestDecompressResponseHandlesGzip(t *testing.T) {
	t.Parallel()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("hello gzip")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	logger := &FileRequestLogger{}
	got, err := logger.decompressResponse(map[string][]string{
		"Content-Encoding": {"gzip"},
	}, compressed.Bytes())
	if err != nil {
		t.Fatalf("decompressResponse returned error: %v", err)
	}
	if string(got) != "hello gzip" {
		t.Fatalf("decompressed body = %q, want %q", string(got), "hello gzip")
	}
}

func TestLogRequestWithOptionsCleansUpOldErrorLogs(t *testing.T) {
	t.Parallel()

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(false, logsDir, "", 1)

	for i := 0; i < 2; i++ {
		if err := logger.LogRequestWithOptions(
			"/v1/responses",
			"POST",
			map[string][]string{"Authorization": {"Bearer secret-token-value"}},
			[]byte("request"),
			500,
			nil,
			[]byte("response"),
			nil,
			nil,
			nil,
			true,
			"",
			timeZero(),
			timeZero(),
		); err != nil {
			t.Fatalf("LogRequestWithOptions() error = %v", err)
		}
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", logsDir, err)
	}

	var errorLogs []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "error-") && filepath.Ext(entry.Name()) == ".log" {
			errorLogs = append(errorLogs, entry.Name())
		}
	}
	if len(errorLogs) != 1 {
		t.Fatalf("error log files = %d, want 1; files=%v", len(errorLogs), errorLogs)
	}
}

func timeZero() time.Time {
	return time.Time{}
}
