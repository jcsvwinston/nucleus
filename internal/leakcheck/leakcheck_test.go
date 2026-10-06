// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package leakcheck

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// A goroutine parked forever is found, with its stack.
func TestFindsAParkedGoroutine(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		parkedForTheTest(stop)
	}()
	t.Cleanup(func() { close(stop); wg.Wait() })

	leaked := Find()
	if len(leaked) == 0 {
		t.Fatal("a goroutine blocked on a channel was not reported")
	}
	if !strings.Contains(strings.Join(leaked, "\n"), "parkedForTheTest") {
		t.Fatalf("the report does not name the leaked goroutine's function:\n%s", strings.Join(leaked, "\n\n"))
	}
}

//go:noinline
func parkedForTheTest(stop <-chan struct{}) { <-stop }

// A goroutine on its way out is not a leak: Find retries before it reports.
func TestToleratesAGoroutineThatIsEnding(t *testing.T) {
	done := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()
	if leaked := Find(); len(leaked) > 0 {
		t.Fatalf("a goroutine that ends within the retries was reported:\n%s", strings.Join(leaked, "\n\n"))
	}
	<-done
}

// The test runner's goroutines are expected. Inside a test, the goroutine
// running TestMain waits in testing.(*T).Run and the test itself is the
// caller; with nothing else started, nothing is reported.
func TestNothingStartedNothingReported(t *testing.T) {
	if leaked := Find(); len(leaked) > 0 {
		t.Fatalf("goroutines reported with none started:\n%s", strings.Join(leaked, "\n\n"))
	}
}

func TestParsesHeaderStateAndFrames(t *testing.T) {
	gs := goroutines()
	if len(gs) == 0 {
		t.Fatal("no goroutines parsed")
	}
	if gs[0].state != "running" {
		t.Fatalf("the caller's state = %q, want running", gs[0].state)
	}
	if !gs[0].has("github.com/jcsvwinston/nucleus/internal/leakcheck.TestParsesHeaderStateAndFrames") {
		t.Fatalf("the caller's frames do not name this test: %v", gs[0].funcs)
	}
}
