// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package interceptor

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register makes an interceptor selectable by name from configuration.
//
// Call it from an init function in the implementing package, then import
// that package for its side effects:
//
//	func init() {
//	    interceptor.Register("audit", New)
//	}
//
// A name already taken is an error rather than a silent replacement: two
// packages claiming "audit" would make the effective interceptor depend on
// import order, and for something in the request path that is a security
// control whose identity depends on the order of an import block.
func Register(name string, factory Factory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("router: interceptor name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("router: interceptor %q: factory cannot be nil", normalized)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[normalized]; exists {
		return fmt.Errorf("router: interceptor %q is already registered", normalized)
	}
	registry[normalized] = factory
	return nil
}

// Registered returns every selectable interceptor name, sorted.
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

// Unregister removes a registered interceptor. It exists for tests that
// register a fake and must not leak it into the next one.
func Unregister(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, strings.ToLower(strings.TrimSpace(name)))
}

// Build resolves an ORDERED list of registered names into the chain of
// interceptors, handing each one its own configuration subtree.
//
// The returned slice is in the same order as names: first in the list is
// outermost, so it sees the request first and the response last — the
// order middleware is normally read in.
//
// An unregistered name fails, naming what IS registered. That error is
// the only place an operator discovers the registry exists, and a typo in
// a list of request interceptors must not resolve to "one fewer
// protection, quietly".
func Build(names []string, providerConfig map[string]map[string]any) ([]Interceptor, error) {
	out := make([]Interceptor, 0, len(names))
	seen := make(map[string]struct{}, len(names))

	for i, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, fmt.Errorf("router: http_interceptors[%d] is empty", i)
		}
		if _, dup := seen[name]; dup {
			// The same interceptor twice is almost certainly a
			// copy-paste, and running it twice is rarely what anybody
			// means — a rate limiter counting each request twice, an
			// audit log with every entry duplicated.
			return nil, fmt.Errorf("router: interceptor %q appears twice in http_interceptors; an interceptor runs once, and the position it runs in is what the order declares", name)
		}
		seen[name] = struct{}{}

		factory, ok := Lookup(name)
		if !ok {
			return nil, fmt.Errorf("router: interceptor %q is not registered (registered: %s). An interceptor registers itself when its package is imported for side effects.",
				name, registeredList())
		}
		built, err := factory(Config{Name: name, ProviderConfig: providerConfig[name]})
		if err != nil {
			return nil, fmt.Errorf("router: interceptor %q: %w", name, err)
		}
		if built == nil {
			return nil, fmt.Errorf("router: interceptor %q: the factory returned no interceptor and no error, so the request path would silently lose it", name)
		}
		out = append(out, built)
	}
	return out, nil
}

func registeredList() string {
	names := Registered()
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
