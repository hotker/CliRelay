package bodyutil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func useTempRequestBodyCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetRequestBodyCacheDir(dir)
	t.Cleanup(ResetRequestBodyCacheDir)
	return dir
}

func TestLimitBodyMiddlewareRejectsOversizedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(LimitBodyMiddleware(8))
	r.POST("/management", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/management", bytes.NewReader([]byte(`{"value":"too-large"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
}

func TestLimitBodyMiddlewareRestoresBodyForHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(LimitBodyMiddleware(64))
	r.POST("/management", func(c *gin.Context) {
		body, err := ReadRequestBody(c, 64)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		c.String(http.StatusOK, string(body))
	})

	req := httptest.NewRequest(http.MethodPost, "/management", bytes.NewReader([]byte(`{"value":"ok"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if got := w.Body.String(); got != `{"value":"ok"}` {
		t.Fatalf("unexpected response body: %s", got)
	}
}

func TestLimitBodyMiddlewareRejectsOversizedDeleteRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(LimitBodyMiddleware(8))
	r.DELETE("/management", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodDelete, "/management", bytes.NewReader([]byte(`{"value":"too-large"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
}

func TestReadRequestBodyUsesCacheAndSetRequestBodyRefreshesIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousThreshold := RequestBodyDiskThreshold()
	t.Cleanup(func() { SetRequestBodyDiskThreshold(previousThreshold) })
	SetRequestBodyDiskThreshold(1 << 20)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"value":"original"}`))

	body, err := ReadRequestBody(c, 64)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(body) != `{"value":"original"}` {
		t.Fatalf("body = %s", body)
	}

	c.Request.Body = io.NopCloser(strings.NewReader(`{"value":"mutated-without-cache-refresh"}`))
	body, err = ReadRequestBody(c, 64)
	if err != nil {
		t.Fatalf("unexpected cached read error: %v", err)
	}
	if string(body) != `{"value":"original"}` {
		t.Fatalf("cached body = %s", body)
	}

	SetRequestBody(c, []byte(`{"value":"updated"}`))
	body, err = ReadRequestBody(c, 64)
	if err != nil {
		t.Fatalf("unexpected refreshed read error: %v", err)
	}
	if string(body) != `{"value":"updated"}` {
		t.Fatalf("refreshed body = %s", body)
	}
}

func TestReadRequestBodySpillsLargePayloadToDiskAndCleanupRemovesIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := useTempRequestBodyCache(t)
	previousThreshold := RequestBodyDiskThreshold()
	t.Cleanup(func() { SetRequestBodyDiskThreshold(previousThreshold) })
	SetRequestBodyDiskThreshold(8)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"value":"larger-than-threshold"}`))

	body, err := ReadRequestBody(c, 128)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(body) != `{"value":"larger-than-threshold"}` {
		t.Fatalf("body = %s", body)
	}
	storage, ok := cachedRequestBodyStorage(c)
	if !ok {
		t.Fatal("expected cached body storage")
	}
	if !storage.IsDisk() {
		t.Fatalf("expected disk-backed body storage, got %T", storage)
	}
	diskStorage, ok := storage.(*diskBodyStorage)
	if !ok {
		t.Fatalf("cached storage type = %T, want *diskBodyStorage", storage)
	}
	path := diskStorage.path
	if filepath.Dir(path) != cacheDir {
		t.Fatalf("temp body file dir = %q, want %q", filepath.Dir(path), cacheDir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected temp body file to exist: %v", err)
	}

	CleanupRequestBody(c)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected temp body file to be removed, stat err=%v", err)
	}
	if _, ok := cachedRequestBodyStorage(c); ok {
		t.Fatal("expected body storage cache to be cleared")
	}
}

func TestSetRequestBodyRefreshClosesPreviousDiskStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTempRequestBodyCache(t)
	previousThreshold := RequestBodyDiskThreshold()
	t.Cleanup(func() { SetRequestBodyDiskThreshold(previousThreshold) })
	SetRequestBodyDiskThreshold(8)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"value":"larger-than-threshold"}`))
	if _, err := ReadRequestBody(c, 128); err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	storage, ok := cachedRequestBodyStorage(c)
	if !ok {
		t.Fatal("expected cached body storage")
	}
	diskStorage, ok := storage.(*diskBodyStorage)
	if !ok {
		t.Fatalf("cached storage type = %T, want *diskBodyStorage", storage)
	}
	path := diskStorage.path

	SetRequestBody(c, []byte(`{"value":"small"}`))

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected previous temp body file to be removed, stat err=%v", err)
	}
	body, err := ReadRequestBody(c, 128)
	if err != nil {
		t.Fatalf("unexpected refreshed read error: %v", err)
	}
	if string(body) != `{"value":"small"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestCreateBodyStorageFromReaderSpillsAtThreshold(t *testing.T) {
	useTempRequestBodyCache(t)
	previousThreshold := RequestBodyDiskThreshold()
	t.Cleanup(func() { SetRequestBodyDiskThreshold(previousThreshold) })
	SetRequestBodyDiskThreshold(8)

	storage, err := CreateBodyStorageFromReader(strings.NewReader("12345678"), -1, 16)
	if err != nil {
		t.Fatalf("CreateBodyStorageFromReader: %v", err)
	}
	defer storage.Close()

	if !storage.IsDisk() {
		t.Fatalf("expected payload exactly at threshold to use disk storage, got %T", storage)
	}
	if storage.Size() != 8 {
		t.Fatalf("storage size = %d, want 8", storage.Size())
	}
}

func TestCurrentRequestBodyStorageStatsTracksActiveStorage(t *testing.T) {
	useTempRequestBodyCache(t)
	previousThreshold := RequestBodyDiskThreshold()
	t.Cleanup(func() { SetRequestBodyDiskThreshold(previousThreshold) })
	SetRequestBodyDiskThreshold(8)

	before := CurrentRequestBodyStorageStats()
	memory := CreateBodyStorage([]byte("small"))
	afterMemory := CurrentRequestBodyStorageStats()
	if afterMemory.ActiveMemoryBytes-before.ActiveMemoryBytes != 5 {
		t.Fatalf("active memory delta = %d, want 5", afterMemory.ActiveMemoryBytes-before.ActiveMemoryBytes)
	}

	disk := CreateBodyStorage([]byte("larger-than-threshold"))
	afterDisk := CurrentRequestBodyStorageStats()
	if afterDisk.ActiveDiskFiles-afterMemory.ActiveDiskFiles != 1 {
		t.Fatalf("active disk file delta = %d, want 1", afterDisk.ActiveDiskFiles-afterMemory.ActiveDiskFiles)
	}
	if afterDisk.ActiveDiskBytes-afterMemory.ActiveDiskBytes != int64(len("larger-than-threshold")) {
		t.Fatalf("active disk byte delta = %d", afterDisk.ActiveDiskBytes-afterMemory.ActiveDiskBytes)
	}

	if err := memory.Close(); err != nil {
		t.Fatalf("close memory storage: %v", err)
	}
	if err := disk.Close(); err != nil {
		t.Fatalf("close disk storage: %v", err)
	}
	afterClose := CurrentRequestBodyStorageStats()
	if afterClose.ActiveMemoryBytes != before.ActiveMemoryBytes {
		t.Fatalf("active memory after close = %d, want %d", afterClose.ActiveMemoryBytes, before.ActiveMemoryBytes)
	}
	if afterClose.ActiveDiskFiles != before.ActiveDiskFiles || afterClose.ActiveDiskBytes != before.ActiveDiskBytes {
		t.Fatalf("active disk after close files=%d bytes=%d, want files=%d bytes=%d", afterClose.ActiveDiskFiles, afterClose.ActiveDiskBytes, before.ActiveDiskFiles, before.ActiveDiskBytes)
	}
}

func TestCleanupOldRequestBodyCacheFiles(t *testing.T) {
	cacheDir := useTempRequestBodyCache(t)
	oldPath := filepath.Join(cacheDir, "clirelay-request-body-old.tmp")
	freshPath := filepath.Join(cacheDir, "clirelay-request-body-fresh.tmp")
	otherPath := filepath.Join(cacheDir, "other.tmp")
	for _, path := range []string{oldPath, freshPath, otherPath} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old cache: %v", err)
	}

	if err := CleanupOldRequestBodyCacheFiles(5 * time.Minute); err != nil {
		t.Fatalf("CleanupOldRequestBodyCacheFiles: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old cache file removed, stat err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("expected fresh cache file kept: %v", err)
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("expected non body-cache file kept: %v", err)
	}
}

type noBytesBodyStorage struct {
	*bytes.Reader
	data        []byte
	bytesCalled bool
}

func newNoBytesBodyStorage(data []byte) *noBytesBodyStorage {
	return &noBytesBodyStorage{
		Reader: bytes.NewReader(data),
		data:   data,
	}
}

func (s *noBytesBodyStorage) Close() error { return nil }
func (s *noBytesBodyStorage) Bytes() ([]byte, error) {
	s.bytesCalled = true
	return nil, errors.New("Bytes must not be called")
}
func (s *noBytesBodyStorage) Size() int64  { return int64(len(s.data)) }
func (s *noBytesBodyStorage) IsDisk() bool { return true }

func TestDecodeJSONRequestBodyDoesNotCallBytesForDiskStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"model":"gpt-5.5","input":"large-value","stream":true}`)
	storage := newNoBytesBodyStorage(payload)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(requestBodyCacheKey, storage)

	var decoded struct {
		Model string `json:"model"`
	}
	if err := DecodeJSONRequestBody(c, 1024, &decoded); err != nil {
		t.Fatalf("DecodeJSONRequestBody: %v", err)
	}
	if storage.bytesCalled {
		t.Fatal("DecodeJSONRequestBody called Bytes on disk-backed storage")
	}
	if decoded.Model != "gpt-5.5" {
		t.Fatalf("decoded model = %q", decoded.Model)
	}
	restored, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(restored) != string(payload) {
		t.Fatalf("restored body = %s", restored)
	}
}

func TestModelRequestBodyLimitCanBeConfigured(t *testing.T) {
	previous := ModelRequestBodyLimit()
	t.Cleanup(func() { SetModelRequestBodyLimit(previous) })

	SetModelRequestBodyLimit(32)
	if got := ModelRequestBodyLimit(); got != 32 {
		t.Fatalf("ModelRequestBodyLimit = %d, want 32", got)
	}

	SetModelRequestBodyLimit(0)
	if got := ModelRequestBodyLimit(); got != DefaultModelBodyLimit {
		t.Fatalf("ModelRequestBodyLimit default = %d, want %d", got, DefaultModelBodyLimit)
	}
}

func TestRequestBodyDiskThresholdCanBeConfigured(t *testing.T) {
	previous := RequestBodyDiskThreshold()
	t.Cleanup(func() { SetRequestBodyDiskThreshold(previous) })

	SetRequestBodyDiskThreshold(32)
	if got := RequestBodyDiskThreshold(); got != 32 {
		t.Fatalf("RequestBodyDiskThreshold = %d, want 32", got)
	}

	SetRequestBodyDiskThreshold(0)
	if got := RequestBodyDiskThreshold(); got != DefaultRequestBodyDiskThreshold {
		t.Fatalf("RequestBodyDiskThreshold default = %d, want %d", got, DefaultRequestBodyDiskThreshold)
	}
}
