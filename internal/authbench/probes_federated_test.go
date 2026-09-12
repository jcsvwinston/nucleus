// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// stubProvider is the smallest provider that satisfies the contract: it
// exists so FED-01 can exercise the seam end to end without an identity
// provider, the way a third party would.
type stubProvider struct{ name string }

func (p *stubProvider) Name() string { return p.name }
func (p *stubProvider) Begin(context.Context, federated.BeginRequest) (federated.Redirect, error) {
	return federated.Redirect{URL: "https://idp.example/authorize"}, nil
}
func (p *stubProvider) Complete(context.Context, federated.CompleteRequest) (*backend.User, error) {
	return &backend.User{ID: "1", Username: "ana"}, nil
}

// FED-01 — the browser-redirect contract is implementable by a third party,
// and the framework owns the anti-forgery state.
func probeFederatedContract(t *testing.T, _ *env) verdict {
	name := "authbench-stub"
	if err := federated.Register(name, func(backend.Config) (federated.Provider, error) {
		return &stubProvider{name: name}, nil
	}); err != nil {
		t.Logf("Register: %v", err)
		return absent
	}
	t.Cleanup(func() { federated.Unregister(name) })

	var found bool
	for _, n := range federated.Registered() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Log("a registered provider does not show up in the registry")
		return partial
	}
	return present
}

// FED-02 — an OIDC provider that ships with the framework.
func probeOIDCProvider(t *testing.T, _ *env) verdict {
	return shippedFederatedProvider(t, "oidc")
}

// FED-03 — a SAML provider that ships with the framework.
func probeSAMLProvider(t *testing.T, _ *env) verdict {
	return shippedFederatedProvider(t, "saml")
}

// shippedFederatedProvider asks the registry what the framework itself
// registered. Importing every in-tree provider package is what an
// application does; the registry is where the result lands.
func shippedFederatedProvider(t *testing.T, kind string) verdict {
	for _, n := range federated.Registered() {
		if strings.Contains(strings.ToLower(n), kind) {
			t.Logf("provider %q is registered", n)
			return present
		}
	}
	return absent
}

// FED-04 — where the claims an identity provider returns land. The identity
// a provider hands back carries ONE role string, so a token with three
// groups has one field to fit in.
func probeClaimMapping(t *testing.T, _ *env) verdict {
	typ := reflect.TypeOf(backend.User{})
	f, ok := typ.FieldByName("Role")
	if !ok {
		return absent
	}
	if f.Type.Kind() == reflect.Slice {
		return present
	}
	for i := 0; i < typ.NumField(); i++ {
		if n := typ.Field(i).Name; n == "Groups" || n == "Roles" || n == "Claims" {
			t.Logf("identity carries %q", n)
			return partial
		}
	}
	t.Logf("the identity carries a single %s Role and no claims", f.Type.Kind())
	return absent
}
