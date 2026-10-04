// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

type notesConfig struct {
	Mode string `default:"fast" validate:"oneof=fast slow"`
}

type conformanceNote struct {
	ID    int64  `db:"column:id;pk" json:"id"`
	Title string `db:"column:title" json:"title"`
}

// notesModule is a module that does everything right: a typed config with
// defaults, a model, an embedded migration, its own policy rows and CSRF
// exemption about the routes it serves, a job, a webhook, and hooks that
// succeed. mutate breaks one thing at a time.
func notesModule(mutate func(m *nucleus.Module[notesConfig])) nucleus.ModuleSpec {
	ok := func(c *nucleus.Context) error { return c.NoContent() }
	m := nucleus.Module[notesConfig]{
		Name:   "notes",
		Prefix: "/notes",
		Models: []any{&conformanceNote{}},
		Migrations: fstest.MapFS{
			"000001_create_notes.up.sql":   {Data: []byte("CREATE TABLE conformance_notes (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL);")},
			"000001_create_notes.down.sql": {Data: []byte("DROP TABLE conformance_notes;")},
		},
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/", Action: "read"},
			{Subject: "anonymous", Object: "/items", Action: "create"},
		},
		CSRFExempt: []string{"/items"},
		OnStart: func(_ context.Context, rt nucleus.Runtime, cfg notesConfig) error {
			if cfg.Mode != "fast" {
				return fmt.Errorf("mode %q", cfg.Mode)
			}
			return rt.ApplyModuleMigrations()
		},
		OnShutdown: func(context.Context, nucleus.Runtime, notesConfig) error { return nil },
		Jobs: func(j nucleus.JobRegistry, _ notesConfig) {
			_ = j.Register("sweep", nucleus.JobSpec{Every: time.Hour, Handler: func(context.Context) error { return nil }})
		},
		Webhooks: func(w nucleus.WebhookRegistry, _ notesConfig) {
			_ = w.Register("/ping", nucleus.WebhookSpec{Secret: "conformance", Handler: func(http.ResponseWriter, *http.Request) {}})
		},
		Routes: func(r nucleus.Router, _ notesConfig) {
			r.Get("/items", ok)
			r.Get("/items/{id}", ok)
			r.Post("/items", ok)
		},
	}
	if mutate != nil {
		mutate(&m)
	}
	return m.Build()
}

// errRecorder stands in for the test, so a module that fails can be checked
// without failing this one.
type errRecorder struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (r *errRecorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func failing(checks []nucleus.ModuleCheck) []string {
	var out []string
	for _, c := range checks {
		if c.Err != nil {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}

func TestCheckModulePassesAWellFormedModule(t *testing.T) {
	checks := nucleustest.CheckModule(t, notesModule(nil))
	var names []string
	for _, c := range checks {
		names = append(names, c.Name)
	}
	want := "name depends-on prefix config requires policies csrf-exempt templates models start migrations jobs webhooks routes shutdown"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("checks ran: %s\nwant:       %s", got, want)
	}
}

// Every defect at once, each one named by its own check: boot would have
// reported the configuration and stopped.
//
// A row or an exemption that is about no route is judged against the
// routes, so it gets its own test: here the routes fail to register.
func TestCheckModuleNamesEveryDefectAtOnce(t *testing.T) {
	rec := &errRecorder{TB: t}
	checks := nucleustest.CheckModule(rec, notesModule(func(m *nucleus.Module[notesConfig]) {
		m.Config.Mode = "medium"
		m.Policies = append(m.Policies, nucleus.PolicyRule{Subject: "anonymous", Object: "/items", Action: "get"})
		m.CSRFExempt = []string{"items"}
		m.Migrations = fstest.MapFS{"000001_broken.up.sql": {Data: []byte("CREATE TABLE (")}, "000001_broken.down.sql": {Data: []byte("")}}
		m.OnStart = nil
		m.OnShutdown = func(context.Context, nucleus.Runtime, notesConfig) error { return errors.New("left a file open") }
		routes := m.Routes
		m.Routes = func(r nucleus.Router, c notesConfig) {
			routes(r, c)
			r.Get("/items", func(c *nucleus.Context) error { return nil })
		}
	}))
	want := []string{"config", "csrf-exempt", "migrations", "policies", "routes", "shutdown"}
	if got := failing(checks); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("failing checks %v, want %v\n%v", got, want, checks)
	}
	if len(rec.errs) != len(want) {
		t.Errorf("the kit reported %d failures for %d failing checks:\n%s", len(rec.errs), len(want), strings.Join(rec.errs, "\n"))
	}
	for _, e := range rec.errs {
		if !strings.Contains(e, `module "notes" fails the `) {
			t.Errorf("a failure must name the module and the check: %s", e)
		}
	}
}

func TestCheckModuleInUsesTheApplicationsDatabases(t *testing.T) {
	needsReports := notesModule(func(m *nucleus.Module[notesConfig]) { m.Requires = []string{"reports"} })

	rec := &errRecorder{TB: t}
	if got := failing(nucleustest.CheckModule(rec, needsReports)); strings.Join(got, ",") != "requires" {
		t.Errorf("without the database: failing %v, want [requires]", got)
	}

	dbs := nucleustest.TempSQLite(t)
	dbs["reports"] = app.DatabaseConfig{URL: dbs["default"].URL + ".reports"}
	nucleustest.CheckModuleIn(t, nucleus.New().WithDatabases(dbs), needsReports)
}

func TestCheckModuleInRefusesAnotherModulesName(t *testing.T) {
	rec := &errRecorder{TB: t}
	other := nucleus.Module[struct{}]{Name: "notes"}.Build()
	got := failing(nucleustest.CheckModuleIn(rec, nucleus.New().Mount(other), notesModule(nil)))
	if strings.Join(got, ",") != "name" {
		t.Errorf("failing %v, want [name]", got)
	}
}

// A row and an exemption about routes the module does not serve load
// cleanly at boot and never apply; the kit says so.
func TestCheckModuleNamesDeclarationsAboutNothing(t *testing.T) {
	rec := &errRecorder{TB: t}
	checks := nucleustest.CheckModule(rec, notesModule(func(m *nucleus.Module[notesConfig]) {
		m.Policies = append(m.Policies, nucleus.PolicyRule{Subject: "anonymous", Object: "/archive", Action: "read"})
		m.CSRFExempt = append(m.CSRFExempt, "/elsewhere")
	}))
	if got := failing(checks); strings.Join(got, ",") != "csrf-exempt,policies" {
		t.Fatalf("failing %v, want [csrf-exempt policies]\n%v", got, checks)
	}
	for _, c := range checks {
		switch c.Name {
		case "policies":
			if !strings.Contains(c.Err.Error(), "Policies[2]") || !strings.Contains(c.Err.Error(), "/notes/archive") {
				t.Errorf("the policies verdict must name the row and where it resolves: %v", c.Err)
			}
		case "csrf-exempt":
			if !strings.Contains(c.Err.Error(), "CSRFExempt[1]") || !strings.Contains(c.Err.Error(), "/notes/elsewhere") {
				t.Errorf("the csrf-exempt verdict must name the exemption and where it resolves: %v", c.Err)
			}
		}
	}
}
