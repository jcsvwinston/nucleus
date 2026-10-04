// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// starterConfig writes the configuration of an api-starter-shaped
// application: a throwaway SQLite database, then whatever block follows.
func starterConfig(t *testing.T, block string) string {
	t.Helper()
	dir := t.TempDir()
	body := "databases:\n  default:\n    url: sqlite://" + filepath.Join(dir, "app.db") + "\n" + block
	path := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The builder's loader records whether the configuration WROTE storage on
// the same terms as app.LoadConfig — including the default written by hand,
// which provenance alone attributes to the default.
func TestFromConfigFile_RecordsWhetherStorageIsDeclared(t *testing.T) {
	cases := []struct {
		name  string
		block string
		env   map[string]string
		want  bool
	}{
		{name: "no storage block", want: false},
		{name: "a provider", block: "storage:\n  provider: memory\n", want: true},
		{name: "the default written by hand", block: "storage:\n  provider: local\n", want: true},
		{name: "the environment", env: map[string]string{"NUCLEUS_STORAGE__PROVIDER": "memory"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			a, err := nucleus.New().FromConfigFile(starterConfig(t, tc.block)).WithoutDefaults().Build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if a.Config.StorageDeclared != tc.want {
				t.Fatalf("StorageDeclared = %v, want %v", a.Config.StorageDeclared, tc.want)
			}
		})
	}
}

// NU-99 on the builder: the api starter's shape without WithStorage still
// starts — what booted before keeps booting until the major (QADR-0010) —
// and builds no store for the block it ignores. The ERROR line it logs is
// pinned in pkg/app, where the logger is.
func TestBuilder_WithoutDefaults_DeclaredStorageStillStarts(t *testing.T) {
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(starterConfig(t, "storage:\n  provider: memory\n")).
		WithOpenAuthz().
		WithoutDefaults())
	t.Cleanup(srv.Stop)
	if s := srv.Runtime().Storage(); s != nil {
		t.Fatalf("WithoutDefaults() without WithStorage() built a store (%T)", s)
	}
}

// The api starter's shape: WithoutDefaults().WithStorage() builds the
// storage nucleus.yml declares, and none while it declares none.
func TestBuilder_WithStorage_FollowsTheConfiguration(t *testing.T) {
	t.Run("declared", func(t *testing.T) {
		srv := nucleustest.Start(t, nucleus.New().
			FromConfigFile(starterConfig(t, "storage:\n  provider: memory\n")).
			WithOpenAuthz().
			WithoutDefaults().
			WithStorage())
		t.Cleanup(srv.Stop)
		if srv.Runtime().Storage() == nil {
			t.Fatal("WithStorage() built no store for a declared storage block")
		}
	})
	t.Run("not declared", func(t *testing.T) {
		srv := nucleustest.Start(t, nucleus.New().
			FromConfigFile(starterConfig(t, "")).
			WithOpenAuthz().
			WithoutDefaults().
			WithStorage())
		t.Cleanup(srv.Stop)
		if s := srv.Runtime().Storage(); s != nil {
			t.Fatalf("a configuration that declares no storage got a store (%T)", s)
		}
	})
}

// NU-100 on the builder: the strict loader says the ldap backend is not
// installed — and how to install it — instead of suggesting an unrelated
// key, and the refusal is still an unknown-keys error.
func TestFromConfigFile_PublishedBackendKeysSayNotInstalled(t *testing.T) {
	_, err := nucleus.New().
		FromConfigFile(starterConfig(t, "auth_backends: [ldap]\nauth:\n  ldap:\n    url: ldap://127.0.0.1:1\n    base_dn: dc=example,dc=test\n")).
		WithoutDefaults().
		Build()
	if err == nil {
		t.Fatal("the builder accepted the subtree of a backend that is not linked")
	}
	if !errors.Is(err, nucleus.ErrUnknownConfigKeys) {
		t.Errorf("the refusal no longer wraps ErrUnknownConfigKeys: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"auth.ldap.url (not installed: `nucleus add ldap`)",
		"github.com/jcsvwinston/nucleus/providers/ldap",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "did you mean databases") {
		t.Errorf("the refusal still sends the operator to an unrelated key:\n%s", msg)
	}
}
