package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/sessionstore"
)

// ErrTokenRevoked reports a token that validated correctly and has been
// revoked. It is deliberately distinct from a signature or expiry failure:
// a caller that logs "invalid token" for a revoked one cannot tell an
// attack from a sign-out.
var ErrTokenRevoked = errors.New("auth: token has been revoked")

// RevocationStore records the identifiers of tokens that must no longer be
// accepted, until they expire on their own.
//
// A bearer token is valid until it expires, and that is the property that
// makes it cheap: no lookup, no shared state. Revocation buys back the one
// case the property cannot cover — a token that leaked, or a sign-out that
// has to mean something before the expiry — and it buys it at the price of
// a lookup per request. Both halves of that trade are deliberate, so the
// store is opt-in: a deployment that does not set one keeps the cheap
// property, and Validate does not touch it.
//
// An entry is written with the token's OWN expiry, so the store never grows
// beyond the tokens that are still live.
type RevocationStore interface {
	// Revoke records an identifier as revoked until expiresAt.
	Revoke(ctx context.Context, id string, expiresAt time.Time) error
	// Revoked reports whether an identifier has been revoked.
	Revoked(ctx context.Context, id string) (bool, error)
}

// MemoryRevocationStore keeps revocations in this process. It is the right
// store for a single-process deployment and the wrong one for several: a
// token revoked on one node stays valid on the others. Use
// NewSessionStoreRevocations with a shared session store for that.
type MemoryRevocationStore struct {
	mu      sync.RWMutex
	entries map[string]time.Time
}

// NewMemoryRevocationStore returns an in-process revocation store.
func NewMemoryRevocationStore() *MemoryRevocationStore {
	return &MemoryRevocationStore{entries: map[string]time.Time{}}
}

// Revoke implements RevocationStore.
func (s *MemoryRevocationStore) Revoke(_ context.Context, id string, expiresAt time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("auth: cannot revoke an empty token id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now())
	s.entries[id] = expiresAt
	return nil
}

// Revoked implements RevocationStore.
func (s *MemoryRevocationStore) Revoked(_ context.Context, id string) (bool, error) {
	s.mu.RLock()
	expiry, ok := s.entries[id]
	s.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if !expiry.IsZero() && time.Now().After(expiry) {
		s.mu.Lock()
		delete(s.entries, id)
		s.mu.Unlock()
		return false, nil
	}
	return true, nil
}

// Len reports how many revocations are held, live ones only. It exists for
// tests and for an operator endpoint that wants to show the size of the
// list rather than guess at it.
func (s *MemoryRevocationStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now())
	return len(s.entries)
}

func (s *MemoryRevocationStore) purgeLocked(now time.Time) {
	for id, expiry := range s.entries {
		if !expiry.IsZero() && now.After(expiry) {
			delete(s.entries, id)
		}
	}
}

// sessionStoreRevocations records revocations in a session store, so a
// deployment that already runs Redis or SQL sessions gets revocation that
// every node sees, with the expiry the store already enforces.
type sessionStoreRevocations struct {
	store  sessionstore.Store
	prefix string
}

// NewSessionStoreRevocations adapts a session store into a RevocationStore.
// The prefix keeps revocation keys from colliding with session tokens in a
// shared keyspace; it defaults to "revoked:".
func NewSessionStoreRevocations(store SessionStore, prefix string) (RevocationStore, error) {
	if store == nil {
		return nil, errors.New("auth: nil session store")
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = "revoked:"
	}
	return &sessionStoreRevocations{store: store, prefix: prefix}, nil
}

func (s *sessionStoreRevocations) Revoke(_ context.Context, id string, expiresAt time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("auth: cannot revoke an empty token id")
	}
	if expiresAt.IsZero() {
		// A store needs an expiry to forget the entry. Without one the
		// safe reading is "as long as any token could live".
		expiresAt = time.Now().Add(24 * time.Hour)
	}
	payload, err := json.Marshal(map[string]any{"revoked_at": time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("auth: encode revocation: %w", err)
	}
	return s.store.Commit(s.prefix+id, payload, expiresAt)
}

func (s *sessionStoreRevocations) Revoked(_ context.Context, id string) (bool, error) {
	_, found, err := s.store.Find(s.prefix + strings.TrimSpace(id))
	if err != nil {
		return false, fmt.Errorf("auth: read revocation: %w", err)
	}
	return found, nil
}
