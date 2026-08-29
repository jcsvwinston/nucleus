package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// capableStore is a framework SessionStore (Find/Commit/Delete) that ALSO
// carries the optional capabilities a real backend has: enumeration and the
// context-taking variants. Redis and SQL — the two stores shipped — are of
// this shape, which is why the type-erasure below was not academic.
type capableStore struct {
	mu   sync.Mutex
	data map[string][]byte

	sawFindCtx, sawCommitCtx, sawDeleteCtx bool
}

func newCapableStore() *capableStore {
	return &capableStore{data: map[string][]byte{}}
}

func (s *capableStore) Find(token string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[token]
	return b, ok, nil
}

func (s *capableStore) Commit(token string, b []byte, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[token] = b
	return nil
}

func (s *capableStore) Delete(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, token)
	return nil
}

func (s *capableStore) All() (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

func (s *capableStore) AllCtx(context.Context) (map[string][]byte, error) { return s.All() }

func (s *capableStore) FindCtx(_ context.Context, token string) ([]byte, bool, error) {
	s.mu.Lock()
	s.sawFindCtx = true
	s.mu.Unlock()
	return s.Find(token)
}

func (s *capableStore) CommitCtx(_ context.Context, token string, b []byte, e time.Time) error {
	s.mu.Lock()
	s.sawCommitCtx = true
	s.mu.Unlock()
	return s.Commit(token, b, e)
}

func (s *capableStore) DeleteCtx(_ context.Context, token string) error {
	s.mu.Lock()
	s.sawDeleteCtx = true
	s.mu.Unlock()
	return s.Delete(token)
}

// TestSetSessionStore_PreservesOptionalCapabilities is the regression guard
// for a wrapper that silently threw away everything it did not know about.
//
// SetSessionStore — the door pkg/app uses for EVERY configured store, so
// `session_store: redis` and `session_store: sql` both go through it —
// wrapped the store in an adapter carrying exactly three methods. The
// capabilities scs and ActiveSessions discover by type assertion were gone:
//
//	store desnudo:        AllCtx=true  All=true  FindCtx=true
//	tras SetSessionStore: AllCtx=false All=false FindCtx=false
//	ActiveSessions -> len=0  err="auth: session store does not support enumeration"
//
// Three measured consequences: orbit's active-sessions view answered 200
// with "not supported" and zero rows, SCS().Iterate — the escape hatch
// ActiveSessions' own godoc points at — panicked, and every session read and
// write lost the request context and fell back to context.Background().
//
// The test enters through SetSessionStore. The pre-existing enumeration
// tests use the default MemStore or SetStore directly — always the door that
// does NOT wrap — which is why a regression introduced in v1.13.0 went
// unnoticed: `grep -rn ActiveSessions` finds no test in pkg/app either.
func TestSetSessionStore_PreservesOptionalCapabilities(t *testing.T) {
	store := newCapableStore()
	sm := NewSessionManager(SessionConfig{})
	sm.SetSessionStore(store)

	installed := sm.SCS().Store
	if _, ok := installed.(interface {
		AllCtx(context.Context) (map[string][]byte, error)
	}); !ok {
		t.Errorf("the installed store lost AllCtx: %T", installed)
	}
	if _, ok := installed.(interface {
		All() (map[string][]byte, error)
	}); !ok {
		t.Errorf("the installed store lost All: %T", installed)
	}
	if _, ok := installed.(interface {
		FindCtx(context.Context, string) ([]byte, bool, error)
	}); !ok {
		t.Errorf("the installed store lost FindCtx: %T", installed)
	}

	// The capability that matters end to end: the operator can see sessions.
	deadline := time.Now().Add(time.Hour).UTC()
	payload, err := sm.SCS().Codec.Encode(deadline, map[string]any{"user_id": "u1"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := sm.SCS().Store.Commit("tok-1", payload, deadline); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := sm.ActiveSessions(context.Background())
	if err != nil {
		t.Fatalf("ActiveSessions: %v", err)
	}
	if len(got) != 1 || got[0].Token != "tok-1" {
		t.Fatalf("ActiveSessions must enumerate a store installed by SetSessionStore, got %+v", got)
	}

	// The context-taking variants must actually reach the store, or every
	// read and write silently drops the request context.
	if _, _, err := sm.SCS().Store.(interface {
		FindCtx(context.Context, string) ([]byte, bool, error)
	}).FindCtx(context.Background(), "tok-1"); err != nil {
		t.Fatalf("FindCtx: %v", err)
	}
	store.mu.Lock()
	sawFind := store.sawFindCtx
	store.mu.Unlock()
	if !sawFind {
		t.Error("FindCtx did not reach the underlying store; the context is being dropped")
	}
}

// TestSetSessionStore_NonIterableStillReportsNotIterable is the control:
// making the adapter carry the capabilities must not make a store CLAIM one
// it does not have. A backend with no enumeration must still say so —
// with the sentinel, not with a panic and not with an empty list that reads
// as "no sessions".
func TestSetSessionStore_NonIterableStillReportsNotIterable(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	sm.SetSessionStore(bareStore{})

	_, err := sm.ActiveSessions(context.Background())
	if !errors.Is(err, ErrSessionStoreNotIterable) {
		t.Fatalf("want ErrSessionStoreNotIterable for a store without enumeration, got %v", err)
	}
}

// bareStore implements the framework contract and nothing else.
type bareStore struct{}

func (bareStore) Find(string) ([]byte, bool, error)      { return nil, false, nil }
func (bareStore) Commit(string, []byte, time.Time) error { return nil }
func (bareStore) Delete(string) error                    { return nil }
