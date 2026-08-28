// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Live tests against a REAL directory, in the discipline this repository
// uses for S3 and Redis: the fake proves the branches, the real server
// proves the protocol. They skip unless NUCLEUS_LDAP_URL points at one.
//
// The lane in CI starts an OpenLDAP container and seeds the two entries
// these tests expect; see the package README for the exact commands, which
// are the same ones the workflow runs.
package ldap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
)

func liveConfig(t *testing.T) Config {
	t.Helper()
	url := os.Getenv("NUCLEUS_LDAP_URL")
	if url == "" {
		t.Skip("set NUCLEUS_LDAP_URL to run the live LDAP tests")
	}
	return Config{
		URL:          url,
		BaseDN:       envOr("NUCLEUS_LDAP_BASE_DN", "ou=people,dc=example,dc=org"),
		BindDN:       envOr("NUCLEUS_LDAP_BIND_DN", "cn=admin,dc=example,dc=org"),
		BindPassword: envOr("NUCLEUS_LDAP_BIND_PASSWORD", "adminpassword"),
		UserFilter:   "(uid=%s)",
		AttrUsername: "uid",
		AttrEmail:    "mail",
		AttrName:     "cn",
		Timeout:      10 * time.Second,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func liveBackend(t *testing.T) *Backend {
	t.Helper()
	b, err := NewWithConfig(liveConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	return b
}

func TestLive_AuthenticatesAgainstARealDirectory(t *testing.T) {
	b := liveBackend(t)

	user, err := b.Authenticate(context.Background(), "ana", "correcta")
	if err != nil {
		t.Fatalf("a valid credential must authenticate: %v", err)
	}
	if user.Username != "ana" || user.Email != "ana@example.org" {
		t.Errorf("the directory's attributes must reach auth.User, got %+v", user)
	}
	if user.ID != "uid=ana,"+b.cfg.BaseDN {
		t.Errorf("ID must be the entry's DN, got %q", user.ID)
	}
}

func TestLive_RejectionsAreRejections(t *testing.T) {
	b := liveBackend(t)

	for _, tc := range []struct{ name, user, pass string }{
		{"wrong password", "ana", "incorrecta"},
		{"unknown user", "nadie", "cualquiera"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := b.Authenticate(context.Background(), tc.user, tc.pass)
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("want a rejection, got %v", err)
			}
			// A rejection must never be reported as unavailability: the
			// chain would fall through to the next backend and, worse, an
			// operator would go looking for a network problem.
			if errors.Is(err, auth.ErrBackendUnavailable) {
				t.Error("a rejection must not also read as unavailable")
			}
		})
	}
}

// The empty-password guard, exercised against a real server instead of
// asserted from the RFC.
//
// The test RECORDS what this directory and this client do with a bind as a
// real user's DN and an empty password — RFC 4513 §5.1.2 makes it an
// unauthenticated bind, which a server may answer with success — and then
// requires the backend to reject those credentials either way. The name
// says "whatever the directory says" because that is the property being
// pinned: the outcome must not depend on the server's or the library's
// choice, both of which we do not control.
func TestLive_EmptyPasswordIsRejectedWhateverTheDirectorySays(t *testing.T) {
	cfg := liveConfig(t)
	dn := "uid=ana," + cfg.BaseDN

	c, err := goldap.DialURL(cfg.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	rawErr := c.Bind(dn, "")
	if rawErr != nil {
		// If a directory ever refuses this on its own, the guard becomes
		// belt-and-braces rather than load-bearing — worth knowing, not
		// worth failing over, because the next directory will not.
		t.Logf("this directory refused the unauthenticated bind by itself (%v); the guard is still what makes the behaviour independent of the server", rawErr)
	} else {
		t.Log("the directory ACCEPTED a bind with an empty password — this is why the guard is the first thing Authenticate does")
	}

	b, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	if _, err := b.Authenticate(context.Background(), "ana", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("the backend must reject an empty password whatever the directory says, got %v", err)
	}
}

// Filter injection against a real server: the crafted username must not
// authenticate as anybody, even though the same string unescaped would
// match every entry in the subtree.
func TestLive_FilterInjectionCannotAuthenticate(t *testing.T) {
	b := liveBackend(t)

	_, err := b.Authenticate(context.Background(), "*)(uid=*", "correcta")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("a crafted username must not authenticate, got %v", err)
	}
	_, err = b.Authenticate(context.Background(), "*", "correcta")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("a wildcard username must not authenticate, got %v", err)
	}
}

// An unreachable directory must be UNAVAILABLE and never a rejection —
// the property the whole chain design rests on.
func TestLive_UnreachableDirectoryIsUnavailable(t *testing.T) {
	cfg := liveConfig(t)
	cfg.URL = "ldap://127.0.0.1:1" // nothing listens here
	cfg.Timeout = 2 * time.Second
	b, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	_, err = b.Authenticate(context.Background(), "ana", "correcta")
	if !errors.Is(err, auth.ErrBackendUnavailable) {
		t.Fatalf("an unreachable directory must be unavailable, got %v", err)
	}
	if errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("an unreachable directory reported as a rejection would lock out the break-glass account")
	}
}

// The whole arc in one test: a configuration FILE goes through the
// framework's own loader, the chain is built from what it declared, and a
// real person authenticates against a real directory.
//
// Everything else in this package tests one link. This is the only test
// that fails if any of them is wired wrong — the config subtree not
// reaching the factory, the backend not registering under the name the
// file uses, the chain not consulting it, the loader rejecting the
// section. It is what "integrated rather than a plugin" has to mean in
// practice.
func TestLive_EndToEndFromAConfigFile(t *testing.T) {
	cfg := liveConfig(t) // skips unless a directory is available

	yml := fmt.Sprintf(`auth_backends:
  - %s
auth:
  %s:
    url: %q
    base_dn: %q
    bind_dn: %q
    bind_password: %q
`, BackendName, BackendName, cfg.URL, cfg.BaseDN, cfg.BindDN, cfg.BindPassword)

	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := app.LoadConfig(path)
	if err != nil {
		t.Fatalf("the framework's loader must accept a file that configures this backend: %v", err)
	}

	chain, err := auth.NewChainFrom(auth.ChainConfig{
		Backends:       loaded.AuthBackends,
		ProviderConfig: loaded.AuthBackendConfig,
	})
	if err != nil {
		t.Fatalf("the chain must build from what the file declared: %v", err)
	}

	user, err := chain.Authenticate(context.Background(), "ana", "correcta")
	if err != nil {
		t.Fatalf("a real credential must authenticate through the whole wire: %v", err)
	}
	if user.Username != "ana" {
		t.Errorf("got %+v", user)
	}

	if _, err := chain.Authenticate(context.Background(), "ana", "incorrecta"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("and a wrong one must be rejected through the same wire, got %v", err)
	}
}
