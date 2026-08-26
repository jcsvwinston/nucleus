// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco A, §2: el store de sesión deja de elegirse con un switch cerrado.
package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeSessionStore struct{ closed bool }

func (f *fakeSessionStore) Delete(string) error                    { return nil }
func (f *fakeSessionStore) Find(string) ([]byte, bool, error)      { return nil, false, nil }
func (f *fakeSessionStore) Commit(string, []byte, time.Time) error { return nil }

func TestRegisterSessionStore_ThirdPartyBackend(t *testing.T) {
	const name = "testkv"
	t.Cleanup(func() { unregisterSessionStoreForTest(name) })

	fake := &fakeSessionStore{}
	if err := RegisterSessionStore(name, func(SessionStoreParams) (SessionStore, func(context.Context) error, error) {
		return fake, func(context.Context) error { fake.closed = true; return nil }, nil
	}); err != nil {
		t.Fatalf("a third-party session store must be registrable: %v", err)
	}

	store, shutdown, err := BuildSessionStore(name, SessionStoreParams{})
	if err != nil {
		t.Fatalf("BuildSessionStore: %v", err)
	}
	if store != SessionStore(fake) {
		t.Fatalf("BuildSessionStore returned %T, want the registered store", store)
	}
	if shutdown == nil {
		t.Fatal("a store holding resources must be able to return a shutdown hook")
	}
	if err := shutdown(context.Background()); err != nil || !fake.closed {
		t.Errorf("the framework must call the shutdown hook: err=%v closed=%v", err, fake.closed)
	}
}

func TestRegisteredSessionStores_IncludesBuiltIns(t *testing.T) {
	got := strings.Join(RegisteredSessionStores(), " ")
	for _, want := range []string{"memory", "sql", "redis"} {
		if !strings.Contains(got, want) {
			t.Errorf("built-in %q must be registered like any other store, got: %s", want, got)
		}
	}
}

// "memory" is expressed as a factory returning a nil store, so the caller
// keeps the manager's in-memory default without a special case.
func TestBuildSessionStore_MemoryKeepsTheDefault(t *testing.T) {
	for _, name := range []string{"memory", ""} {
		store, shutdown, err := BuildSessionStore(name, SessionStoreParams{})
		if err != nil || store != nil || shutdown != nil {
			t.Errorf("%q must resolve to the in-memory default, got store=%v shutdown=%v err=%v", name, store, shutdown != nil, err)
		}
	}
}

func TestBuildSessionStore_UnknownNamesTheKnownOnes(t *testing.T) {
	_, _, err := BuildSessionStore("dynamodb", SessionStoreParams{})
	if err == nil {
		t.Fatal("an unknown store must fail")
	}
	for _, want := range []string{"dynamodb", "memory", "redis"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error must name the unknown store and the registered ones (missing %q): %v", want, err)
		}
	}
}

func TestRegisterSessionStore_Rejects(t *testing.T) {
	noop := func(SessionStoreParams) (SessionStore, func(context.Context) error, error) { return nil, nil, nil }
	if err := RegisterSessionStore("", noop); err == nil {
		t.Error("an empty name must be rejected")
	}
	if err := RegisterSessionStore("nilfactory", nil); err == nil {
		t.Error("a nil factory must be rejected")
	}
	if err := RegisterSessionStore("redis", noop); err == nil {
		t.Error("silently replacing a registered store would make the effective backend depend on import order")
	}
}
