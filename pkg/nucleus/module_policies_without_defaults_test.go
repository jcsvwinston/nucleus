// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// NU-126 through the builder: a WithoutDefaults() application mounting a
// module whose rows keep writes from anonymous callers still starts, still
// answers those writes (QADR-0010: nothing that boots today changes before
// the major), and says so in one ERROR line naming the module, its deny rows
// and the routes that answer anyone. On the default stack the same module is
// enforced and nothing is said; a module whose rows grant anonymous every
// action on every route it serves loses nothing and is not reported.

// bootLog runs start with the process's stdout captured, which is where the
// application's logger writes, and returns what the boot wrote.
func bootLog(t *testing.T, start func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		start()
	}()
	return <-done
}

func readOnlyNotes() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "notes",
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/notes", Action: "read"},
			{Subject: "anonymous", Object: "/notes/*", Action: "read"},
			{Subject: "banned", Object: "/notes", Action: "*", Effect: "deny"},
		},
		CSRFExempt: []string{"/notes"},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, []string{}) })
			r.Post("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusCreated, map[string]string{"ok": "true"}) })
		},
	}.Build()
}

func TestBuilder_WithoutDefaults_ModulePoliciesAreReported(t *testing.T) {
	var srv *nucleustest.Server
	out := bootLog(t, func() {
		srv = nucleustest.Start(t, nucleus.New().
			FromConfigFile(starterConfig(t, "log_level: error\nlog_format: text\n")).
			WithoutDefaults().
			Mount(readOnlyNotes()))
	})
	if got := srv.Post("/notes", map[string]string{}).Status; got != http.StatusCreated {
		t.Fatalf("POST /notes on WithoutDefaults(): %d, want 201 — nothing is enforced before the major", got)
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "module policies DISCARDED") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("want exactly one ERROR line, got %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"level=ERROR", "notes (3 rows, 1 deny)", "deny_rows=1", "POST /notes", "WithoutDefaults()", "DEP-2026-017"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the line does not say %q:\n%s", want, lines[0])
		}
	}
}

func TestBuilder_ModulePolicies_NothingDiscarded_NoLine(t *testing.T) {
	openAccounts := nucleus.Module[struct{}]{
		Name: "open",
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/open", Action: "*"},
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/open", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, nil) })
		},
	}.Build()
	for name, tc := range map[string]struct {
		builder   func(*nucleus.AppBuilder) *nucleus.AppBuilder
		module    nucleus.ModuleSpec
		path      string
		wantWrite int
	}{
		"the defaults enforce the rows": {
			builder:   func(b *nucleus.AppBuilder) *nucleus.AppBuilder { return b },
			module:    readOnlyNotes(),
			path:      "/notes",
			wantWrite: http.StatusForbidden,
		},
		"WithoutDefaults, rows that grant anonymous everything": {
			builder:   func(b *nucleus.AppBuilder) *nucleus.AppBuilder { return b.WithoutDefaults() },
			module:    openAccounts,
			path:      "/open",
			wantWrite: http.StatusOK,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var srv *nucleustest.Server
			out := bootLog(t, func() {
				srv = nucleustest.Start(t, tc.builder(nucleus.New().
					FromConfigFile(starterConfig(t, "log_level: error\nlog_format: text\n"))).
					Mount(tc.module))
			})
			if strings.Contains(out, "module policies DISCARDED") {
				t.Fatalf("the line fired where no row is discarded:\n%s", out)
			}
			if got := srv.Post(tc.path, map[string]string{}).Status; got != tc.wantWrite {
				t.Fatalf("POST %s: %d, want %d", tc.path, got, tc.wantWrite)
			}
		})
	}
}
