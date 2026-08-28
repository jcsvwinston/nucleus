// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package backend is the contract a third-party authentication backend
// implements — and nothing else.
//
// It exists because of a measurement. The contract used to live in
// pkg/auth, so a provider that implements two methods imported a package
// that links 115 third-party packages: session stores, JWT, Redis,
// Prometheus, OpenTelemetry, the gRPC gateway. None of them are needed to
// answer "do these credentials belong to a real user", and every one of
// them was a dependency the author of a plugin inherited without asking.
//
// This package links two packages from ONE module (the configuration
// decoder). That is the whole point, and it is a property worth
// protecting: an import added here is paid by everyone who writes a
// backend, so it needs the same scrutiny as a change to the interface
// itself. contracts.TestPluginContract_StaysLight is the forcing function.
//
// The names remain available from pkg/auth as aliases, so nothing that
// already compiled stops compiling.
package backend

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/internal/providerconfig"
)

// ErrInvalidCredentials reports that a backend recognised the request and
// rejected it: the user does not exist, or the password does not match.
//
// It is deliberately ONE error for both cases. A backend that
// distinguishes them — a different error, or merely a faster answer —
// publishes a user enumerator, and that is a property of the whole chain
// rather than of one backend.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrBackendUnavailable reports that a backend could not reach the system
// it authenticates against — the directory is down, the connection timed
// out. It is NOT a rejection: the chain records it and tries the next
// backend, which is what lets a local break-glass account work while the
// directory is unreachable.
var ErrBackendUnavailable = errors.New("auth: backend unavailable")

// User is the identity a backend returns. Plain fields on purpose: it
// crosses the boundary between the framework and code it does not own, so
// it carries no behaviour and no third-party type.
type User struct {
	ID          string
	Username    string
	Email       string
	Role        string
	IsSuperuser bool
}

// Backend authenticates a username and password against one identity
// source: the application's own user table, an LDAP directory, anything
// else someone registers.
//
// Authenticate returns the authenticated user, or ErrInvalidCredentials
// when the backend is sure the answer is no, or ErrBackendUnavailable when
// it could not reach its source. Any other error is treated as
// unavailable — a backend that fails in an unexpected way must not be able
// to lock everyone out.
type Backend interface {
	// Name identifies the backend in logs and configuration.
	Name() string
	// Authenticate verifies credentials against this identity source.
	Authenticate(ctx context.Context, username, password string) (*User, error)
}

// Config carries the `auth.<backend>.*` subtree that belongs to one
// registered authentication backend.
type Config struct {
	// Name is the registered name the chain selected this backend by.
	Name string

	// ProviderConfig is the raw `auth.<name>.*` subtree. Read it with Bind
	// rather than reaching into the map.
	ProviderConfig map[string]any
}

// Bind decodes the backend's own configuration subtree into dst, applying
// `default:` tags to fields the file left unset.
//
// A key the destination struct does not declare is an ERROR, not a
// silently ignored line. Backend configuration is exactly where a typo
// would otherwise sit unnoticed until the day the setting mattered — and
// here that day is an outage of the login path.
func (c Config) Bind(dst any) error {
	name := c.Name
	if name == "" {
		name = "auth backend"
	}
	return providerconfig.Bind(name, c.ProviderConfig, dst)
}

// Factory builds a Backend from the configuration subtree the framework
// binds for it.
type Factory func(cfg Config) (Backend, error)

// ErrUserNotFound is what a UserProvider returns when the username does
// not exist.
var ErrUserNotFound = errors.New("auth: user not found")

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register makes an authentication backend selectable by name from
// configuration.
//
// Call it from an init function in the package that implements the
// backend, then import that package for its side effects — the shape
// database/sql drivers use:
//
//	package ldap
//
//	func init() {
//	    backend.Register("ldap", New)
//	}
//
// Registering a name that is already taken is an ERROR rather than a
// silent replacement: two packages claiming "ldap" would otherwise make
// the effective backend depend on import order, a bug that only ever
// appears in someone else's deployment.
func Register(name string, factory Factory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("auth: backend name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("auth: backend %q: factory cannot be nil", normalized)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[normalized]; exists {
		return fmt.Errorf("auth: backend %q is already registered", normalized)
	}
	registry[normalized] = factory
	return nil
}

// Registered returns every selectable backend name, sorted.
func Registered() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// Unregister removes a registered backend.
//
// It exists for tests that register a fake and must not leak it into the
// next one. Production code has no reason to call it, and calling it does
// not affect a chain that already holds the backend — the chain resolved
// its factories when it was built.
func Unregister(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, strings.ToLower(strings.TrimSpace(name)))
}
