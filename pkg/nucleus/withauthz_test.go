// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// WithAuthz through the builder (owner decision 2026-10-06): a core-only
// application that opts back into the default stack's authorization loads
// the rows its modules declare — allow and deny — into a default-deny
// enforcer, instead of discarding them with an ERROR line (NU-126), and
// refuses an anonymous request to a route no row allows.

// guardedNotes opens reads of /notes to anonymous callers, keeps writes for
// nobody, and denies one read the subtree grant would allow.
func guardedNotes() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "notes",
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/notes", Action: "read"},
			{Subject: "anonymous", Object: "/notes/*", Action: "read"},
			{Subject: "anonymous", Object: "/notes/secret", Action: "read", Effect: "deny"},
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, []string{}) })
			r.Post("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusCreated, map[string]string{"ok": "true"}) })
			r.Get("/notes/public", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, nil) })
			r.Get("/notes/secret", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, nil) })
		},
	}.Build()
}

func TestBuilder_WithAuthz_EnforcesModulePolicies(t *testing.T) {
	var srv *nucleustest.Server
	out := bootLog(t, func() {
		srv = nucleustest.Start(t, nucleus.New().
			FromConfigFile(starterConfig(t, "log_level: info\nlog_format: text\n")).
			WithoutDefaults().
			WithAuthz().
			Mount(guardedNotes()))
	})
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/notes", http.StatusOK},
		{http.MethodGet, "/notes/public", http.StatusOK},
		{http.MethodPost, "/notes", http.StatusForbidden},
		{http.MethodGet, "/notes/secret", http.StatusForbidden},
	} {
		var got int
		if tc.method == http.MethodPost {
			got = srv.Post(tc.path, map[string]string{}).Status
		} else {
			got = srv.Get(tc.path).Status
		}
		if got != tc.want {
			t.Errorf("%s %s with WithAuthz(): %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
	if strings.Contains(out, "module policies DISCARDED") {
		t.Fatalf("WithAuthz() loads the rows and the boot log still says they are discarded:\n%s", out)
	}
	if !strings.Contains(out, "module policies loaded into the live enforcer") {
		t.Fatalf("the boot log does not say the module's rows were loaded:\n%s", out)
	}
	if !strings.Contains(out, "authz: default-deny with 0 policy rows") {
		t.Fatalf("the boot log does not say the application is default-deny:\n%s", out)
	}
}

// A module that declares no rows serves routes nobody is granted: default-deny
// answers an anonymous request 403 — the same as the default stack.
func TestBuilder_WithAuthz_RouteWithoutAPolicyIsDenied(t *testing.T) {
	bare := nucleus.Module[struct{}]{
		Name: "bare",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/bare", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, nil) })
		},
	}.Build()
	for name, builder := range map[string]func(*nucleus.AppBuilder) *nucleus.AppBuilder{
		"WithoutDefaults + WithAuthz": func(b *nucleus.AppBuilder) *nucleus.AppBuilder { return b.WithoutDefaults().WithAuthz() },
		"the defaults":                func(b *nucleus.AppBuilder) *nucleus.AppBuilder { return b },
	} {
		t.Run(name, func(t *testing.T) {
			srv := nucleustest.Start(t, builder(nucleus.New().
				FromConfigFile(starterConfig(t, "log_level: error\n"))).
				Mount(bare))
			if got := srv.Get("/bare").Status; got != http.StatusForbidden {
				t.Fatalf("GET /bare anonymously: %d, want 403", got)
			}
			if got := srv.Get("/healthz").Status; got == http.StatusForbidden {
				t.Fatalf("GET /healthz is on the bootstrap allow-list and answered 403")
			}
		})
	}
}

// The package-level option and the builder method are the same thing: an
// App built from the struct with nucleus.WithAuthz() in Options enforces.
func TestWithAuthz_PackageOptionMatchesTheBuilder(t *testing.T) {
	b := nucleus.New().FromConfigFile(starterConfig(t, "log_level: error\n")).Mount(guardedNotes())
	built, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	built.Options = append(built.Options, nucleus.WithoutDefaults(), nucleus.WithAuthz())
	srv := nucleustest.StartApp(t, built)
	if got := srv.Post("/notes", map[string]string{}).Status; got != http.StatusForbidden {
		t.Fatalf("POST /notes with nucleus.WithAuthz() in App.Options: %d, want 403", got)
	}
}
