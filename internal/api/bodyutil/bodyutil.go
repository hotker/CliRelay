package bodyutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const (
	DefaultRequestBodyLimit         int64 = 16 << 20
	DefaultModelBodyLimit           int64 = 128 << 20
	DefaultRequestBodyDiskThreshold int64 = 8 << 20
	ManagementBodyLimit             int64 = 2 << 20
	ConfigYAMLBodyLimit             int64 = 2 << 20
	AuthFileBodyLimit               int64 = 2 << 20
	VertexCredentialBodyLimit       int64 = 2 << 20
)

var ErrBodyTooLarge = errors.New("request body too large")

const requestBodyCacheKey = "cliproxy.request_body_cache"

var modelRequestBodyLimit atomic.Int64
var requestBodyDiskThreshold atomic.Int64

func init() {
	modelRequestBodyLimit.Store(DefaultModelBodyLimit)
	requestBodyDiskThreshold.Store(DefaultRequestBodyDiskThreshold)
}

// SetModelRequestBodyLimit configures the request-body limit used by model API endpoints.
func SetModelRequestBodyLimit(limit int64) {
	if limit <= 0 {
		limit = DefaultModelBodyLimit
	}
	modelRequestBodyLimit.Store(limit)
}

// ModelRequestBodyLimit returns the active request-body limit for model API endpoints.
func ModelRequestBodyLimit() int64 {
	limit := modelRequestBodyLimit.Load()
	if limit <= 0 {
		return DefaultModelBodyLimit
	}
	return limit
}

// SetRequestBodyDiskThreshold configures when reusable request bodies spill to disk.
func SetRequestBodyDiskThreshold(threshold int64) {
	if threshold <= 0 {
		threshold = DefaultRequestBodyDiskThreshold
	}
	requestBodyDiskThreshold.Store(threshold)
}

// RequestBodyDiskThreshold returns the active in-memory threshold for reusable request bodies.
func RequestBodyDiskThreshold() int64 {
	threshold := requestBodyDiskThreshold.Load()
	if threshold <= 0 {
		return DefaultRequestBodyDiskThreshold
	}
	return threshold
}

func normalizeLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultRequestBodyLimit
	}
	return limit
}

func cachedRequestBodyStorage(c *gin.Context) (BodyStorage, bool) {
	if c == nil {
		return nil, false
	}
	bodyVal, ok := c.Get(requestBodyCacheKey)
	if !ok || bodyVal == nil {
		return nil, false
	}
	storage, ok := bodyVal.(BodyStorage)
	return storage, ok && storage != nil
}

func restoreRequestBody(c *gin.Context, storage BodyStorage) error {
	if c == nil || c.Request == nil || storage == nil {
		return nil
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(storage)
	c.Request.ContentLength = storage.Size()
	return nil
}

func setRequestBodyStorage(c *gin.Context, storage BodyStorage) {
	if c == nil || c.Request == nil || storage == nil {
		return
	}
	if previous, ok := cachedRequestBodyStorage(c); ok && previous != storage {
		_ = previous.Close()
	}
	c.Set(requestBodyCacheKey, storage)
	_ = restoreRequestBody(c, storage)
}

// SetRequestBody caches and restores a request body for downstream consumers.
func SetRequestBody(c *gin.Context, body []byte) {
	if c == nil || c.Request == nil {
		return
	}
	setRequestBodyStorage(c, CreateBodyStorage(body))
}

// CachedRequestBody returns a cached request body without reading from the network stream.
func CachedRequestBody(c *gin.Context, maxBytes int64) ([]byte, bool) {
	storage, ok := cachedRequestBodyStorage(c)
	if !ok {
		return nil, false
	}
	if maxBytes > 0 && storage.Size() > maxBytes {
		return nil, false
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, false
	}
	return bytes.Clone(body), true
}

// CleanupRequestBody closes reusable request-body storage after request handling.
func CleanupRequestBody(c *gin.Context) {
	storage, ok := cachedRequestBodyStorage(c)
	if !ok {
		return
	}
	_ = storage.Close()
	c.Set(requestBodyCacheKey, nil)
}

// ReadRequestBody reads and restores an incoming HTTP request body with a strict size limit.
func ReadRequestBody(c *gin.Context, limit int64) ([]byte, error) {
	if c == nil || c.Request == nil {
		return nil, nil
	}

	storage, err := GetRequestBodyStorage(c, limit)
	if err != nil {
		return nil, err
	}
	if storage == nil {
		return nil, nil
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	return body, nil
}

// GetRequestBodyStorage reads and restores an incoming request body as reusable storage.
func GetRequestBodyStorage(c *gin.Context, limit int64) (BodyStorage, error) {
	if c == nil || c.Request == nil {
		return nil, nil
	}

	limit = normalizeLimit(limit)
	if storage, ok := cachedRequestBodyStorage(c); ok {
		if storage.Size() > limit {
			return nil, ErrBodyTooLarge
		}
		if err := restoreRequestBody(c, storage); err != nil {
			return nil, err
		}
		return storage, nil
	}
	if c.Request.Body == nil {
		return nil, nil
	}

	if c.Request.ContentLength > limit {
		return nil, ErrBodyTooLarge
	}
	bodyReader := c.Request.Body
	if c.Writer != nil {
		bodyReader = http.MaxBytesReader(c.Writer, bodyReader, limit)
		c.Request.Body = bodyReader
	}
	storage, err := CreateBodyStorageFromReader(bodyReader, c.Request.ContentLength, limit)
	_ = bodyReader.Close()
	if err != nil {
		return nil, err
	}
	setRequestBodyStorage(c, storage)
	return storage, nil
}

// DecodeJSONRequestBody decodes a reusable JSON body without materializing disk-backed storage.
func DecodeJSONRequestBody(c *gin.Context, limit int64, v any) error {
	storage, err := GetRequestBodyStorage(c, limit)
	if err != nil {
		return err
	}
	if storage == nil {
		return nil
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decoder := json.NewDecoder(storage)
	if err := decoder.Decode(v); err != nil {
		_ = restoreRequestBody(c, storage)
		return err
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		_ = restoreRequestBody(c, storage)
		return err
	}
	return restoreRequestBody(c, storage)
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain a single JSON value")
		}
		return err
	}
	return nil
}

// ReadAll reads from any reader with a strict size limit.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	limit = normalizeLimit(limit)
	limited := &io.LimitedReader{R: r, N: limit + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}

func IsTooLarge(err error) bool {
	if errors.Is(err, ErrBodyTooLarge) {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func IsStorageUnavailable(err error) bool {
	return errors.Is(err, ErrBodyStorageUnavailable)
}

func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var timeoutErr interface{ Timeout() bool }
	return errors.As(err, &timeoutErr) && timeoutErr.Timeout()
}

// LimitBodyMiddleware eagerly reads and restores request bodies with a hard limit.
// It is intended for small management JSON payloads so downstream binders can reuse the body safely.
func LimitBodyMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.Body == nil {
			c.Next()
			return
		}
		if !shouldLimitRequestBody(c.Request) {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit && limit > 0 {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if _, err := ReadRequestBody(c, limit); err != nil {
			if IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		c.Next()
	}
}

func shouldLimitRequestBody(req *http.Request) bool {
	if req == nil || req.Body == nil {
		return false
	}
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Type")))
	return !strings.HasPrefix(contentType, "multipart/form-data")
}
