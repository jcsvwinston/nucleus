// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// The contract a third-party backend implements now lives in
// pkg/auth/backend, a leaf package that links ONE third-party package
// instead of the 115 this one drags in (sessions, JWT, Redis, Prometheus,
// OpenTelemetry). None of those are needed to answer "do these credentials
// belong to a real user", and every one of them was a dependency the
// author of a plugin inherited without asking.
//
// The names below are ALIASES, not copies: auth.User and backend.User are
// the same type, auth.ErrInvalidCredentials is the same error value, so
// everything that compiled before still compiles and errors.Is still
// matches across the boundary. New backends should import
// pkg/auth/backend directly and pay for what they use.
type (
	// Backend authenticates a username and password against one identity
	// source. See backend.Backend.
	Backend = backend.Backend
	// BackendConfig carries a backend's own `auth.<name>.*` subtree.
	BackendConfig = backend.Config
	// BackendFactory builds a Backend from its configuration subtree.
	BackendFactory = backend.Factory
)

var (
	// ErrInvalidCredentials reports a certain rejection. See
	// backend.ErrInvalidCredentials.
	ErrInvalidCredentials = backend.ErrInvalidCredentials
	// ErrBackendUnavailable reports that a backend could not reach its
	// source. See backend.ErrBackendUnavailable.
	ErrBackendUnavailable = backend.ErrBackendUnavailable
	// ErrUserNotFound is what a UserProvider returns when the username
	// does not exist. A provider may return it, or any other error; the
	// adapter in user_provider_backend.go maps every lookup failure to the
	// same outcome on purpose.
	//
	// It belongs in this block, with the other two: it used to be declared
	// separately with its own errors.New and the SAME message text, so a
	// leaf backend returning backend.ErrUserNotFound was unrecognisable to
	// code comparing against this one — and identical messages left nothing
	// to see in a log.
	ErrUserNotFound = backend.ErrUserNotFound
)

// RegisterBackend makes an authentication backend selectable by name from
// configuration. It delegates to backend.Register; a new backend should
// call that one and avoid importing this package at all.
func RegisterBackend(name string, factory BackendFactory) error {
	return backend.Register(name, factory)
}

// RegisteredBackends returns every selectable backend name, sorted.
func RegisteredBackends() []string { return backend.Registered() }

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

// ChainConfig declares the ordered chain and carries each backend's own
// configuration subtree.
type ChainConfig struct {
	// Backends is the ORDERED list of registered names to consult.
	Backends []string

	// ProviderConfig maps a backend name to its `auth.<name>.*` subtree.
	// A name with no entry gets an empty BackendConfig, which is the
	// normal case for a backend that needs no settings.
	ProviderConfig map[string]map[string]any
}

// NewChain builds a chain from registered backend names, in order.
//
// It is the convenience form for a chain whose backends need no
// configuration of their own; NewChainFrom is the full form.
func NewChain(names ...string) (*Chain, error) {
	return NewChainFrom(ChainConfig{Backends: names})
}

// NewChainFrom builds a chain from registered backend names, in order,
// handing each backend the configuration subtree that belongs to it.
func NewChainFrom(cfg ChainConfig) (*Chain, error) {
	names := cfg.Backends
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

		factory, ok := backend.Lookup(name)
		if !ok {
			return nil, unknownBackendError(raw)
		}
		backend, err := factory(BackendConfig{Name: name, ProviderConfig: cfg.ProviderConfig[name]})
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

// unknownBackendError explains a name the registry does not hold.
//
// When the name is one this project PUBLISHES, "unknown" is true and
// nearly useless: the operator wrote it because the documentation told
// them to, and what they need is the import line. A registry that only
// reports absence sends them reading source code to discover that the
// backend exists and lives one `go get` away.
func unknownBackendError(raw string) error {
	registered := strings.Join(RegisteredBackends(), ", ")
	if registered == "" {
		registered = "none"
	}
	if p, ok := knownproviders.AuthBackend(raw); ok {
		return fmt.Errorf("auth: %s %q ships with this framework as a separate module and nothing has imported it yet (registered: %s).\n\n\tAdd it with:\n\n%s\n\n\tIt is a separate module so that an application which does not use it does not carry its dependencies.",
			p.Kind, raw, registered, p.InstallHint())
	}
	return fmt.Errorf("auth: unknown authentication backend %q (registered: %s) — register one with auth.RegisterBackend, or use one of the backends this framework publishes: %s",
		raw, registered, strings.Join(knownproviders.AuthBackendNames(), ", "))
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
// A backend that REJECTS ends the attempt; one that is UNAVAILABLE is
// skipped. The two look alike — neither produced a user — but their first
// causes are opposite, and giving them the same effect is a fail-open:
// a rejection is the identity source's verdict on these credentials, while
// an unreachable backend proves nothing at all.
//
// Concretely, that is what stops a stale local row from being a bypass. If
// an employee's directory account is revoked and her local account still
// carries the old password, the directory's rejection ends the attempt and
// the local backend never gets its turn. Only an unreachable directory
// falls through to it, which is the break-glass path the ordering exists
// for.
//
// The consequence is worth stating plainly: a chain is a FALLBACK for
// unavailability, not a way to federate several user populations. Every
// account must be acceptable to the first backend that recognises the
// request, because anything behind a rejection is unreachable by design.
//
// Rejection covers both "no such user" and "wrong password": pkg/auth/backend
// collapses them into ErrInvalidCredentials on purpose, since a backend that
// told them apart would publish a user enumerator — and, because the chain
// stops on rejection, would publish it for every backend behind it too.
//
// The caller can still tell the two outcomes apart. If every backend
// rejected, the answer is ErrInvalidCredentials. If any backend was
// unavailable and none accepted, the error says so and names it, because
// "wrong password" and "the directory is down" send an operator hunting in
// different places.
func (c *Chain) Authenticate(ctx context.Context, username, password string) (*User, error) {
	if c == nil || len(c.backends) == 0 {
		return nil, fmt.Errorf("auth: no authentication backends configured")
	}

	var unavailable []string
	rejected := false
	for _, backend := range c.backends {
		user, err := backend.Authenticate(ctx, username, password)
		switch {
		case err == nil && user != nil:
			return user, nil
		case errors.Is(err, ErrInvalidCredentials):
			// A certain no: stop walking. Backends behind this one are not
			// consulted — continuing here is what let a revoked directory
			// account in through a stale local row.
			//
			// break, not an immediate return, so the error below still
			// accounts for any backend that was unavailable EARLIER in the
			// chain. With [dir down, local rejects] we cannot claim the
			// credentials are wrong: dir might have accepted them, and we
			// never got to ask.
			rejected = true
		default:
			// Anything that is not a clean rejection counts as
			// unavailable, including an unexpected error: a backend
			// failing in a way nobody anticipated must not be able to
			// lock every user out of the application.
			unavailable = append(unavailable, backend.Name())
		}
		if rejected {
			break
		}
	}

	if len(unavailable) > 0 {
		return nil, fmt.Errorf("%w: no backend accepted the credentials, and %s could not be reached — a rejection here does not prove the credentials are wrong",
			ErrBackendUnavailable, strings.Join(unavailable, ", "))
	}
	return nil, ErrInvalidCredentials
}

// unregisterBackendForTest removes a backend. Test-only.
func unregisterBackendForTest(name string) { backend.Unregister(name) }
