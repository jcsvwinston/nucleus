package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// With sessions in memory the list is per process, and the application says
// so rather than letting an operator believe a revoked token is dead
// everywhere.
func TestWireTokenRevocation_MemorySessionsFallBackAndWarn(t *testing.T) {
	jwtMgr := auth.NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "apptest")
	sm := auth.NewSessionManager(auth.SessionConfig{})

	if err := wireTokenRevocation(jwtMgr, sm, nil); err != nil {
		t.Fatalf("wire: %v", err)
	}

	token, err := jwtMgr.Generate("1", "ana", "editor")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := jwtMgr.Revoke(context.Background(), token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := jwtMgr.ValidateContext(context.Background(), token); err == nil {
		t.Fatal("the revoked token still validates")
	}
}

// A shared session store is reused, so revocation reaches every replica
// that reads it — without asking the operator for a second Redis.
func TestWireTokenRevocation_ReusesTheSharedSessionStore(t *testing.T) {
	shared := newSharedStoreForTest()

	first := auth.NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "apptest")
	firstSessions := auth.NewSessionManager(auth.SessionConfig{})
	firstSessions.SetSessionStore(shared)
	if err := wireTokenRevocation(first, firstSessions, nil); err != nil {
		t.Fatalf("wire: %v", err)
	}

	// A second replica: same signing material, same session store, its own
	// manager — which is what two pods behind a load balancer are.
	second := auth.NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "apptest")
	secondSessions := auth.NewSessionManager(auth.SessionConfig{})
	secondSessions.SetSessionStore(shared)
	if err := wireTokenRevocation(second, secondSessions, nil); err != nil {
		t.Fatalf("wire: %v", err)
	}

	token, err := first.Generate("1", "ana", "editor")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := second.ValidateContext(context.Background(), token); err != nil {
		t.Fatalf("the other replica rejected a live token: %v", err)
	}
	if err := first.Revoke(context.Background(), token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := second.ValidateContext(context.Background(), token); err == nil {
		t.Fatal("the other replica still accepts a revoked token")
	}
}

// --- a session store that lives in this process, standing in for a shared one

func newSharedStoreForTest() auth.SessionStore {
	return &sharedTestStore{data: map[string][]byte{}, expiry: map[string]time.Time{}}
}

type sharedTestStore struct {
	data   map[string][]byte
	expiry map[string]time.Time
}

func (s *sharedTestStore) Find(token string) ([]byte, bool, error) {
	payload, ok := s.data[token]
	if !ok {
		return nil, false, nil
	}
	if exp, ok := s.expiry[token]; ok && !exp.IsZero() && time.Now().After(exp) {
		delete(s.data, token)
		return nil, false, nil
	}
	return payload, true, nil
}

func (s *sharedTestStore) Commit(token string, payload []byte, expiry time.Time) error {
	s.data[token] = payload
	s.expiry[token] = expiry
	return nil
}

func (s *sharedTestStore) Delete(token string) error {
	delete(s.data, token)
	return nil
}
