package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryRevocationStore_RoundTrip(t *testing.T) {
	store := NewMemoryRevocationStore()
	ctx := context.Background()

	if revoked, err := store.Revoked(ctx, "unknown"); err != nil || revoked {
		t.Fatalf("an unknown id must not be revoked (got %v, %v)", revoked, err)
	}
	if err := store.Revoke(ctx, "abc", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked, err := store.Revoked(ctx, "abc"); err != nil || !revoked {
		t.Fatalf("expected abc to be revoked (got %v, %v)", revoked, err)
	}
}

// An entry is held with the token's own expiry, so the list cannot grow
// past the tokens that are still live.
func TestMemoryRevocationStore_ForgetsExpiredEntries(t *testing.T) {
	store := NewMemoryRevocationStore()
	ctx := context.Background()

	if err := store.Revoke(ctx, "old", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked, _ := store.Revoked(ctx, "old"); revoked {
		t.Fatal("an expired revocation is still reported as revoked")
	}
	if err := store.Revoke(ctx, "new", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if n := store.Len(); n != 1 {
		t.Fatalf("expected the expired entry to be purged, %d entries held", n)
	}
}

func TestMemoryRevocationStore_RejectsEmptyID(t *testing.T) {
	if err := NewMemoryRevocationStore().Revoke(context.Background(), "  ", time.Now()); err == nil {
		t.Fatal("an empty id was accepted")
	}
}

// A revocation recorded through a shared session store is visible to every
// node that reads the same store — the reason this adapter exists.
func TestSessionStoreRevocations_SharedAcrossManagers(t *testing.T) {
	shared := newMemorySessionStoreForTest()
	ctx := context.Background()

	nodeA, err := NewSessionStoreRevocations(shared, "")
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	nodeB, err := NewSessionStoreRevocations(shared, "")
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}

	if err := nodeA.Revoke(ctx, "shared-id", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	revoked, err := nodeB.Revoked(ctx, "shared-id")
	if err != nil || !revoked {
		t.Fatalf("the other node did not see the revocation (%v, %v)", revoked, err)
	}
}

// --- the JWT half ---------------------------------------------------------

func TestJWTManager_RevokedTokenIsRefused(t *testing.T) {
	m := NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "authtest")
	m.SetRevocationStore(NewMemoryRevocationStore())
	ctx := context.Background()

	token, err := m.Generate("1", "ana", "editor")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := m.ValidateContext(ctx, token); err != nil {
		t.Fatalf("a fresh token must validate: %v", err)
	}
	if err := m.Revoke(ctx, token); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, err = m.ValidateContext(ctx, token)
	if !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("a revoked token must fail with ErrTokenRevoked, got %v", err)
	}
}

// Revoking one token does not touch another: the list is per token, not per
// user, which is what "this device is lost" needs.
func TestJWTManager_RevocationIsPerToken(t *testing.T) {
	m := NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "authtest")
	m.SetRevocationStore(NewMemoryRevocationStore())
	ctx := context.Background()

	first, _ := m.Generate("1", "ana", "editor")
	second, _ := m.Generate("1", "ana", "editor")
	if first == second {
		t.Fatal("two tokens for the same user came out identical: they cannot be revoked apart")
	}
	if err := m.Revoke(ctx, first); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := m.ValidateContext(ctx, second); err != nil {
		t.Fatalf("the other token was revoked too: %v", err)
	}
}

// A store that cannot answer must not read as "not revoked": that would
// turn an outage into an authentication bypass, silently.
func TestJWTManager_RevocationStoreFailureDeniesTheToken(t *testing.T) {
	m := NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "authtest")
	m.SetRevocationStore(failingRevocations{})
	token, _ := m.Generate("1", "ana", "editor")

	if _, err := m.ValidateContext(context.Background(), token); err == nil {
		t.Fatal("a failing revocation store let the token through")
	}
}

// Without a store there is nowhere to record the decision, and Revoke says
// so rather than reporting success.
func TestJWTManager_RevokeWithoutStoreIsAnError(t *testing.T) {
	m := NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "authtest")
	token, _ := m.Generate("1", "ana", "editor")
	if err := m.Revoke(context.Background(), token); err == nil {
		t.Fatal("Revoke reported success with no revocation store configured")
	}
}

// Revocation runs on a VALIDATED token: otherwise anyone could deny service
// by posting a forged id.
func TestJWTManager_RevokeRefusesAnUnverifiedToken(t *testing.T) {
	m := NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "authtest")
	m.SetRevocationStore(NewMemoryRevocationStore())
	other := NewJWTManager(strings.Repeat("another-secret-xx", 2), time.Hour, "elsewhere")
	foreign, _ := other.Generate("1", "mallory", "admin")

	if err := m.Revoke(context.Background(), foreign); err == nil {
		t.Fatal("a token this manager cannot validate was accepted for revocation")
	}
}

// A validation with no store configured does not reach one — the cheap
// property a bearer token exists for is kept for deployments that want it.
func TestJWTManager_NoStoreMeansNoLookup(t *testing.T) {
	m := NewJWTManager(strings.Repeat("revocation-secret", 2), time.Hour, "authtest")
	token, _ := m.Generate("1", "ana", "editor")
	if _, err := m.Validate(token); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

type failingRevocations struct{}

func (failingRevocations) Revoke(context.Context, string, time.Time) error { return nil }
func (failingRevocations) Revoked(context.Context, string) (bool, error) {
	return false, errors.New("store unreachable")
}

// newMemorySessionStoreForTest returns a session store that lives in this
// process: the shape of a shared store (Redis, SQL) without the dependency.
func newMemorySessionStoreForTest() SessionStore {
	return &testMemoryStore{data: map[string]storedEntry{}}
}

type storedEntry struct {
	payload []byte
	expiry  time.Time
}

type testMemoryStore struct {
	mu   sync.Mutex
	data map[string]storedEntry
}

func (s *testMemoryStore) Find(token string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[token]
	if !ok {
		return nil, false, nil
	}
	if !entry.expiry.IsZero() && time.Now().After(entry.expiry) {
		delete(s.data, token)
		return nil, false, nil
	}
	return entry.payload, true, nil
}

func (s *testMemoryStore) Commit(token string, payload []byte, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[token] = storedEntry{payload: payload, expiry: expiry}
	return nil
}

func (s *testMemoryStore) Delete(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, token)
	return nil
}
