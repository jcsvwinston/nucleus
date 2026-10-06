// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// NU-119: Stored and StoredKeys read with context.Background() through the
// application's TenantStore. In a multi-tenant application that is an
// operation without a tenant, so the kit's own read met the policy that
// exists for background jobs: by default it logged — and used up — the
// one-time WARN about the shared key space, and with
// multitenant.require_tenant_storage: true it failed the test with
// storage.ErrNoTenantInContext. And it looked in the shared key space, so a
// test could not read back the file a tenant's request had stored.
//
// Stored and StoredKeys now read below tenant scoping, and StoredFor and
// StoredKeysFor read one tenant's files by the keys its requests used.

// sharedKeySpaceWarn is the WARN the tenant policy logs the first time an
// operation without a tenant degrades to the shared key space.
const sharedKeySpaceWarn = "degraded to the SHARED (unprefixed) key space"

// kitDone separates, in the captured log, what the kit's reads logged from
// what the application's own operation logged afterwards.
const kitDone = "--- NU-119: kit reads done ---"

// filesApp stores each upload under uploads/<name>, through the
// application's own store and the request's context. multiTenant
// configures it like the site's multi-tenant tutorial: the tenant comes from
// the X-Tenant-ID header and every tenant shares the default database.
func filesApp(t *testing.T, multiTenant, strict bool) (nucleus.App, *runtimeProbe) {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.JWTSecret = strings.Repeat("nucleustest-secret-", 3)
	cfg.Databases = TempSQLite(t)
	cfg.Storage.Provider = "memory"
	cfg.LogFormat = "text"
	cfg.LogLevel = "warn"
	if multiTenant {
		cfg.MultiTenant = app.MultiTenantConfig{
			Enabled:              true,
			Resolver:             "header",
			Header:               "X-Tenant-ID",
			RequireTenantStorage: strict,
		}
	}
	rt := &runtimeProbe{}
	m := nucleus.Module[struct{}]{
		Name: "files",
		OnStart: func(_ context.Context, r nucleus.Runtime, _ struct{}) error {
			rt.rt.Store(r)
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Put("/files/{name}", func(c *nucleus.Context) error {
				store := rt.rt.Load().(nucleus.Runtime).Storage()
				_, err := store.Put(c.Request.Context(), "uploads/"+c.Param("name"), c.Request.Body, storage.PutOptions{ContentType: "text/plain"})
				if err != nil {
					return err
				}
				return c.NoContent()
			})
		},
	}.Build()
	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		Options: []app.Option{app.WithOpenAuthz()},
	}, rt
}

// upload stores body as uploads/<name> in a request for tenant ("" sends
// no tenant header).
func upload(t *testing.T, srv *Server, tenant, name, body string) {
	t.Helper()
	var opts []RequestOption
	if tenant != "" {
		opts = append(opts, WithHeader("X-Tenant-ID", tenant))
	}
	if r := srv.Put("/files/"+name, body, opts...); r.Status != http.StatusNoContent {
		t.Fatalf("PUT /files/%s as %q: %d %s", name, tenant, r.Status, r)
	}
}

// appLog redirects os.Stdout — the application builds its logger on it —
// for the rest of the test. Mark writes a line into the same stream, so the
// marks and what the application logged come out in the order they
// happened; Close restores os.Stdout and returns everything written.
type appLog struct {
	orig *os.File
	w    *os.File
	done chan string
	once sync.Once
	out  string
}

func captureAppLog(t *testing.T) *appLog {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	l := &appLog{orig: os.Stdout, w: w, done: make(chan string, 1)}
	os.Stdout = w
	go func() {
		b, _ := io.ReadAll(r)
		l.done <- string(b)
	}()
	t.Cleanup(func() { l.Close() })
	return l
}

func (l *appLog) Mark(s string) { _, _ = fmt.Fprintln(l.w, s) }

func (l *appLog) Close() string {
	l.once.Do(func() {
		os.Stdout = l.orig
		_ = l.w.Close()
		l.out = <-l.done
	})
	return l.out
}

func linesWith(s, substr string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// kitFatal runs fn against a copy of srv whose failures are recorded
// instead of ending the test, and returns what the kit said ("" when it
// did not fail).
func kitFatal(t *testing.T, srv *Server, fn func(*Server)) string {
	t.Helper()
	rec := &recordingTB{TB: t}
	shadow := *srv
	shadow.tb = rec
	func() {
		defer func() { _ = recover() }()
		fn(&shadow)
	}()
	return rec.fatal
}

// In both modes the test reads each tenant's files by the keys its requests
// used, sees the whole store below tenant scoping, and the kit's reads leave
// the policy alone: no WARN, no failure — while the application's own
// operations without a tenant still meet it.
func TestStoredInAMultiTenantApplication(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("require_tenant_storage=%t", strict), func(t *testing.T) {
			log := captureAppLog(t)
			a, _ := filesApp(t, true, strict)
			srv := StartApp(t, a)
			upload(t, srv, "acme", "invoice.txt", "acme's invoice")
			upload(t, srv, "globex", "invoice.txt", "globex's invoice")
			upload(t, srv, "globex", "logo.txt", "globex's logo")

			if msg := kitFatal(t, srv, func(s *Server) {
				// One tenant, by the keys its requests used.
				if got := string(s.StoredFor("acme", "uploads/invoice.txt")); got != "acme's invoice" {
					t.Errorf(`StoredFor("acme", "uploads/invoice.txt") = %q`, got)
				}
				if got := string(s.StoredFor("globex", "uploads/invoice.txt")); got != "globex's invoice" {
					t.Errorf(`StoredFor("globex", "uploads/invoice.txt") = %q`, got)
				}
				if got := string(s.StoredFor(" ACME ", "uploads/invoice.txt")); got != "acme's invoice" {
					t.Errorf("StoredFor does not compare the tenant the way the resolver does: %q", got)
				}
				if got, want := s.StoredKeysFor("acme", ""), []string{"uploads/invoice.txt"}; !reflect.DeepEqual(got, want) {
					t.Errorf(`StoredKeysFor("acme", "") = %v; want %v`, got, want)
				}
				if got, want := s.StoredKeysFor("globex", "uploads/"), []string{"uploads/invoice.txt", "uploads/logo.txt"}; !reflect.DeepEqual(got, want) {
					t.Errorf(`StoredKeysFor("globex", "uploads/") = %v; want %v`, got, want)
				}
				if got := s.StoredKeysFor("initech", ""); len(got) != 0 {
					t.Errorf(`StoredKeysFor("initech", "") = %v; want nothing`, got)
				}
				// The whole store, as the backend holds it.
				if got := string(s.Stored("acme/uploads/invoice.txt")); got != "acme's invoice" {
					t.Errorf(`Stored("acme/uploads/invoice.txt") = %q`, got)
				}
				want := []string{"acme/uploads/invoice.txt", "globex/uploads/invoice.txt", "globex/uploads/logo.txt"}
				if got := s.StoredKeys(""); !reflect.DeepEqual(got, want) {
					t.Errorf(`StoredKeys("") = %v; want %v`, got, want)
				}
			}); msg != "" {
				t.Fatalf("the kit failed the test: %s", msg)
			}

			// A key the tenant never stored is a failure, not another
			// tenant's file.
			if msg := kitFatal(t, srv, func(s *Server) { s.StoredFor("acme", "uploads/logo.txt") }); !strings.Contains(msg, `StoredFor("acme", "uploads/logo.txt")`) {
				t.Errorf("StoredFor of a key acme never stored said %q", msg)
			}

			log.Mark(kitDone)
			// The application's own operation without a tenant still meets
			// the policy.
			_, err := srv.Runtime().Storage().List(context.Background(), storage.ListOptions{Limit: 1})
			if strict && !errors.Is(err, storage.ErrNoTenantInContext) {
				t.Errorf("the application's tenant-less List in strict mode: err = %v; want ErrNoTenantInContext", err)
			}
			if !strict && err != nil {
				t.Errorf("the application's tenant-less List in default mode: %v", err)
			}
			srv.Stop()

			kit, after, found := strings.Cut(log.Close(), kitDone)
			if !found {
				t.Fatalf("marker missing from the captured log:\n%s", kit)
			}
			if lines := linesWith(kit, sharedKeySpaceWarn); len(lines) != 0 {
				t.Errorf("the kit's reads tripped the tenant policy:\n%s", strings.Join(lines, "\n"))
			}
			if lines := linesWith(after, sharedKeySpaceWarn); !strict && len(lines) != 1 {
				t.Errorf("the application's own tenant-less List must still warn once, got %d lines:\n%s", len(lines), after)
			}
		})
	}
}

// A single-tenant application's keys carry no tenant: asking for one is a
// question the configuration cannot answer, and the kit says which helper
// reads them. Stored and StoredKeys read what they always read.
func TestStoredForInASingleTenantApplication(t *testing.T) {
	a, _ := filesApp(t, false, false)
	srv := StartApp(t, a)
	upload(t, srv, "", "invoice.txt", "the invoice")

	if got := string(srv.Stored("uploads/invoice.txt")); got != "the invoice" {
		t.Errorf(`Stored("uploads/invoice.txt") = %q`, got)
	}
	if got, want := srv.StoredKeys("uploads/"), []string{"uploads/invoice.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf(`StoredKeys("uploads/") = %v; want %v`, got, want)
	}
	msg := kitFatal(t, srv, func(s *Server) { s.StoredFor("acme", "uploads/invoice.txt") })
	if !strings.Contains(msg, "not multi-tenant") || !strings.Contains(msg, "read them with Stored") {
		t.Errorf("StoredFor in a single-tenant application said %q", msg)
	}
	msg = kitFatal(t, srv, func(s *Server) { s.StoredKeysFor("acme", "") })
	if !strings.Contains(msg, "not multi-tenant") || !strings.Contains(msg, "read them with StoredKeys") {
		t.Errorf("StoredKeysFor in a single-tenant application said %q", msg)
	}
}

// An empty tenant is the shared key space, which Stored already reads.
func TestStoredForRefusesAnEmptyTenant(t *testing.T) {
	a, _ := filesApp(t, true, false)
	srv := StartApp(t, a)
	msg := kitFatal(t, srv, func(s *Server) { s.StoredFor("  ", "uploads/invoice.txt") })
	if !strings.Contains(msg, "the tenant is empty") || !strings.Contains(msg, "what Stored reads") {
		t.Errorf("StoredFor with an empty tenant said %q", msg)
	}
}
