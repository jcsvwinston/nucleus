// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// E2E for Runtime.ApplyModuleMigrations (ADR-022 §2, the ADR-013 §R1
// follow-up): a module's EMBEDDED migrations become live schema through one
// deliberate OnStart call — module-scoped ledger, idempotent across process
// restarts — with zero disk migration files and zero manual `nucleus
// migrate` steps. The framework itself still never applies anything: the
// call below is module code.
package nucleus

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func shopMigrations() fstest.MapFS {
	return fstest.MapFS{
		"000001_create_shop_items.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE shop_items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")},
		"000001_create_shop_items.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE IF EXISTS shop_items;")},
	}
}

func TestRun_ApplyModuleMigrationsEndToEnd(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "shop.db")

	boot := func(t *testing.T) (base string, done chan error) {
		t.Helper()
		port := freeLocalPort(t)

		var shopDB *sql.DB
		modDef := Module[struct{}]{
			Name:       "shop",
			Migrations: shopMigrations(),
			Policies: []PolicyRule{
				{Subject: "anonymous", Object: "/items", Action: "read"},
				{Subject: "anonymous", Object: "/items", Action: "create"},
				{Subject: "anonymous", Object: "/ledger", Action: "read"},
			},
			CSRFExempt: []string{"/items"},
			OnStart: func(ctx context.Context, rt Runtime, _ struct{}) error {
				// The deliberate call: embedded SQL -> live schema through
				// the module-scoped ledger. Idempotent on the second boot.
				if err := rt.ApplyModuleMigrations(); err != nil {
					return err
				}
				shopDB = rt.DB()
				return nil
			},
			Routes: func(r Router, _ struct{}) {
				r.Post("/items", func(c *Context) error {
					if _, err := shopDB.Exec("INSERT INTO shop_items (name) VALUES ('demo')"); err != nil {
						return c.String(http.StatusInternalServerError, err.Error())
					}
					return c.JSON(http.StatusOK, map[string]string{"created": "true"})
				})
				r.Get("/items", func(c *Context) error {
					var n int
					if err := shopDB.QueryRow("SELECT COUNT(*) FROM shop_items").Scan(&n); err != nil {
						return c.String(http.StatusInternalServerError, err.Error())
					}
					return c.String(http.StatusOK, fmt.Sprintf("%d", n))
				})
				r.Get("/ledger", func(c *Context) error {
					var n int
					if err := shopDB.QueryRow("SELECT COUNT(*) FROM nucleus_schema_migrations WHERE id LIKE 'shop/%'").Scan(&n); err != nil {
						return c.String(http.StatusInternalServerError, err.Error())
					}
					return c.String(http.StatusOK, fmt.Sprintf("%d", n))
				})
			},
		}

		cfg := app.DefaultConfig()
		cfg.Host = "127.0.0.1"
		cfg.Port = port
		cfg.CSRFEnabled = true
		cfg.Databases = map[string]app.DatabaseConfig{
			"default": {URL: "sqlite://" + dbFile},
		}

		done = make(chan error, 1)
		go func() {
			done <- Run(App{
				Config:  cfg,
				Modules: map[string]ModuleSpec{"shop": modDef.Build()},
			})
		}()
		base = fmt.Sprintf("http://127.0.0.1:%d", port)
		waitForServer(t, &http.Client{Timeout: 2 * time.Second}, base, done)
		return base, done
	}

	client := &http.Client{Timeout: 2 * time.Second}
	fetch := func(base, path string) (int, string) {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := make([]byte, 64)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(body[:n]))
	}

	// First boot: schema comes into existence from the embedded FS.
	base, done := boot(t)
	if resp, err := client.Post(base+"/items", "application/json", strings.NewReader(`{}`)); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /items on fresh embedded schema: %v (status %v)", err, resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if code, body := fetch(base, "/items"); code != http.StatusOK || body != "1" {
		t.Fatalf("GET /items: want 200/1, got %d/%q", code, body)
	}
	// The ledger row is module-namespaced: shop/000001_create_shop_items.
	if code, body := fetch(base, "/ledger"); code != http.StatusOK || body != "1" {
		t.Fatalf("GET /ledger: want 200/1 namespaced row, got %d/%q", code, body)
	}
	shutDownRunApp(t, done)

	// Second boot over the SAME database file: ApplyModuleMigrations must be
	// idempotent (ledger says applied; nothing re-runs, boot succeeds) and
	// the data survives.
	base, done = boot(t)
	if code, body := fetch(base, "/items"); code != http.StatusOK || body != "1" {
		t.Fatalf("after restart: want the row to survive (200/1), got %d/%q", code, body)
	}
	if code, body := fetch(base, "/ledger"); code != http.StatusOK || body != "1" {
		t.Fatalf("after restart: want exactly 1 ledger row (no re-apply), got %d/%q", code, body)
	}
	shutDownRunApp(t, done)
}

// A runtime not bound to a module (the bare newRuntime of unit tests) and a
// module without embedded Migrations both get clear errors, not surprises.
func TestApplyModuleMigrations_ErrorPaths(t *testing.T) {
	if err := (runtime{}).ApplyModuleMigrations(); err == nil || !strings.Contains(err.Error(), "unbacked") {
		t.Fatalf("unbacked runtime: want error, got %v", err)
	}
	rt := runtime{core: &app.App{}}
	if err := rt.ApplyModuleMigrations(); err == nil || !strings.Contains(err.Error(), "not bound to a module") {
		t.Fatalf("module-less runtime: want error, got %v", err)
	}
	rt = runtime{core: &app.App{}, moduleName: "shop"}
	if err := rt.ApplyModuleMigrations(); err == nil || !strings.Contains(err.Error(), "declares no embedded Migrations") {
		t.Fatalf("no-FS module: want the actionable error, got %v", err)
	}
}
