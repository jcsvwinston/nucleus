// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrInvalidCredentials reports that a backend recognised the request and
// rejected it: the user does not exist, or the password does not match.
//
// It is deliberately ONE error for both cases. A backend that distinguishes
// them — a different error, or merely a faster answer — publishes a user
// enumerator, and that is a property of the whole chain, not of one
// backend: the chain stops on this error and moves on to the next backend
// on any other, so an implementation that leaks the distinction leaks it
// for everyone.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrBackendUnavailable reports that a backend could not reach the system
// it authenticates against — the directory is down, the connection timed
// out. It is NOT a rejection: the chain records it and tries the next
// backend, which is what lets a local break-glass account work while the
// directory is unreachable.
var ErrBackendUnavailable = errors.New("auth: backend unavailable")

// Backend authenticates a username and password against one identity
// source: the application's own user table, an LDAP directory, anything
// else someone registers.
//
// Authenticate returns the authenticated user, or ErrInvalidCredentials
// when the backend is sure the answer is no, or ErrBackendUnavailable when
// it could not reach its source. Any other error is treated as unavailable
// and recorded — a backend that fails in an unexpected way must not be
// able to lock everyone out.
type Backend interface {
	// Name identifies the backend in logs and configuration.
	Name() string
	// Authenticate verifies credentials against this identity source.
	Authenticate(ctx context.Context, username, password string) (*User, error)
}

// BackendFactory builds a Backend. Per-backend settings arrive through the
// configuration subtree the framework binds for it.
type BackendFactory func() (Backend, error)

var (
	backendsMu sync.RWMutex
	backends   = map[string]BackendFactory{}
)

// RegisterBackend makes an authentication backend selectable by name from
// configuration.
//
// Same shape as the storage and session-store registries, and the same
// reason for refusing a duplicate name: two packages claiming "ldap" would
// make the effective backend depend on import order.
//
//	package ldapauth
//
//	func init() {
//	    auth.RegisterBackend("ldap", New)
//	}
func RegisterBackend(name string, factory BackendFactory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("auth: backend name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("auth: backend %q: factory cannot be nil", normalized)
	}

	backendsMu.Lock()
	defer backendsMu.Unlock()
	if _, exists := backends[normalized]; exists {
		return fmt.Errorf("auth: backend %q is already registered", normalized)
	}
	backends[normalized] = factory
	return nil
}

// RegisteredBackends returns every selectable backend name, sorted.
func RegisteredBackends() []string {
	backendsMu.RLock()
	defer backendsMu.RUnlock()

	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Chain authenticates against an ORDERED list of backends, stopping at the
// first one that accepts.
//
// The order is the point, and it is why this is a list and not the map the
// backends themselves live in. The deployment everyone actually wants is
// "the directory first, a local account second": when the directory is
// unreachable, somebody still has to be able to get in and fix it. A map
// cannot express that, and a set of independent backends cannot either.
type Chain struct {
	backends []Backend
}

// NewChain builds a chain from registered backend names, in order.
func NewChain(names ...string) (*Chain, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("auth: an authentication chain needs at least one backend (registered: %s)",
			strings.Join(RegisteredBackends(), ", "))
	}

	chain := &Chain{}
	seen := map[string]struct{}{}
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("auth: backend %q appears twice in the chain — order is meaningful, so a repeat is a mistake rather than a no-op", name)
		}
		seen[name] = struct{}{}

		backendsMu.RLock()
		factory, ok := backends[name]
		backendsMu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("auth: unknown authentication backend %q (registered: %s) — register one with auth.RegisterBackend",
				raw, strings.Join(RegisteredBackends(), ", "))
		}
		backend, err := factory()
		if err != nil {
			return nil, fmt.Errorf("auth: building backend %q: %w", name, err)
		}
		chain.backends = append(chain.backends, backend)
	}
	if len(chain.backends) == 0 {
		return nil, fmt.Errorf("auth: an authentication chain needs at least one backend")
	}
	return chain, nil
}

// Names returns the chain's backends in order.
func (c *Chain) Names() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.backends))
	for _, b := range c.backends {
		names = append(names, b.Name())
	}
	return names
}

// Authenticate walks the chain in order and returns the first acceptance.
//
// A rejection moves to the next backend; so does an unavailable one. The
// distinction matters for what comes back when nobody accepts: if every
// backend REJECTED, the credentials are wrong and the caller gets
// ErrInvalidCredentials. If any backend was UNAVAILABLE, the caller gets
// an error saying so — because "wrong password" and "the directory is
// down" are different operational situations, and telling an operator the
// first when the truth is the second sends them hunting in the wrong
// place.
func (c *Chain) Authenticate(ctx context.Context, username, password string) (*User, error) {
	if c == nil || len(c.backends) == 0 {
		return nil, fmt.Errorf("auth: no authentication backends configured")
	}

	var unavailable []string
	for _, backend := range c.backends {
		user, err := backend.Authenticate(ctx, username, password)
		switch {
		case err == nil && user != nil:
			return user, nil
		case errors.Is(err, ErrInvalidCredentials):
			continue
		default:
			// Anything that is not a clean rejection counts as
			// unavailable, including an unexpected error: a backend
			// failing in a way nobody anticipated must not be able to
			// lock every user out of the application.
			unavailable = append(unavailable, backend.Name())
		}
	}

	if len(unavailable) > 0 {
		return nil, fmt.Errorf("%w: no backend accepted the credentials, and %s could not be reached — a rejection here does not prove the credentials are wrong",
			ErrBackendUnavailable, strings.Join(unavailable, ", "))
	}
	return nil, ErrInvalidCredentials
}

// unregisterBackendForTest removes a backend. Test-only.
func unregisterBackendForTest(name string) {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	delete(backends, strings.ToLower(strings.TrimSpace(name)))
}
