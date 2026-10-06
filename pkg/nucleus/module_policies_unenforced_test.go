// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"bytes"
	"log/slog"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/routedump"
)

// NU-126: an application built WithoutDefaults() builds no RBAC enforcer, so
// the rows its modules declare in Policies — deny rows included — were
// dropped without a word, and the routes those rows keep from anonymous
// callers on the default stack answered anyone. What is reported is what the
// rows would have refused: a row granting the anonymous subject every action
// on a route the module serves is what a core-only application gives every
// route anyway, so a module whose rows are all of that shape loses nothing.

func notesLikeRoutes() []routedump.Route {
	return []routedump.Route{
		{Method: http.MethodGet, Pattern: "/notes", Module: "notes"},
		{Method: http.MethodPost, Pattern: "/notes", Module: "notes"},
		{Method: http.MethodGet, Pattern: "/notes/{id}", Module: "notes"},
		{Method: http.MethodPut, Pattern: "/notes/{id}", Module: "notes"},
		{Method: http.MethodDelete, Pattern: "/notes/{id}", Module: "notes"},
	}
}

func TestUnenforcedModulePolicies(t *testing.T) {
	cases := []struct {
		name   string
		module Module[struct{}]
		routes []routedump.Route
		want   []modulePolicyGap
	}{
		{
			name:   "no rows",
			module: Module[struct{}]{Name: "plain"},
			routes: []routedump.Route{{Method: http.MethodGet, Pattern: "/plain", Module: "plain"}},
			want:   nil,
		},
		{
			// The accounts module's shape: anonymous may do anything on
			// every route the module serves.
			name: "every route granted to anonymous, every action",
			module: Module[struct{}]{Name: "open", Policies: []PolicyRule{
				{Subject: "anonymous", Object: "/open", Action: "*"},
				{Subject: "anonymous", Object: "/open/*", Action: "*"},
			}},
			routes: []routedump.Route{
				{Method: http.MethodGet, Pattern: "/open", Module: "open"},
				{Method: http.MethodPost, Pattern: "/open/{id}", Module: "open"},
			},
			want: nil,
		},
		{
			// `nucleus generate module`'s default: anonymous reads, a write
			// needs an authenticated subject.
			name: "anonymous may only read",
			module: Module[struct{}]{Name: "notes", Policies: []PolicyRule{
				{Subject: "anonymous", Object: "/notes", Action: "read"},
				{Subject: "anonymous", Object: "/notes/*", Action: "read"},
			}},
			routes: notesLikeRoutes(),
			want: []modulePolicyGap{{module: "notes", rows: 2, deny: 0,
				unguarded: []string{"POST /notes", "PUT /notes/{id}", "DELETE /notes/{id}"}}},
		},
		{
			name: "a role's rows only",
			module: Module[struct{}]{Name: "notes", Policies: []PolicyRule{
				{Subject: "editor", Object: "/notes", Action: "*"},
				{Subject: "editor", Object: "/notes/*", Action: "*"},
			}},
			routes: notesLikeRoutes(),
			want: []modulePolicyGap{{module: "notes", rows: 2, deny: 0,
				unguarded: []string{"GET /notes", "POST /notes", "GET /notes/{id}", "PUT /notes/{id}", "DELETE /notes/{id}"}}},
		},
		{
			// A deny row refuses someone the default stack would refuse;
			// here it refuses nobody, whoever it names.
			name: "a deny row for a role, everything else open",
			module: Module[struct{}]{Name: "open", Policies: []PolicyRule{
				{Subject: "anonymous", Object: "/open", Action: "*"},
				{Subject: "banned", Object: "/open", Action: "*", Effect: "deny"},
			}},
			routes: []routedump.Route{{Method: http.MethodGet, Pattern: "/open", Module: "open"}},
			want:   []modulePolicyGap{{module: "open", rows: 2, deny: 1}},
		},
		{
			name: "a deny row for anonymous on part of the module",
			module: Module[struct{}]{Name: "open", Prefix: "/open", Policies: []PolicyRule{
				{Subject: "anonymous", Object: "/", Action: "*"},
				{Subject: "anonymous", Object: "/admin/*", Action: "*", Effect: "deny"},
			}},
			routes: []routedump.Route{
				{Method: http.MethodGet, Pattern: "/open/items", Module: "open"},
				{Method: "*", Pattern: "/open/admin/*", Module: "open"},
			},
			want: []modulePolicyGap{{module: "open", rows: 2, deny: 1, unguarded: []string{"* /open/admin/*"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.module.Build()
			got := unenforcedModulePolicies([]ModuleSpec{spec}, map[string][]routedump.Route{spec.Name(): tc.routes})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unenforcedModulePolicies =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

// One ERROR line, whatever the number of modules: which modules, how many
// rows and deny rows, which routes answer anyone, what to do, and the notice.
func TestLogModulePoliciesUnenforced(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, nil))
	logModulePoliciesUnenforced(logger, []modulePolicyGap{
		{module: "billing", rows: 3, deny: 1},
		{module: "notes", rows: 2, unguarded: []string{"POST /notes", "DELETE /notes/{id}"}},
	})
	out := strings.TrimSpace(buf.String())
	if n := strings.Count(out, "\n") + 1; n != 1 {
		t.Fatalf("want one line, got %d:\n%s", n, out)
	}
	for _, want := range []string{
		"level=ERROR", "module policies DISCARDED", "WithoutDefaults()",
		`modules="billing (3 rows, 1 deny), notes (2 rows, 0 deny)"`,
		"rows=5", "deny_rows=1",
		`unguarded="POST /notes, DELETE /notes/{id}"`,
		"DEP-2026-017", "v2.0.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the line does not say %q:\n%s", want, out)
		}
	}
}

// The notice the boot log cites exists in the register.
func TestModulePoliciesUnenforcedNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "docs", "deprecations", depModulePoliciesUnenforced+"-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/%s-*.md: found %v, want exactly one", depModulePoliciesUnenforced, matches)
	}
}
