package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActiveSessions_MemoryStore(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	scs := sm.SCS()
	deadline := time.Now().Add(time.Hour).UTC()
	payload, err := scs.Codec.Encode(deadline, map[string]any{"user_id": "u1", "role": "admin"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := scs.Store.Commit("tok-1", payload, deadline); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := sm.ActiveSessions(context.Background())
	if err != nil {
		t.Fatalf("ActiveSessions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	s := got[0]
	if s.Token != "tok-1" {
		t.Errorf("Token = %q, want tok-1", s.Token)
	}
	if !s.Deadline.Equal(deadline) {
		t.Errorf("Deadline = %v, want %v", s.Deadline, deadline)
	}
	if s.Values["user_id"] != "u1" || s.Values["role"] != "admin" {
		t.Errorf("Values = %v, want user_id=u1 role=admin", s.Values)
	}
}

// A corrupt payload is skipped, not fatal — one bad entry must not blind the
// operator to the rest.
func TestActiveSessions_SkipsUndecodable(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	scs := sm.SCS()
	deadline := time.Now().Add(time.Hour).UTC()
	good, err := scs.Codec.Encode(deadline, map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := scs.Store.Commit("good", good, deadline); err != nil {
		t.Fatalf("Commit good: %v", err)
	}
	if err := scs.Store.Commit("garbage", []byte("not-a-gob-payload"), deadline); err != nil {
		t.Fatalf("Commit garbage: %v", err)
	}

	got, err := sm.ActiveSessions(context.Background())
	if err != nil {
		t.Fatalf("ActiveSessions: %v", err)
	}
	if len(got) != 1 || got[0].Token != "good" {
		t.Fatalf("want only the decodable session, got %+v", got)
	}
}

// nonIterableStore satisfies scs.Store (Delete/Find/Commit) but implements
// neither All nor AllCtx, exercising the ErrSessionStoreNotIterable branch.
type nonIterableStore struct{}

func (nonIterableStore) Delete(string) error                    { return nil }
func (nonIterableStore) Find(string) ([]byte, bool, error)      { return nil, false, nil }
func (nonIterableStore) Commit(string, []byte, time.Time) error { return nil }

func TestActiveSessions_NotIterable(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	sm.SetStore(nonIterableStore{})
	if _, err := sm.ActiveSessions(context.Background()); !errors.Is(err, ErrSessionStoreNotIterable) {
		t.Fatalf("err = %v, want ErrSessionStoreNotIterable", err)
	}
}

func TestActiveSessions_NilManager(t *testing.T) {
	var sm *SessionManager
	if _, err := sm.ActiveSessions(context.Background()); !errors.Is(err, ErrNilSessionManager) {
		t.Fatalf("err = %v, want ErrNilSessionManager (not a panic)", err)
	}
}

// errBoom is returned by faultyIterableStore.All to exercise the error-wrap path.
var errBoom = errors.New("store boom")

// faultyIterableStore is iterable but its All() fails, so ActiveSessions must
// propagate the error (wrapped) rather than swallow it.
type faultyIterableStore struct{ nonIterableStore }

func (faultyIterableStore) All() (map[string][]byte, error) { return nil, errBoom }

func TestActiveSessions_AllError(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	sm.SetStore(faultyIterableStore{})
	if _, err := sm.ActiveSessions(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want it to wrap errBoom", err)
	}
}
