// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package memoryprovider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// NU-12: the enqueue policy is honoured — retries with backoff, a timeout on
// the handler's context, and an explicit error for a queue that does not
// exist here.
func TestMemoryProvider_HonoursMaxRetryAndTimeout(t *testing.T) {
	m, err := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	var attempts atomic.Int32
	done := make(chan struct{})
	_ = m.HandleFunc("flaky", func(ctx context.Context, task tasks.Task) error {
		if attempts.Add(1) < 3 {
			return errors.New("not yet")
		}
		close(done)
		return nil
	})
	if _, err := m.EnqueueJSONWithPolicy("flaky", nil, tasks.EnqueuePolicy{MaxRetry: 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("handler was not retried to success: %d attempts", attempts.Load())
	}
	if m.Retried() != 2 || m.failed.Load() != 0 {
		t.Fatalf("retried=%d failed=%d, want 2 retries and no failure", m.Retried(), m.failed.Load())
	}

	// Timeout: the handler sees a deadline and its failure is retried
	// MaxRetry times before it counts as failed.
	var timedOut atomic.Int32
	_ = m.HandleFunc("slow", func(ctx context.Context, task tasks.Task) error {
		<-ctx.Done()
		timedOut.Add(1)
		return ctx.Err()
	})
	if _, err := m.EnqueueJSONWithPolicy("slow", nil, tasks.EnqueuePolicy{MaxRetry: 1, Timeout: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.failed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.failed.Load() != 1 || timedOut.Load() != 2 {
		t.Fatalf("timeout: failed=%d attempts=%d, want 1 failure after 2 timed-out attempts", m.failed.Load(), timedOut.Load())
	}

	if _, err := m.EnqueueJSONWithPolicy("flaky", nil, tasks.EnqueuePolicy{Queue: "critical"}); !errors.Is(err, ErrUnsupportedQueue) {
		t.Fatalf("named queue accepted: err=%v", err)
	}
}

func TestScheduler_CountsDroppedTicks(t *testing.T) {
	m, _ := NewManager(tasks.Config{Concurrency: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s, err := NewScheduler(SchedulerConfig{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	// A policy the provider refuses makes every tick a dropped one.
	if _, err := s.RegisterJSON("* * * * * *", "job", nil, tasks.EnqueuePolicy{Queue: "other"}); err != nil {
		t.Fatal(err)
	}
	s.Start()
	defer func() { _ = s.Close() }()
	deadline := time.Now().Add(4 * time.Second)
	for s.Dropped() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if s.Dropped() == 0 {
		t.Fatalf("a tick that could not be enqueued was not counted")
	}
}
