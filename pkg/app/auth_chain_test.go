// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco C: la cadena de autenticación se declara desde la configuración y
// el UserProvider de la aplicación por fin se consume.
//
// auth.UserProvider has described how to reach an application's users
// since v0.x. It was frozen in the contract baseline and called by
// nothing: the framework declared the interface and never used it. This
// wires it to the login path.
package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

type tableProvider struct {
	user, pass string
	calls      int
}

func (p *tableProvider) FindByID(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p *tableProvider) FindByUsername(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p *tableProvider) FindByEmail(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p *tableProvider) ValidateCredentials(_ context.Context, username, password string) (*auth.User, error) {
	p.calls++
	if username == p.user && password == p.pass {
		return &auth.User{ID: "1", Username: username}, nil
	}
	return nil, auth.ErrUserNotFound
}

func newTestApp(t *testing.T, backends []string, opts ...Option) *App {
	t.Helper()
	cfg := DefaultConfig()
	cfg.AuthBackends = backends
	cfg.Databases = map[string]DatabaseConfig{
		"default": {URL: "sqlite://" + t.TempDir() + "/auth.db"},
	}
	a, err := New(&cfg, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	return a
}

func TestAuthChain_UserProviderIsConsulted(t *testing.T) {
	provider := &tableProvider{user: "ana", pass: "correcta"}
	a := newTestApp(t, []string{"local"}, WithUserProvider(provider))

	if a.AuthChain == nil {
		t.Fatal("declaring auth_backends must build a chain")
	}

	user, err := a.AuthChain.Authenticate(context.Background(), "ana", "correcta")
	if err != nil || user == nil || user.Username != "ana" {
		t.Fatalf("the application's own user table must authenticate: user=%v err=%v", user, err)
	}
	if provider.calls == 0 {
		t.Error("the UserProvider must actually be called — it was declared and unused for entire major versions")
	}
}

// Every rejection reason must be indistinguishable to the caller.
func TestAuthChain_RejectionsAreIndistinguishable(t *testing.T) {
	provider := &tableProvider{user: "ana", pass: "correcta"}
	a := newTestApp(t, []string{"local"}, WithUserProvider(provider))

	_, wrongPass := a.AuthChain.Authenticate(context.Background(), "ana", "mala")
	_, noSuchUser := a.AuthChain.Authenticate(context.Background(), "nadie", "loquesea")

	if !errors.Is(wrongPass, auth.ErrInvalidCredentials) || !errors.Is(noSuchUser, auth.ErrInvalidCredentials) {
		t.Fatalf("both rejections must be ErrInvalidCredentials: wrongPass=%v noSuchUser=%v", wrongPass, noSuchUser)
	}
	if wrongPass.Error() != noSuchUser.Error() {
		t.Errorf("a wrong password and an unknown user must be indistinguishable, got %q vs %q", wrongPass, noSuchUser)
	}
}

// No declaration, no chain. An empty chain that always rejects would be a
// worse answer than none.
func TestAuthChain_NotDeclaredMeansNoChain(t *testing.T) {
	a := newTestApp(t, nil, WithUserProvider(&tableProvider{}))
	if a.AuthChain != nil {
		t.Error("an application that declares no backends must not get a chain")
	}
}

// A name that is not registered can only fail at the first login attempt,
// which is the worst moment to find a typo in an authentication list.
func TestAuthChain_UnknownBackendFailsAtBoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthBackends = []string{"ldap"}
	cfg.Databases = map[string]DatabaseConfig{"default": {URL: "sqlite://" + t.TempDir() + "/x.db"}}

	_, err := New(&cfg)
	if err == nil {
		t.Fatal("an unregistered backend must fail the boot, not the first login")
	}
	if !strings.Contains(err.Error(), "ldap") {
		t.Errorf("the error must name the offending backend, got %v", err)
	}
}

// Arco D §1: the subtree a backend declares must reach it through App.New,
// not only through the loader that captured it.
//
// The registry shipped with a factory that took no arguments, so a
// directory backend had nowhere to read its URL from — while the godoc on
// BackendFactory already promised the subtree. This is the end of that
// wire: config file → capture → App.New → factory.
func TestAuthChain_BackendReceivesItsConfigSubtree(t *testing.T) {
	var got auth.BackendConfig
	var bound struct {
		URL     string `koanf:"url" validate:"required"`
		Timeout string `koanf:"timeout" default:"5s"`
	}
	name := "subtreetest"
	if err := auth.RegisterBackend(name, func(cfg auth.BackendConfig) (auth.Backend, error) {
		got = cfg
		if err := cfg.Bind(&bound); err != nil {
			return nil, err
		}
		return &stubBackend{name: name}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	cfg := DefaultConfig()
	cfg.AuthBackends = []string{name}
	cfg.AuthBackendConfig = map[string]map[string]any{
		name: {"url": "ldaps://dc.corp.local:636"},
	}
	cfg.Databases = map[string]DatabaseConfig{
		"default": {URL: "sqlite://" + t.TempDir() + "/auth.db"},
	}
	a, err := New(&cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	if got.Name != name {
		t.Errorf("the backend must know the name it was selected by, got %q", got.Name)
	}
	if bound.URL != "ldaps://dc.corp.local:636" {
		t.Errorf("the configured value must reach the backend, got %q", bound.URL)
	}
	if bound.Timeout != "5s" {
		t.Errorf("an omitted field must come from its default tag, got %q", bound.Timeout)
	}
}

type stubBackend struct{ name string }

func (s *stubBackend) Name() string { return s.name }
func (s *stubBackend) Authenticate(context.Context, string, string) (*auth.User, error) {
	return nil, auth.ErrInvalidCredentials
}
