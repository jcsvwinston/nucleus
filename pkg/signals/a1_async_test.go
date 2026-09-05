// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package signals

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// NU-35: an async handler that panics is logged, not fatal, and the number
// of handlers in flight is bounded.
func TestEmitAsync_RecoversAndBounds(t *testing.T) {
	var buf bytes.Buffer
	b := NewBus(slog.New(slog.NewTextHandler(&buf, nil)))
	var ran atomic.Int32
	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < asyncLimit*3; i++ {
		wg.Add(1)
		b.On(PostSave, func(Event) error {
			defer wg.Done()
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inFlight.Add(-1)
			ran.Add(1)
			return nil
		})
	}
	wg.Add(1)
	b.On(PostSave, func(Event) error { defer wg.Done(); panic("boom") })

	b.EmitAsync(Event{Signal: PostSave, ModelName: "Thing"})
	wg.Wait()
	if int(ran.Load()) != asyncLimit*3 {
		t.Fatalf("%d handlers ran, want %d", ran.Load(), asyncLimit*3)
	}
	if peak.Load() > asyncLimit {
		t.Fatalf("%d handlers in flight at once, limit is %d", peak.Load(), asyncLimit)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), "panicked") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "panicked") {
		t.Fatalf("panicking handler not logged: %s", buf.String())
	}
}
