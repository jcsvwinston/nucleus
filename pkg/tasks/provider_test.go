// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"strings"
	"testing"
)

type stubProvider struct{ name string }

func (s stubProvider) Open(ProviderOptions) (ProviderRuntime, error) {
	return ProviderRuntime{}, nil
}

func (s stubProvider) Inspect(ProviderOptions) RuntimeSnapshot {
	return RuntimeSnapshot{Enabled: true, Reason: s.name}
}

// The registry is how a jobs provider that ships as its own module reaches
// `jobs_provider`: a name registers once, resolves to what registered it,
// and the names come back sorted so an error message lists them stably.
func TestRegisterProvider_RegistersLooksUpAndLists(t *testing.T) {
	t.Cleanup(resetProviders())

	if err := RegisterProvider("zeta", stubProvider{name: "zeta"}); err != nil {
		t.Fatalf("register zeta: %v", err)
	}
	if err := RegisterProvider("alpha", stubProvider{name: "alpha"}); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	p, ok := LookupProvider("alpha")
	if !ok {
		t.Fatal("alpha is registered and LookupProvider does not find it")
	}
	if got := p.Inspect(ProviderOptions{}).Reason; got != "alpha" {
		t.Fatalf("LookupProvider(alpha) resolved to %q", got)
	}
	if _, ok := LookupProvider("rabbitmq"); ok {
		t.Fatal("a name nothing registered must not resolve")
	}
	if got := strings.Join(RegisteredProviders(), ","); got != "alpha,zeta" {
		t.Fatalf("RegisteredProviders() = %q, want alpha,zeta", got)
	}
}

// A duplicate is an error, not a silent overwrite: a module linked twice
// through two paths should fail at init where the import is visible.
func TestRegisterProvider_RefusesDuplicatesEmptyNamesAndNil(t *testing.T) {
	t.Cleanup(resetProviders())

	if err := RegisterProvider("dup", stubProvider{}); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := RegisterProvider("dup", stubProvider{}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("second registration of the same name: got %v", err)
	}
	if err := RegisterProvider("", stubProvider{}); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if err := RegisterProvider("nilprov", nil); err == nil {
		t.Fatal("a nil provider must be refused")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustRegisterProvider must panic on a duplicate")
		}
	}()
	MustRegisterProvider("dup", stubProvider{})
}

// resetProviders empties the registry for one test and returns the cleanup
// that restores what was there.
func resetProviders() func() {
	providersMu.Lock()
	saved := providers
	providers = map[string]Provider{}
	providersMu.Unlock()
	return func() {
		providersMu.Lock()
		providers = saved
		providersMu.Unlock()
	}
}
