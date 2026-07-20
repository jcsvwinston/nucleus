package asynqprovider

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// startedManager builds a Manager against a fresh miniredis and returns it
// with a channel that carries Run's return value.
func startedManager(t *testing.T, ctx context.Context) (*Manager, chan error) {
	t.Helper()
	mr := miniredis.RunT(t)
	mgr, err := NewManager(tasks.Config{RedisURL: "redis://" + mr.Addr(), Concurrency: 1}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- mgr.Run(ctx) }()
	// Give the server a moment to reach its processing loop so the test
	// exercises a running worker, not a still-starting one.
	time.Sleep(100 * time.Millisecond)
	return mgr, done
}

// TestManagerRun_ReturnsOnContextCancel is the regression pin for the
// signal-wait bug: Run used asynq's Server.Run, which blocks in an OS-signal
// wait that an external Shutdown does not unblock — so a context-driven
// embedded worker (the framework's module-jobs runtime) could never stop.
// Run must return promptly once ctx is cancelled.
func TestManagerRun_ReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mgr, done := startedManager(t, ctx)
	defer mgr.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after ctx cancel: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return within 15s of ctx cancellation")
	}
}

// TestManagerRun_ReturnsOnClose covers the second stop path: closing the
// manager directly (no ctx cancellation) must also unblock Run.
func TestManagerRun_ReturnsOnClose(t *testing.T) {
	mgr, done := startedManager(t, context.Background())

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after Close: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return within 15s of Close")
	}
}
