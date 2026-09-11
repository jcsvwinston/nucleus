// Package apikeys is the credential a program uses to call an API: issued
// once, shown once, revocable, scoped, and recognisable in a log.
//
// A framework that only has passwords and browser sessions pushes every
// machine-to-machine caller into one of two bad places — a shared password
// in a config file, or a JWT with no way to revoke it. This package is the
// third option, and it is the one every product ends up needing.
//
// # The shape of a key
//
//	nk_a1b2c3d4_Zm9vYmFyYmF6cXV4...
//	│  │        └ the secret: 256 bits, never stored
//	│  └ the key id: what a listing shows and a log line names
//	└ a fixed prefix, so a leaked key is recognisable in a scan
//
// The prefix is not decoration. Secret scanners match on prefixes, and a
// key that looks like any other base64 string is one nobody can find in a
// repository they just leaked.
package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Prefix is the fixed marker every key carries.
const Prefix = "nk"

// Errors callers distinguish.
var (
	// ErrInvalidKey covers a malformed key, an unknown one, a revoked one
	// and an expired one — one error, because telling them apart tells an
	// attacker which guess was closer.
	ErrInvalidKey = errors.New("apikeys: invalid key")
	// ErrNotFound is for management operations on a key id that does not
	// exist. It is NOT what a failed authentication returns.
	ErrNotFound = errors.New("apikeys: no such key")
)

// Key is an issued credential. SecretHash is what is stored; the secret
// itself exists only in the string returned by Issue.
type Key struct {
	ID          string
	Name        string
	OwnerID     string
	SecretHash  string
	Scopes      []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastUsedAt  time.Time
	RevokedAt   time.Time
	RotatedFrom string
}

// Active reports whether the key may authenticate at this moment.
func (k Key) Active(now time.Time) bool {
	if !k.RevokedAt.IsZero() {
		return false
	}
	if !k.ExpiresAt.IsZero() && now.After(k.ExpiresAt) {
		return false
	}
	return true
}

// HasScope reports whether the key carries a scope. A key with NO scopes
// carries none: an empty list is "this key may do nothing in particular",
// never "this key may do anything". The opposite default is how an
// unscoped key ends up with more power than a scoped one.
func (k Key) HasScope(scope string) bool {
	scope = strings.TrimSpace(scope)
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Store is where keys live.
type Store interface {
	Create(ctx context.Context, key Key) error
	// ByID returns the key regardless of its state; authentication
	// decides what to do about revocation and expiry.
	ByID(ctx context.Context, id string) (Key, error)
	List(ctx context.Context, ownerID string) ([]Key, error)
	Revoke(ctx context.Context, id string, at time.Time) error
	// TouchLastUsed records use. It is called on the hot path, so an
	// implementation is free to sample or batch — nothing depends on it
	// being exact.
	TouchLastUsed(ctx context.Context, id string, at time.Time) error
}

// Issue creates a key and returns BOTH the record and the single string the
// caller must copy now. The secret is not recoverable afterwards: only its
// hash is stored, which is what makes a leaked database not a leak of every
// key.
func Issue(ctx context.Context, store Store, spec Key) (Key, string, error) {
	if store == nil {
		return Key{}, "", errors.New("apikeys: nil store")
	}
	id, err := randomToken(6)
	if err != nil {
		return Key{}, "", err
	}
	secret, err := randomToken(32)
	if err != nil {
		return Key{}, "", err
	}

	key := spec
	key.ID = id
	key.SecretHash = hashSecret(secret)
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	key.Scopes = normalizeScopes(key.Scopes)
	if err := store.Create(ctx, key); err != nil {
		return Key{}, "", fmt.Errorf("apikeys: create: %w", err)
	}
	return key, Prefix + "_" + id + "_" + secret, nil
}

// Authenticate resolves a presented key string.
//
// The comparison is constant-time, and the lookup is by ID — so a wrong
// secret costs one hash, not a scan of every key, and a valid id with a
// wrong secret is indistinguishable from an unknown id.
func Authenticate(ctx context.Context, store Store, presented string) (Key, error) {
	id, secret, err := parse(presented)
	if err != nil {
		return Key{}, err
	}
	key, err := store.ByID(ctx, id)
	if err != nil {
		return Key{}, ErrInvalidKey
	}
	if subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(key.SecretHash)) != 1 {
		return Key{}, ErrInvalidKey
	}
	if !key.Active(time.Now().UTC()) {
		return Key{}, ErrInvalidKey
	}
	return key, nil
}

// Rotate issues a replacement and revokes the original after a grace
// period, so a deployment can roll the new key out before the old one stops
// working. A rotation that breaks the caller at the moment it happens is a
// rotation nobody performs.
func Rotate(ctx context.Context, store Store, id string, grace time.Duration) (Key, string, error) {
	previous, err := store.ByID(ctx, id)
	if err != nil {
		return Key{}, "", err
	}

	replacement, secret, err := Issue(ctx, store, Key{
		Name:        previous.Name,
		OwnerID:     previous.OwnerID,
		Scopes:      previous.Scopes,
		ExpiresAt:   previous.ExpiresAt,
		RotatedFrom: previous.ID,
	})
	if err != nil {
		return Key{}, "", err
	}

	// The old key keeps working for the grace period by being given an
	// expiry rather than a revocation: Active() already enforces it, so
	// there is one rule about when a key stops working.
	previous.ExpiresAt = time.Now().UTC().Add(grace)
	if grace <= 0 {
		previous.RevokedAt = time.Now().UTC()
	}
	if err := store.Create(ctx, previous); err != nil {
		return Key{}, "", fmt.Errorf("apikeys: update the rotated key: %w", err)
	}
	return replacement, secret, nil
}

// parse splits a presented key. It never reports WHICH part was wrong.
//
// SplitN with a limit of three, not Split: the separator is "_" and the
// parts are base32, so the split cannot over-count today — but a future
// alphabet that includes the separator would turn a valid key into an
// invalid one for about one key in ten, intermittently. That is the defect
// the first draft of this file had, with base64url secrets, and it is
// cheaper to make the parser immune than to remember.
func parse(presented string) (id, secret string, err error) {
	parts := strings.SplitN(strings.TrimSpace(presented), "_", 3)
	if len(parts) != 3 || parts[0] != Prefix || parts[1] == "" || parts[2] == "" {
		return "", "", ErrInvalidKey
	}
	return parts[1], parts[2], nil
}

// hashSecret is SHA-256 and not bcrypt, deliberately: the secret carries
// 256 bits of entropy, so there is nothing to brute force, and a per-
// request bcrypt on an API call would be a denial of service its caller
// controls.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// randomToken returns n random bytes in lower-case base32 without padding.
//
// The alphabet matters: base64url contains "_", which is the separator
// between a key's parts, so a secret carrying one used to split into four
// pieces and be rejected — for about one key in ten, at random. Base32 is
// letters and digits only, and it also survives being read aloud, typed
// from a screenshot and pasted by a shell that treats "+" specially.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("apikeys: generate: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)), nil
}

func normalizeScopes(scopes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}
