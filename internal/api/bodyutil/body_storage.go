package bodyutil

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	bodyStorageCacheDirName = "clirelay-request-body-cache"
	bodyStorageTempPattern  = "clirelay-request-body-*.tmp"
	bodyStorageMinFreeBytes = 16 << 20
)

var ErrBodyStorageClosed = os.ErrClosed
var ErrBodyStorageUnavailable = errors.New("request body disk cache unavailable")

var requestBodyCacheDir atomic.Value

var requestBodyStorageStats struct {
	activeMemoryBytes atomic.Int64
	activeDiskBytes   atomic.Int64
	activeDiskFiles   atomic.Int64
	memoryCreates     atomic.Int64
	diskCreates       atomic.Int64
}

// RequestBodyStorageStats snapshots active reusable body storage.
type RequestBodyStorageStats struct {
	ActiveMemoryBytes int64
	ActiveDiskBytes   int64
	ActiveDiskFiles   int64
	MemoryCreates     int64
	DiskCreates       int64
}

// BodyStorage keeps request bodies reusable without forcing large payloads to
// stay resident in memory for the rest of the request.
type BodyStorage interface {
	io.ReadSeeker
	io.Closer
	Bytes() ([]byte, error)
	Size() int64
	IsDisk() bool
}

type memoryBodyStorage struct {
	mu     sync.Mutex
	data   []byte
	reader *bytes.Reader
	size   int64
	closed bool
}

func newMemoryBodyStorage(data []byte) *memoryBodyStorage {
	size := int64(len(data))
	requestBodyStorageStats.activeMemoryBytes.Add(size)
	requestBodyStorageStats.memoryCreates.Add(1)
	return &memoryBodyStorage{
		data:   data,
		reader: bytes.NewReader(data),
		size:   size,
	}
}

func (s *memoryBodyStorage) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrBodyStorageClosed
	}
	return s.reader.Read(p)
}

func (s *memoryBodyStorage) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrBodyStorageClosed
	}
	return s.reader.Seek(offset, whence)
}

func (s *memoryBodyStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	requestBodyStorageStats.activeMemoryBytes.Add(-s.size)
	s.data = nil
	s.reader = bytes.NewReader(nil)
	return nil
}

func (s *memoryBodyStorage) Bytes() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrBodyStorageClosed
	}
	return s.data, nil
}

func (s *memoryBodyStorage) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	return s.size
}

func (s *memoryBodyStorage) IsDisk() bool {
	return false
}

type diskBodyStorage struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	size   int64
	closed bool
}

func newDiskBodyStorage(data []byte) (*diskBodyStorage, error) {
	file, path, err := createDiskBodyStorageFile(int64(len(data)))
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	requestBodyStorageStats.activeDiskBytes.Add(int64(len(data)))
	requestBodyStorageStats.activeDiskFiles.Add(1)
	requestBodyStorageStats.diskCreates.Add(1)
	return &diskBodyStorage{file: file, path: path, size: int64(len(data))}, nil
}

func createDiskBodyStorageFile(requiredBytes int64) (*os.File, string, error) {
	if requiredBytes <= 0 {
		requiredBytes = RequestBodyDiskThreshold()
	}
	if !RequestBodyCacheAvailable(requiredBytes) {
		return nil, "", ErrBodyStorageUnavailable
	}
	dir := RequestBodyCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	file, err := os.CreateTemp(dir, bodyStorageTempPattern)
	if err != nil {
		return nil, "", err
	}
	return file, file.Name(), nil
}

func (s *diskBodyStorage) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return 0, ErrBodyStorageClosed
	}
	return s.file.Read(p)
}

func (s *diskBodyStorage) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return 0, ErrBodyStorageClosed
	}
	return s.file.Seek(offset, whence)
}

func (s *diskBodyStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.file != nil {
		err = s.file.Close()
		s.file = nil
	}
	if s.path != "" {
		if removeErr := os.Remove(s.path); removeErr != nil && !os.IsNotExist(removeErr) && err == nil {
			err = removeErr
		}
		s.path = ""
	}
	requestBodyStorageStats.activeDiskBytes.Add(-s.size)
	requestBodyStorageStats.activeDiskFiles.Add(-1)
	return err
}

func (s *diskBodyStorage) Bytes() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return nil, ErrBodyStorageClosed
	}
	current, err := s.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if _, err = s.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data := make([]byte, s.size)
	_, err = io.ReadFull(s.file, data)
	if _, seekErr := s.file.Seek(current, io.SeekStart); seekErr != nil && err == nil {
		err = seekErr
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *diskBodyStorage) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	return s.size
}

func (s *diskBodyStorage) IsDisk() bool {
	return true
}

// RequestBodyCacheDir returns the dedicated directory used for request body temp files.
func RequestBodyCacheDir() string {
	if value := requestBodyCacheDir.Load(); value != nil {
		if dir, ok := value.(string); ok && dir != "" {
			return filepath.Clean(dir)
		}
	}
	return filepath.Join(os.TempDir(), bodyStorageCacheDirName)
}

// SetRequestBodyCacheDir configures the dedicated request-body cache directory.
func SetRequestBodyCacheDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		requestBodyCacheDir.Store("")
		return
	}
	requestBodyCacheDir.Store(filepath.Clean(dir))
}

// ResetRequestBodyCacheDir restores the default request-body cache directory.
func ResetRequestBodyCacheDir() {
	requestBodyCacheDir.Store("")
}

// RequestBodyCacheAvailable reports whether the cache filesystem has room for a body.
func RequestBodyCacheAvailable(requiredBytes int64) bool {
	dir := RequestBodyCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	if requiredBytes < 0 {
		requiredBytes = 0
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return false
	}
	available := int64(stat.Bavail) * int64(stat.Bsize)
	return available-requiredBytes >= bodyStorageMinFreeBytes
}

// CleanupOldRequestBodyCacheFiles removes abandoned request body temp files.
func CleanupOldRequestBodyCacheFiles(maxAge time.Duration) error {
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	if err := cleanupOldBodyFilesInDir(RequestBodyCacheDir(), maxAge); err != nil {
		return err
	}
	return cleanupOldBodyFilesInDir(os.TempDir(), maxAge)
}

func cleanupOldBodyFilesInDir(dir string, maxAge time.Duration) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if matched, _ := filepath.Match(bodyStorageTempPattern, entry.Name()); !matched {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
	return nil
}

// CurrentRequestBodyStorageStats returns active memory/disk usage counters.
func CurrentRequestBodyStorageStats() RequestBodyStorageStats {
	return RequestBodyStorageStats{
		ActiveMemoryBytes: requestBodyStorageStats.activeMemoryBytes.Load(),
		ActiveDiskBytes:   requestBodyStorageStats.activeDiskBytes.Load(),
		ActiveDiskFiles:   requestBodyStorageStats.activeDiskFiles.Load(),
		MemoryCreates:     requestBodyStorageStats.memoryCreates.Load(),
		DiskCreates:       requestBodyStorageStats.diskCreates.Load(),
	}
}

// CreateBodyStorage stores data in memory until it crosses the configured disk
// threshold. Disk creation failure falls back to memory because the caller
// already owns the complete payload.
func CreateBodyStorage(data []byte) BodyStorage {
	if int64(len(data)) >= RequestBodyDiskThreshold() && RequestBodyCacheAvailable(int64(len(data))) {
		if storage, err := newDiskBodyStorage(data); err == nil {
			return storage
		}
	}
	return newMemoryBodyStorage(data)
}

// CreateBodyStorageFromReader streams a body into reusable storage while
// enforcing limit. Large or unknown-length bodies spill to disk as soon as they
// cross the configured threshold.
func CreateBodyStorageFromReader(reader io.Reader, contentLength int64, limit int64) (BodyStorage, error) {
	limit = normalizeLimit(limit)
	threshold := RequestBodyDiskThreshold()
	if threshold <= 0 {
		threshold = DefaultRequestBodyDiskThreshold
	}
	if threshold > limit {
		threshold = limit
	}
	if contentLength > limit {
		return nil, ErrBodyTooLarge
	}
	if contentLength >= threshold && contentLength > 0 {
		return newDiskBodyStorageFromReader(reader, contentLength, limit)
	}

	var buf bytes.Buffer
	var file *os.File
	var path string
	var total int64
	chunk := make([]byte, 32*1024)
	cleanup := func() {
		if file != nil {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}

	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			total += int64(n)
			if total > limit {
				cleanup()
				return nil, ErrBodyTooLarge
			}
			if file == nil && int64(buf.Len()+n) < threshold {
				_, _ = buf.Write(chunk[:n])
			} else {
				if file == nil {
					var err error
					file, path, err = createDiskBodyStorageFile(total)
					if err != nil {
						return nil, err
					}
					if _, err = file.Write(buf.Bytes()); err != nil {
						cleanup()
						return nil, err
					}
					buf.Reset()
				}
				if _, err := file.Write(chunk[:n]); err != nil {
					cleanup()
					return nil, err
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			cleanup()
			return nil, readErr
		}
	}

	if file == nil {
		return newMemoryBodyStorage(buf.Bytes()), nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, err
	}
	requestBodyStorageStats.activeDiskBytes.Add(total)
	requestBodyStorageStats.activeDiskFiles.Add(1)
	requestBodyStorageStats.diskCreates.Add(1)
	return &diskBodyStorage{file: file, path: path, size: total}, nil
}

func newDiskBodyStorageFromReader(reader io.Reader, expectedSize int64, limit int64) (BodyStorage, error) {
	if expectedSize <= 0 {
		expectedSize = limit
	}
	file, path, err := createDiskBodyStorageFile(expectedSize)
	if err != nil {
		return nil, err
	}
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	written, err := io.CopyBuffer(file, limited, make([]byte, 32*1024))
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if written > limit {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, ErrBodyTooLarge
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	requestBodyStorageStats.activeDiskBytes.Add(written)
	requestBodyStorageStats.activeDiskFiles.Add(1)
	requestBodyStorageStats.diskCreates.Add(1)
	return &diskBodyStorage{file: file, path: path, size: written}, nil
}
