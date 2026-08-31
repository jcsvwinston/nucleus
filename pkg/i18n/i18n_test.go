package i18n

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeBundle writes a compiled JSON bundle in the exact layout and format
// `nucleus compilemessages` produces: <dir>/<locale>/LC_MESSAGES/<domain>.json.
func writeBundle(t *testing.T, dir, locale, domain, entriesJSON string) {
	t.Helper()
	bundleDir := filepath.Join(dir, locale, "LC_MESSAGES")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bundleDir, err)
	}
	payload := `{"locale":"` + locale + `","domain":"` + domain + `","entries":` + entriesJSON + `}`
	if err := os.WriteFile(filepath.Join(bundleDir, domain+".json"), []byte(payload), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
}

func newTestTranslator(t *testing.T) *Translator {
	t.Helper()
	dir := t.TempDir()
	writeBundle(t, dir, "en", "messages", `{"greeting":"Hello","farewell":"Goodbye","items":"%d items"}`)
	writeBundle(t, dir, "es", "messages", `{"greeting":"Hola","items":"%d elementos"}`)
	writeBundle(t, dir, "es_MX", "messages", `{"greeting":"Qué onda"}`)
	catalog, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return New(catalog, "en")
}

func TestLoad_MissingDirIsEmptyCatalog(t *testing.T) {
	catalog, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load on missing dir: %v", err)
	}
	if got := len(catalog.Locales()); got != 0 {
		t.Fatalf("expected empty catalog, got %d locales", got)
	}
}

func TestLoad_CorruptBundleFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "en", "LC_MESSAGES")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "messages.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error on corrupt bundle, got nil")
	}
}

func TestLoad_MergesDomainsAndListsLocales(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "en", "messages", `{"greeting":"Hello"}`)
	writeBundle(t, dir, "en", "errors", `{"not_found":"Not found"}`)
	catalog, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := catalog.Locales(); len(got) != 1 || got[0] != "en" {
		t.Fatalf("Locales() = %v, want [en]", got)
	}
	if v, ok := catalog.Lookup("en", "greeting"); !ok || v != "Hello" {
		t.Fatalf("Lookup(greeting) = %q, %v", v, ok)
	}
	if v, ok := catalog.Lookup("en", "not_found"); !ok || v != "Not found" {
		t.Fatalf("Lookup(not_found) = %q, %v", v, ok)
	}
}

func TestTranslator_FallbackChain(t *testing.T) {
	tr := newTestTranslator(t)
	cases := []struct {
		name   string
		locale string
		key    string
		want   string
	}{
		{"exact locale", "es", "greeting", "Hola"},
		{"regional exact (underscore dir, hyphen tag)", "es-MX", "greeting", "Qué onda"},
		{"regional falls back to base", "es-MX", "items", "%d elementos"},
		{"missing in locale falls back to default", "es", "farewell", "Goodbye"},
		{"unknown locale falls back to default", "fr", "greeting", "Hello"},
		{"unknown key falls back to the key", "es", "no_such_key", "no_such_key"},
		{"empty locale uses default", "", "greeting", "Hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tr.T(tc.locale, tc.key); got != tc.want {
				t.Fatalf("T(%q, %q) = %q, want %q", tc.locale, tc.key, got, tc.want)
			}
		})
	}
}

func TestTranslator_FormatsArgs(t *testing.T) {
	tr := newTestTranslator(t)
	if got := tr.T("es", "items", 3); got != "3 elementos" {
		t.Fatalf("T(es, items, 3) = %q", got)
	}
	if got := tr.T("en", "items", 3); got != "3 items" {
		t.Fatalf("T(en, items, 3) = %q", got)
	}
}

func TestNegotiate(t *testing.T) {
	tr := newTestTranslator(t)
	cases := []struct {
		header string
		want   string
	}{
		{"es", "es"},
		{"es-MX", "es-mx"},
		{"es-AR", "es"},              // base-language fallback
		{"fr, es;q=0.8", "es"},       // first acceptable available wins
		{"fr", "en"},                 // nothing available -> default
		{"", "en"},                   // no header -> default
		{"*", "en"},                  // wildcard -> default
		{"es;q=0, en;q=0.5", "en"},   // q=0 means not acceptable
		{"en;q=0.5, es;q=0.9", "es"}, // quality ordering
		{"ES_mx", "es-mx"},           // case/underscore folding
	}
	for _, tc := range cases {
		if got := tr.Negotiate(tc.header); got != tc.want {
			t.Errorf("Negotiate(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestContextT_WithoutTranslatorDegradesToKey(t *testing.T) {
	ctx := t.Context()
	if got := T(ctx, "greeting"); got != "greeting" {
		t.Fatalf("T without translator = %q, want key", got)
	}
	if got := T(ctx, "%d items", 2); got != "2 items" {
		t.Fatalf("T without translator formats args: got %q", got)
	}
}

func TestMiddleware_NegotiatesAndInjects(t *testing.T) {
	tr := newTestTranslator(t)
	handler := tr.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := Locale(r.Context()); got != "es" {
			t.Errorf("Locale(ctx) = %q, want es", got)
		}
		_, _ = w.Write([]byte(T(r.Context(), "greeting")))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "es, en;q=0.5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != "Hola" {
		t.Fatalf("body = %q, want Hola", body)
	}
	if got := rec.Header().Get("Content-Language"); got != "es" {
		t.Fatalf("Content-Language = %q, want es", got)
	}
}

func TestMiddleware_FallsBackToDefaultLocale(t *testing.T) {
	tr := newTestTranslator(t)
	handler := tr.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(T(r.Context(), "greeting")))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de-DE")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != "Hello" {
		t.Fatalf("body = %q, want Hello (default locale)", body)
	}
}
