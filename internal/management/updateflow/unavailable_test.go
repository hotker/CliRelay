package updateflow

import (
	"errors"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestClassifyUpdaterErrorOnlyFlagsFailuresBeforeAConnection(t *testing.T) {
	get := func(err error) error {
		return &url.Error{Op: "Get", URL: "http://clirelay-updater:8320/v1/status", Err: err}
	}
	// Wrapped the way net.Dialer reports dial failures, including name resolution.
	dial := func(err error) error {
		return get(&net.OpError{Op: "dial", Net: "tcp", Err: err})
	}

	tests := []struct {
		name        string
		err         error
		unavailable bool
	}{
		{name: "name does not resolve", err: dial(&net.DNSError{Err: "no such host", Name: "clirelay-updater", IsNotFound: true}), unavailable: true},
		{name: "nothing listening", err: dial(os.NewSyscallError("connect", syscall.ECONNREFUSED)), unavailable: true},
		{name: "no host behind the address", err: dial(os.NewSyscallError("connect", syscall.EHOSTUNREACH)), unavailable: true},
		{name: "dial timed out", err: dial(os.ErrDeadlineExceeded), unavailable: true},
		{name: "connection reset mid-response", err: get(&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)})},
		{name: "updater stalled before headers", err: get(errors.New("net/http: timeout awaiting response headers"))},
		{name: "client timeout hides the phase", err: get(errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"))},
		{name: "updater answered with an error", err: errors.New("updater status 500: update engine failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyUpdaterError(tt.err)
			if errors.Is(got, ErrUpdaterUnavailable) != tt.unavailable {
				t.Fatalf("classifyUpdaterError(%v) unavailable = %v, want %v", tt.err, !tt.unavailable, tt.unavailable)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("classifyUpdaterError(%v) = %v, lost the original error", tt.err, got)
			}
		})
	}
}
