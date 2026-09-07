package shop_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/nucleus/examples/showcase_demo/shop"

	// The test boots a real application, so the test binary links the
	// driver modules the way main.go does — the duplicate-title probe below
	// only answers 409 when Quark's classifier is registered.
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
	_ "github.com/jcsvwinston/quark/drivers/sqlite"
)

// TestShopServesArticlesThroughQuark mounts the module on an in-process
// application and drives it over HTTP: Quark migrates and seeds the schema,
// the module's own policy rows and CSRF exemption are in force, the JSON API
// lists and creates, and a duplicate title answers 409. Nothing here is
// faked — it is the same boot path main.go runs.
func TestShopServesArticlesThroughQuark(t *testing.T) {
	if testing.Short() {
		t.Skip("boots an in-process application; skipped with -short")
	}
	ctx := context.Background()

	databases, driver, dsn := testDatabases(t)
	client, err := quark.New(driver, dsn)
	if err != nil {
		t.Fatalf("quark client: %v", err)
	}
	defer client.Close()
	if err := shop.Migrate(ctx, client); err != nil {
		t.Fatalf("migrate/seed: %v", err)
	}

	srv := nucleustest.Start(t, nucleus.New().
		WithDatabases(databases).
		Mount(shop.Module(client)))

	body := get(t, srv, "/api/articles", http.StatusOK)
	if !strings.Contains(body, `"Hello, Quantum"`) {
		t.Errorf("GET /api/articles: want the seeded article, got %s", body)
	}

	// The module's create row lets an anonymous caller POST (a development
	// default; see the Policies in module.go) and /api/ is CSRF-exempt, so
	// the write lands without a token.
	created := post(t, srv, `{"author_id":1,"title":"probe","body":"from the test"}`, http.StatusCreated)
	if !strings.Contains(created, `"probe"`) {
		t.Errorf("POST /api/articles: want the created article echoed, got %s", created)
	}
	post(t, srv, `{"author_id":1,"title":"probe","body":"duplicate title"}`, http.StatusConflict)

	if body := get(t, srv, "/api/articles", http.StatusOK); !strings.Contains(body, `"probe"`) {
		t.Errorf("GET /api/articles after the POST: want the new article listed, got %s", body)
	}
}

func get(t *testing.T, srv *nucleustest.Server, path string, want int) string {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL(path))
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("GET %s: want %d, got %d body=%s", path, want, resp.StatusCode, body)
	}
	return string(body)
}

func post(t *testing.T, srv *nucleustest.Server, payload string, want int) string {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL("/api/articles"), "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/articles: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("POST /api/articles: want %d, got %d body=%s", want, resp.StatusCode, body)
	}
	return string(body)
}

// testDatabases gives the application and Quark one temporary SQLite file
// each test, the way main.go shares app.db between them.
func testDatabases(t *testing.T) (map[string]app.DatabaseConfig, string, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "shop_test.db")
	return map[string]app.DatabaseConfig{"default": {URL: "sqlite://" + file}}, "sqlite", file
}
