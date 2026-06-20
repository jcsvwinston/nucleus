package nucleus

import (
	"net/http"
	"net/http/httptest"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// Mount attaches an http.Handler subtree under the module prefix, stripping the
// prefix so the mounted handler sees paths relative to its mount point — the
// orbit admin-panel mount case (mirrors the framework's own app.MountAdmin).
func TestRouterMount_ServesSubtreeUnderPrefixStripped(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "/admin")

	var gotPath string
	sub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	a.Mount("/", sub) // "/" joins to the module prefix → mounts at /admin

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/api/models: want 200, got %d", rec.Code)
	}
	if gotPath != "/api/models" {
		t.Errorf("mounted handler saw %q, want the prefix-stripped %q", gotPath, "/api/models")
	}
}

// Mount at a named pattern on a prefix-less adapter mounts the subtree there.
func TestRouterMount_AtNamedPattern(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")

	hit := false
	a.Mount("/files", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.URL.Path != "/app.css" {
			t.Errorf("mounted handler saw %q, want /app.css (prefix stripped)", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/app.css", nil))
	if rec.Code != http.StatusOK || !hit {
		t.Fatalf("GET /files/app.css: want 200+hit, got %d (hit=%v)", rec.Code, hit)
	}
}

// A request for the bare mount pattern (no trailing slash) is 307-redirected to
// the canonical pattern/ — orbit relies on GET /admin → /admin/.
func TestRouterMount_BarePatternRedirects(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")
	a.Mount("/panel", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panel", nil))
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET /panel (bare): want 307, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/panel/" {
		t.Errorf("Location = %q, want /panel/", loc)
	}
}

// Module/With middleware wraps the mounted handler (the guard-the-mount case).
func TestRouterMount_AppliesWithMiddleware(t *testing.T) {
	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")

	guardRan := false
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			guardRan = true
			next.ServeHTTP(w, r)
		})
	}
	a.With(guard).Mount("/panel", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panel/x", nil))
	if rec.Code != http.StatusOK || !guardRan {
		t.Fatalf("With(guard).Mount: GET /panel/x want 200+guardRan, got %d (guardRan=%v)", rec.Code, guardRan)
	}
}
