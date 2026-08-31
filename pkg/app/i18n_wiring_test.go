package app

// Wiring tests for the i18n runtime (audit PR-GAP-03/NF-4): app.New must
// load the compiled catalogs `nucleus compilemessages` writes under
// `locales_path`, mount the Accept-Language negotiation middleware, and let
// handlers translate through c.T(...). Before this wiring existed the
// `default_locale`/`locales_path` keys were read only by the CLI.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// writeCompiledBundle writes a JSON bundle in the exact layout and format
// `nucleus compilemessages` produces.
func writeCompiledBundle(t *testing.T, localesDir, locale, entriesJSON string) {
	t.Helper()
	dir := filepath.Join(localesDir, locale, "LC_MESSAGES")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"locale":"` + locale + `","domain":"messages","entries":` + entriesJSON + `}`
	if err := os.WriteFile(filepath.Join(dir, "messages.json"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAppNew_I18nMiddlewareTranslatesRequests(t *testing.T) {
	localesDir := t.TempDir()
	writeCompiledBundle(t, localesDir, "en", `{"greeting":"Hello"}`)
	writeCompiledBundle(t, localesDir, "es", `{"greeting":"Hola"}`)

	cfg := testAppConfig()
	cfg.LocalesPath = localesDir
	cfg.DefaultLocale = "en"

	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	if a.I18n == nil {
		t.Fatal("App.I18n is nil despite compiled catalogs under locales_path")
	}

	a.Router.Get("/greet", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"msg": c.T("greeting")})
	})

	cases := []struct {
		acceptLanguage string
		wantBody       string
		wantLang       string
	}{
		{"es", `{"msg":"Hola"}`, "es"},
		{"es-MX, en;q=0.5", `{"msg":"Hola"}`, "es"},
		{"de", `{"msg":"Hello"}`, "en"},
		{"", `{"msg":"Hello"}`, "en"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/greet", nil)
		if tc.acceptLanguage != "" {
			req.Header.Set("Accept-Language", tc.acceptLanguage)
		}
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Accept-Language=%q: status %d body=%s", tc.acceptLanguage, rec.Code, rec.Body.String())
		}
		if got := trimJSON(rec.Body.String()); got != tc.wantBody {
			t.Errorf("Accept-Language=%q: body %q, want %q", tc.acceptLanguage, got, tc.wantBody)
		}
		if got := rec.Header().Get("Content-Language"); got != tc.wantLang {
			t.Errorf("Accept-Language=%q: Content-Language %q, want %q", tc.acceptLanguage, got, tc.wantLang)
		}
	}
}

func trimJSON(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestAppNew_NoCatalogsMeansNoI18n(t *testing.T) {
	cfg := testAppConfig()
	cfg.LocalesPath = filepath.Join(t.TempDir(), "no-such-dir")

	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	if a.I18n != nil {
		t.Fatal("App.I18n must be nil when no compiled catalogs exist")
	}
	// And c.T must degrade to the key, not fail.
	a.Router.Get("/greet", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"msg": c.T("greeting")})
	})
	req := httptest.NewRequest(http.MethodGet, "/greet", nil)
	req.Header.Set("Accept-Language", "es")
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if got := trimJSON(rec.Body.String()); got != `{"msg":"greeting"}` {
		t.Fatalf("without catalogs c.T must return the key: %q", got)
	}
}

func TestAppNew_CorruptCatalogFailsStartup(t *testing.T) {
	localesDir := t.TempDir()
	dir := filepath.Join(localesDir, "en", "LC_MESSAGES")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "messages.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testAppConfig()
	cfg.LocalesPath = localesDir

	if _, err := New(cfg, WithOpenAuthz()); err == nil {
		t.Fatal("a corrupt compiled catalog must fail startup loudly")
	}
}
