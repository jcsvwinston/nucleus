// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco D §1: an authentication backend declares its own configuration.
//
// Arco B gave a registered STORAGE provider a configuration subtree. The
// authentication registry shipped in the same line without one: its
// factory took no arguments at all, so a directory backend had nowhere to
// read its URL, its base DN or its TLS settings from — and `auth.ldap.url`
// died as an unknown key before the backend ever ran.
//
// The gap was invisible because the godoc on BackendFactory already
// promised the subtree ("per-backend settings arrive through the
// configuration subtree the framework binds for it"). Prose describing
// something that does not happen is the one defect class no guard catches,
// which is why this test exists before the backend that needs it.
package nucleus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// The backend declares its own shape, exactly as a third-party module
// living outside the framework would.
type dirBackendConfig struct {
	URL     string        `koanf:"url" validate:"required"`
	BaseDN  string        `koanf:"base_dn" validate:"required"`
	Timeout time.Duration `koanf:"timeout" default:"5s"`
}

type dirBackend struct{ cfg dirBackendConfig }

func (d *dirBackend) Name() string { return "dirtest" }
func (d *dirBackend) Authenticate(context.Context, string, string) (*auth.User, error) {
	return nil, auth.ErrInvalidCredentials
}

func TestAuthBackendConfig_RegisteredNamespaceLoadsAndBinds(t *testing.T) {
	var bound dirBackendConfig
	if err := auth.RegisterBackend("dirtest", func(cfg auth.BackendConfig) (auth.Backend, error) {
		if err := cfg.Bind(&bound); err != nil {
			return nil, err
		}
		return &dirBackend{cfg: bound}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	const yml = "auth_backends:\n  - dirtest\nauth:\n  dirtest:\n    url: \"ldaps://dc.corp.local:636\"\n    base_dn: \"ou=people,dc=corp,dc=local\"\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	// (1) The namespace of a REGISTERED backend must survive the
	// unknown-key guard — on BOTH loading paths. The builder is what the
	// server runs on; app.LoadConfig is what every CLI command runs on,
	// and a file that boots a server while `nucleus check` calls it
	// malformed is the "same file, two verdicts" class this framework has
	// closed twice already.
	built, errBuilder := New().FromConfigFile(path).Build()
	loaded, errLoad := app.LoadConfig(path)
	if errBuilder != nil {
		t.Fatalf("a registered backend's config namespace must load on the builder path: %v", errBuilder)
	}
	if errLoad != nil {
		t.Fatalf("a registered backend's config namespace must load on the CLI path too: %v", errLoad)
	}

	// (2) Both paths must capture the same subtree. The runtime builds the
	// chain at Run, so the assertion here is the one the runtime depends
	// on: the configuration reached the config object that Run will use.
	for name, cfg := range map[string]app.Config{"builder": built.Config, "LoadConfig": *loaded} {
		sub, ok := cfg.AuthBackendConfig["dirtest"]
		if !ok {
			t.Fatalf("%s: the backend's subtree was not captured (got %#v)", name, cfg.AuthBackendConfig)
		}
		if sub["url"] != "ldaps://dc.corp.local:636" {
			t.Errorf("%s: the file's value must be captured, got %v", name, sub["url"])
		}
	}

	// (3) The subtree must reach the backend, typed, with defaults for
	// what the file omitted — which is what building the chain does.
	if _, err := auth.NewChainFrom(auth.ChainConfig{
		Backends:       built.Config.AuthBackends,
		ProviderConfig: built.Config.AuthBackendConfig,
	}); err != nil {
		t.Fatalf("build the chain: %v", err)
	}
	if bound.URL != "ldaps://dc.corp.local:636" {
		t.Errorf("the file's value must reach the backend, got %q", bound.URL)
	}
	if bound.BaseDN != "ou=people,dc=corp,dc=local" {
		t.Errorf("the file's value must reach the backend, got %q", bound.BaseDN)
	}
	if bound.Timeout != 5*time.Second {
		t.Errorf("a field the file omitted must come from its default tag, got %v", bound.Timeout)
	}
}

// The exemption is per REGISTERED name, not for the `auth.` namespace. A
// typo must still fail, or the namespace becomes a hole where any
// misspelling passes unseen until the day the setting mattered.
func TestAuthBackendConfig_TypoStillFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	const yml = "auth:\n  dirtst:\n    url: \"x\"\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errBuilder := New().FromConfigFile(path).Build()
	_, errLoad := app.LoadConfig(path)
	for name, err := range map[string]error{"builder": errBuilder, "LoadConfig": errLoad} {
		if err == nil {
			t.Fatalf("%s: an unregistered namespace must still be rejected as an unknown key", name)
		}
		if !strings.Contains(err.Error(), "dirtst") {
			t.Errorf("%s: the error must name the offending key, got %v", name, err)
		}
	}
}

// Arco D §3: configuring a backend and forgetting to list it in the chain
// must not be a silent no-op.
//
// The unknown-key guard cannot catch this one: the name IS registered, so
// `auth.dirtest.*` is legitimately exempt — and then the chain, its only
// consumer, never asks for it. The operator gets a clean boot, a green
// `nucleus check`, and a login page that never consults the directory they
// just configured. "Exit 0 without the effect", one namespace over.
func TestAuthBackendConfig_ConfiguredButNotInTheChainFails(t *testing.T) {
	if err := auth.RegisterBackend("orphantest", func(cfg auth.BackendConfig) (auth.Backend, error) {
		return &dirBackend{}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	path := filepath.Join(t.TempDir(), "nucleus.yml")
	const yml = "auth:\n  orphantest:\n    url: \"ldaps://dc.corp.local:636\"\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errBuilder := New().FromConfigFile(path).Build()
	_, errLoad := app.LoadConfig(path)
	for name, err := range map[string]error{"builder": errBuilder, "LoadConfig": errLoad} {
		if err == nil {
			t.Fatalf("%s: a backend configured but absent from auth_backends must fail", name)
		}
		if !strings.Contains(err.Error(), "auth.orphantest") || !strings.Contains(err.Error(), "auth_backends") {
			t.Errorf("%s: the error must name the section and the chain, got %v", name, err)
		}
	}

	// Listed in the chain, the very same section is fine.
	listed := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(listed, []byte("auth_backends:\n  - orphantest\n"+yml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoadConfig(listed); err != nil {
		t.Fatalf("the same section listed in the chain must load: %v", err)
	}
}
