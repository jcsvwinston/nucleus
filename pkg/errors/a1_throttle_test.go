// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// NU-11: throttling keys by error type (a message with an id in it used to
// be a fresh key each time, so nothing throttled and the map grew), sweeps
// expired entries, and SampleRate samples.
func TestErrorHandler_ThrottleByTypeAndSweep(t *testing.T) {
	var buf bytes.Buffer
	h := NewErrorHandler(slog.New(slog.NewTextHandler(&buf, nil)), &ErrorHandlerConfig{
		ThrottleConfig: &ThrottleConfig{RateLimit: 2, Duration: time.Hour},
	})
	for i := 0; i < 10; i++ {
		h.Report(context.Background(), fmt.Errorf("lookup of record %d failed", i))
	}
	if n := logLines(buf.String()); n != 2 {
		t.Fatalf("logged %d of 10 same-type errors, want 2 (the rate limit)", n)
	}
	if len(h.throttleMap) != 1 {
		t.Fatalf("throttle map has %d keys, want 1 (keyed by type, not message)", len(h.throttleMap))
	}
	// Expired entries are swept once the map is large.
	h.throttleMu.Lock()
	for i := 0; i < throttleSweepThreshold+5; i++ {
		h.throttleMap[fmt.Sprintf("stale-%d", i)] = &throttleEntry{count: 1, resetTime: time.Now().Add(-time.Minute)}
	}
	h.throttleMu.Unlock()
	h.Report(context.Background(), fmt.Errorf("another"))
	if len(h.throttleMap) > 3 {
		t.Fatalf("expired entries not swept: %d keys", len(h.throttleMap))
	}
}

// logLines counts the records the handler wrote; the message of a plain
// error is not on the wire (it is reported as INTERNAL_ERROR), so the
// count is what proves throttling.
func logLines(s string) int {
	return len(strings.Split(strings.TrimSpace(s), "\n")) - boolToInt(strings.TrimSpace(s) == "")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestErrorHandler_SampleRateSamples(t *testing.T) {
	var buf bytes.Buffer
	h := NewErrorHandler(slog.New(slog.NewTextHandler(&buf, nil)), &ErrorHandlerConfig{
		ThrottleConfig: &ThrottleConfig{SampleRate: 0.1},
	})
	for i := 0; i < 400; i++ {
		h.Report(context.Background(), fmt.Errorf("sampled"))
	}
	n := logLines(buf.String())
	if n == 0 || n == 400 || n > 120 {
		t.Fatalf("logged %d of 400 with SampleRate 0.1: sampling is not applied", n)
	}
}
