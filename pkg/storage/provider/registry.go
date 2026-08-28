// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ProviderFactory builds a Store from the resolved storage configuration.
//
// A provider reads the sub-config it owns (`cfg.S3`, `cfg.Local`, …) or,
// for a third-party backend, whatever it needs from the shared fields. It
// returns a Store; everything the framework layers on top — the circuit
// breaker, tenant prefixing, the public-URL mapper — is applied by New
// around whatever comes back, so a provider never has to reimplement any
// of it.
type Factory func(cfg Config) (Store, error)

var (
	providersMu sync.RWMutex
	providers   = map[string]Factory{}
)

// RegisterProvider makes a storage backend selectable by name from
// configuration (`storage.provider`).
//
// Call it from an init function in the package that implements the
// backend, then import that package for its side effects — the same shape
// database/sql drivers use, and the same one this framework already uses
// for mail providers and quark uses for SQL dialects:
//
//	package cephstore
//
//	func init() {
//	    storage.RegisterProvider("ceph", New)
//	}
//
// Registering a name that is already taken is an ERROR rather than a
// silent replacement: two packages claiming "s3" would otherwise make the
// effective backend depend on import order, which is the kind of bug that
// only shows up in someone else's deployment.
func Register(name string, factory Factory) error {
	normalized := normalizeProviderName(name)
	if normalized == "" {
		return fmt.Errorf("storage: provider name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("storage: provider %q: factory cannot be nil", normalized)
	}

	providersMu.Lock()
	defer providersMu.Unlock()
	if _, exists := providers[normalized]; exists {
		return fmt.Errorf("storage: provider %q is already registered", normalized)
	}
	providers[normalized] = factory
	return nil
}

// RegisteredProviders returns every selectable provider name, sorted.
// Built-ins are included, because from the outside they are not special.
func Registered() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()

	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lookupProvider resolves a configured provider name to its factory.
func lookupProvider(name string) (Factory, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	factory, ok := providers[normalizeProviderName(name)]
	return factory, ok
}

// unregisterProviderForTest removes a provider. Test-only: the public API
// deliberately has no way to unregister, because a running application
// swapping its storage backend is not a thing that should be expressible.
func unregisterProviderForTest(name string) {
	providersMu.Lock()
	defer providersMu.Unlock()
	delete(providers, normalizeProviderName(name))
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	f, ok := providers[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// Unregister removes a registered provider.
//
// It exists for tests that register a fake and must not leak it into the
// next one. Production code has no reason to call it.
func Unregister(name string) {
	providersMu.Lock()
	defer providersMu.Unlock()
	delete(providers, strings.ToLower(strings.TrimSpace(name)))
}
