package imagegeneration

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceStartAndGetLifecycle(t *testing.T) {
	t.Parallel()

	svc := NewService(func(ctx context.Context, tenantID string, payload []byte, alt string) ([]byte, error) {
		return []byte(`{"data":[{"b64_json":"abc"}]}`), nil
	}, "test")

	snapshot := svc.Start("tenant-a", []byte(`{"prompt":"hello"}`), "images/generations")
	if snapshot.ID == "" {
		t.Fatalf("Start() returned empty task id")
	}

	got := waitTaskStatus(t, svc, "tenant-a", snapshot.ID, "succeeded")
	if got.Result == nil {
		t.Fatalf("task result is nil")
	}
}

type fakeStatusError struct {
	code int
	err  error
}

func (e fakeStatusError) Error() string {
	return e.err.Error()
}

func (e fakeStatusError) StatusCode() int {
	return e.code
}

func TestServiceCapturesStatusError(t *testing.T) {
	t.Parallel()

	svc := NewService(func(ctx context.Context, tenantID string, payload []byte, alt string) ([]byte, error) {
		return nil, fakeStatusError{code: 429, err: errors.New("rate limited")}
	}, "test")

	snapshot := svc.Start("tenant-a", []byte(`{"prompt":"hello"}`), "images/generations")
	// Wait for the terminal status update after execute returns; closing a channel
	// inside execute races the status write and can still observe "running".
	got := waitTaskStatus(t, svc, "tenant-a", snapshot.ID, "failed")
	if got.Error == nil {
		t.Fatalf("task error is nil")
	}
	if status, _ := got.Error["status"].(int); status != 429 {
		t.Fatalf("task status code = %v, want 429", got.Error["status"])
	}
}

func TestServiceTaskIsTenantScoped(t *testing.T) {
	t.Parallel()

	svc := NewService(func(_ context.Context, tenantID string, _ []byte, _ string) ([]byte, error) {
		if tenantID != "tenant-a" {
			t.Errorf("tenantID = %q", tenantID)
		}
		return []byte(`{"data":[]}`), nil
	}, "test")

	task := svc.Start("tenant-a", []byte(`{"prompt":"hello"}`), "images/generations")
	_ = waitTaskStatus(t, svc, "tenant-a", task.ID, "succeeded")
	if _, ok := svc.Get("tenant-b", task.ID); ok {
		t.Fatal("tenant B can read tenant A task")
	}
	if _, ok := svc.Get("tenant-a", task.ID); !ok {
		t.Fatal("tenant A task missing")
	}
}

// waitTaskStatus polls until the async task reaches wantStatus or the timeout fires.
// Execute callbacks finish before Service.run writes the terminal status, so tests
// must wait on the stored status rather than a channel closed inside execute.
func waitTaskStatus(t *testing.T, svc *Service, tenantID, taskID, wantStatus string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last Snapshot
	for time.Now().Before(deadline) {
		got, ok := svc.Get(tenantID, taskID)
		if !ok {
			t.Fatalf("Get(%q) returned not found", taskID)
		}
		last = got
		if got.Status == wantStatus {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task status = %q, want %q", last.Status, wantStatus)
	return Snapshot{}
}
