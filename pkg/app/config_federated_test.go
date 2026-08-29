// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/providerns"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func registerStub(t *testing.T, typ string) {
	t.Helper()
	if err := federated.Register(typ, func(cfg backend.Config) (federated.Provider, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { federated.Unregister(typ) })
}

// The settings of a declared federated instance must survive the loader.
// A provider that cannot read its issuer is a provider nobody can deploy —
// the same defect the Arco D found for credential backends, which is why
// it travels through the same channel.
func TestLoadConfig_FederatedInstanceSubtreeReachesTheProvider(t *testing.T) {
	path := writeConfig(t, `
public_base_url: https://app.example.com
auth_federated:
  - name: corp
    provider: oidc
    display_name: Corp SSO
  - name: partners
    provider: oidc
auth:
  corp:
    issuer: https://login.corp.example/
  partners:
    issuer: https://idp.partners.example/
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.AuthFederated) != 2 {
		t.Fatalf("want 2 declared instances, got %d (%+v)", len(cfg.AuthFederated), cfg.AuthFederated)
	}
	if cfg.AuthFederated[0].Name != "corp" || cfg.AuthFederated[0].Provider != "oidc" {
		t.Errorf("first instance = %+v", cfg.AuthFederated[0])
	}
	if cfg.AuthFederated[0].DisplayName != "Corp SSO" {
		t.Errorf("display name = %q", cfg.AuthFederated[0].DisplayName)
	}
	if got := cfg.AuthBackendConfig["corp"]["issuer"]; got != "https://login.corp.example/" {
		t.Errorf("corp issuer = %v; the instance's subtree must reach its factory", got)
	}
	if got := cfg.AuthBackendConfig["partners"]["issuer"]; got != "https://idp.partners.example/" {
		t.Errorf("partners issuer = %v", got)
	}
	if cfg.PublicBaseURL != "https://app.example.com" {
		t.Errorf("public base URL = %q", cfg.PublicBaseURL)
	}
}

// "The same file, two verdicts" is the defect this framework has found
// three times. This is the fourth shape it could take: the exemption for
// a federated instance's subtree comes from the DECLARATION, not from a
// registry, so a validator that does not read the declaration would
// reject a file the other one accepts.
func TestFederatedSubtree_BothValidatorsAgree(t *testing.T) {
	body := `
public_base_url: https://app.example.com
auth_federated:
  - name: corp
    provider: oidc
auth:
  corp:
    issuer: https://login.corp.example/
    client_id: nucleus
`
	path := writeConfig(t, body)

	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("app.LoadConfig rejected a file that declares its instance: %v", err)
	}

	// The builder path's own key validation, reached through the same file.
	if err := validateConfigFileKeys(map[string]any{
		"public_base_url":     "https://app.example.com",
		"auth_federated":      []any{map[string]any{"name": "corp", "provider": "oidc"}},
		"auth.corp.issuer":    "https://login.corp.example/",
		"auth.corp.client_id": "nucleus",
	}, declaredFromMap("corp")); err != nil {
		t.Fatalf("the key validator rejected a declared instance's subtree: %v", err)
	}
}

// The exemption is per DECLARED instance and never namespace-wide: a
// subtree nobody declared is still an unknown key. Otherwise `auth.` would
// become the one place in the configuration where any typo passes unseen.
func TestFederatedSubtree_UndeclaredInstanceIsStillAnUnknownKey(t *testing.T) {
	path := writeConfig(t, `
public_base_url: https://app.example.com
auth_federated:
  - name: corp
    provider: oidc
auth:
  corp:
    issuer: https://login.corp.example/
  crop:
    issuer: https://typo.example/
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("a subtree for an instance nobody declared must be an unknown key")
	}
	if !strings.Contains(err.Error(), "crop") {
		t.Errorf("the error must name the undeclared subtree, got: %v", err)
	}
}

// A declared instance whose provider type nobody registered must fail
// when the set is built, naming what IS registered.
func TestFederatedSet_UnregisteredProviderFailsNamingTheRegistered(t *testing.T) {
	registerStub(t, "oidc")

	_, err := auth.NewFederatedSet(auth.FederatedConfig{
		Instances:    []auth.FederatedInstance{{Name: "corp", Provider: "sarl"}},
		CallbackBase: "https://app.example.com",
	})
	if err == nil {
		t.Fatal("an unregistered provider type must fail")
	}
	if !strings.Contains(err.Error(), "oidc") {
		t.Errorf("the error must name the registered types, got: %v", err)
	}
}

func declaredFromMap(names ...string) providerns.Declared {
	return providerns.Declared{FederatedAuth: names}
}
