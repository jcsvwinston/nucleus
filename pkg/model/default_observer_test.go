// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"sync"
	"testing"
)

// The process-wide SQL observer used to be a single slot: the second
// caller replaced the first, silently. The framework itself installs one
// from pkg/app to feed the observability bus, so a third party that
// wanted to watch SQL — the one thing ADR-023 says is possible today —
// turned that bus off by doing it, and nothing said so.
//
// This test fabricates the collision: two independent subscribers, both
// of which must receive the event.
func TestDefaultSQLObserver_TwoSubscribersBothReceive(t *testing.T) {
	t.Cleanup(ResetDefaultSQLObservers)
	ResetDefaultSQLObservers()

	var mu sync.Mutex
	var first, second int
	SetDefaultSQLObserver(func(context.Context, SQLQueryEvent) {
		mu.Lock()
		first++
		mu.Unlock()
	})
	SetDefaultSQLObserver(func(context.Context, SQLQueryEvent) {
		mu.Lock()
		second++
		mu.Unlock()
	})

	emitDefaultSQLObservers(context.Background(), SQLQueryEvent{Operation: "select"})

	mu.Lock()
	defer mu.Unlock()
	if first != 1 {
		t.Errorf("the first subscriber received %d events, want 1 — a later subscriber must not silence it", first)
	}
	if second != 1 {
		t.Errorf("the second subscriber received %d events, want 1", second)
	}
}

// Clearing removes every subscriber, which is what a test needs and what
// the nil argument always meant.
func TestDefaultSQLObserver_NilClearsAll(t *testing.T) {
	t.Cleanup(ResetDefaultSQLObservers)
	ResetDefaultSQLObservers()

	var calls int
	SetDefaultSQLObserver(func(context.Context, SQLQueryEvent) { calls++ })
	SetDefaultSQLObserver(nil)
	emitDefaultSQLObservers(context.Background(), SQLQueryEvent{Operation: "select"})
	if calls != 0 {
		t.Errorf("clearing must remove every subscriber, got %d calls", calls)
	}
}

// A subscriber that panics must not take the request down with it, and
// must not stop the ones registered after it: an observer is a bystander,
// and a bystander cannot be allowed to decide whether the query happened.
func TestDefaultSQLObserver_APanickingSubscriberDoesNotStopTheOthers(t *testing.T) {
	t.Cleanup(ResetDefaultSQLObservers)
	ResetDefaultSQLObservers()

	var reached bool
	SetDefaultSQLObserver(func(context.Context, SQLQueryEvent) { panic("boom") })
	SetDefaultSQLObserver(func(context.Context, SQLQueryEvent) { reached = true })

	emitDefaultSQLObservers(context.Background(), SQLQueryEvent{Operation: "select"})

	if !reached {
		t.Error("a subscriber registered after a panicking one must still be called")
	}
}
