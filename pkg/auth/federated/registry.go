// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package federated

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

// Register makes a federated provider TYPE selectable from configuration.
//
// The name registered here is the protocol — "oidc", "saml" — not the
// identity provider an operator points it at. That separation is the
// difference from the credential-backend registry, and it is deliberate:
// having two identity providers of the same type is ordinary (a corporate
// tenant and a partner one), so the name an operator writes in
// auth_federated is an INSTANCE name they choose, and `provider:` names
// the type registered here. A registry keyed by type would have made the
// second tenant impossible to express without a second module.
//
// Call it from an init function in the implementing package, then import
// that package for its side effects:
//
//	func init() {
//	    federated.Register("oidc", New)
//	}
//
// A name already taken is an error rather than a silent replacement: two
// packages claiming "oidc" would make the effective provider depend on
// import order, which only ever shows up in somebody else's deployment.
func Register(name string, factory Factory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("auth: federated provider type cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("auth: federated provider %q: factory cannot be nil", normalized)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[normalized]; exists {
		return fmt.Errorf("auth: federated provider %q is already registered", normalized)
	}
	registry[normalized] = factory
	return nil
}

// Registered returns every selectable provider type, sorted.
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

// Lookup returns the factory registered under a provider type.
func Lookup(name string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// Unregister removes a registered provider type. It exists for tests that
// register a fake and must not leak it into the next one.
func Unregister(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, strings.ToLower(strings.TrimSpace(name)))
}
