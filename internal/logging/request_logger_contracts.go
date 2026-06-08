package logging

import (
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
)

var requestLogID atomic.Uint64

// RequestLogger defines the interface for logging HTTP requests and responses.
type RequestLogger interface {
	LogRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, apiRequest, apiResponse []byte, apiResponseErrors []*interfaces.ErrorMessage, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error
	LogStreamingRequest(url, method string, headers map[string][]string, body []byte, requestID string) (StreamingLogWriter, error)
	IsEnabled() bool
}

// StreamingLogWriter handles real-time logging of streaming response chunks.
type StreamingLogWriter interface {
	WriteChunkAsync(chunk []byte)
	WriteStatus(status int, headers map[string][]string) error
	WriteAPIRequest(apiRequest []byte) error
	WriteAPIResponse(apiResponse []byte) error
	SetFirstChunkTimestamp(timestamp time.Time)
	Close() error
}

// FileRequestLogger implements RequestLogger using file-based storage.
type FileRequestLogger struct {
	enabled           bool
	logsDir           string
	errorLogsMaxFiles int
}

// NewFileRequestLogger creates a new file-based request logger.
func NewFileRequestLogger(enabled bool, logsDir string, configDir string, errorLogsMaxFiles int) *FileRequestLogger {
	if !filepath.IsAbs(logsDir) && configDir != "" {
		logsDir = filepath.Join(configDir, logsDir)
	}
	return &FileRequestLogger{
		enabled:           enabled,
		logsDir:           logsDir,
		errorLogsMaxFiles: errorLogsMaxFiles,
	}
}
