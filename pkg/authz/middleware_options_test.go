package authz

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

const optsTestSecret = "test-secret-key-for-authz-test!!"

// RequireRoleWithOptions: an authenticated user lacking the role triggers
// OnDeny instead of the default JSON envelope, with a 403/Authenticated Denial.
func TestRequireRoleWithOptions_OnDeny_Forbidden(t *testing.T) {
	e := newTestEnforcer(t)
	jwtMgr := auth.NewJWTManager(optsTestSecret, time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "user") // role "user", not "admin"

	var got Denial
	called := false
	opts := AuthzOptions{OnDeny: func(w http.ResponseWriter, r *http.Request, d Denial) {
		called = true
		got = d
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("styled-403-page"))
	}}

	handler := jwtMgr.Middleware()(
		e.RequireRoleWithOptions(opts, "admin")(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called on denial")
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("OnDeny was not invoked")
	}
	if !got.Authenticated {
		t.Error("Denial.Authenticated should be true for a signed-in user")
	}
	if got.Status != http.StatusForbidden {
		t.Errorf("Denial.Status = %d, want 403", got.Status)
	}
	if got.Reason != "insufficient role" {
		t.Errorf("Denial.Reason = %q, want %q", got.Reason, "insufficient role")
	}
	if w.Body.String() != "styled-403-page" {
		t.Errorf("body = %q, want the custom page (default JSON envelope leaked?)", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "application/json; charset=utf-8" {
		t.Error("default JSON envelope was written despite OnDeny")
	}
}

// RequireRoleWithOptions: an anonymous request triggers OnDeny with a
// 401/!Authenticated Denial, letting an SSR app redirect to login.
func TestRequireRoleWithOptions_OnDeny_RedirectsAnonymous(t *testing.T) {
	e := newTestEnforcer(t)

	var got Denial
	opts := AuthzOptions{OnDeny: func(w http.ResponseWriter, r *http.Request, d Denial) {
		got = d
		if !d.Authenticated {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}}

	// No JWT middleware in the chain → no claims in context.
	handler := e.RequireRoleWithOptions(opts, "admin")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called for an anonymous request")
		}),
	)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got.Authenticated {
		t.Error("Denial.Authenticated should be false for an anonymous request")
	}
	if got.Status != http.StatusUnauthorized {
		t.Errorf("Denial.Status = %d, want 401", got.Status)
	}
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (redirect to login)", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

// RequireRoleWithOptions: when the user is allowed, OnDeny must NOT fire and the
// wrapped handler runs.
func TestRequireRoleWithOptions_Allowed_DoesNotCallOnDeny(t *testing.T) {
	e := newTestEnforcer(t)
	jwtMgr := auth.NewJWTManager(optsTestSecret, time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "admin")

	opts := AuthzOptions{OnDeny: func(w http.ResponseWriter, r *http.Request, d Denial) {
		t.Error("OnDeny should not be called when the user is allowed")
	}}

	reached := false
	handler := jwtMgr.Middleware()(
		e.RequireRoleWithOptions(opts, "admin")(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !reached {
		t.Error("wrapped handler was not reached for an allowed user")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// MiddlewareWithOptions: an authenticated user with no matching policy triggers
// OnDeny with a 403/Authenticated Denial.
func TestMiddlewareWithOptions_OnDeny_Forbidden(t *testing.T) {
	e := newTestEnforcer(t) // no policy for user-1
	jwtMgr := auth.NewJWTManager(optsTestSecret, time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "admin")

	var got Denial
	called := false
	opts := AuthzOptions{OnDeny: func(w http.ResponseWriter, r *http.Request, d Denial) {
		called = true
		got = d
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("styled-403-page"))
	}}

	handler := jwtMgr.Middleware()(
		e.MiddlewareWithOptions(opts)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called on denial")
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("OnDeny was not invoked")
	}
	if !got.Authenticated || got.Status != http.StatusForbidden {
		t.Errorf("Denial = %+v, want {Status:403 Authenticated:true}", got)
	}
	if w.Body.String() != "styled-403-page" {
		t.Errorf("body = %q, want the custom page", w.Body.String())
	}
}

// MiddlewareWithOptions: an anonymous request triggers OnDeny with a
// 401/!Authenticated Denial.
func TestMiddlewareWithOptions_OnDeny_Unauthenticated(t *testing.T) {
	e := newTestEnforcer(t)

	var got Denial
	called := false
	opts := AuthzOptions{OnDeny: func(w http.ResponseWriter, r *http.Request, d Denial) {
		called = true
		got = d
		w.WriteHeader(http.StatusUnauthorized)
	}}

	handler := e.MiddlewareWithOptions(opts)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called for an anonymous request")
		}),
	)

	r := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("OnDeny was not invoked")
	}
	if got.Authenticated || got.Status != http.StatusUnauthorized {
		t.Errorf("Denial = %+v, want {Status:401 Authenticated:false}", got)
	}
}

// A nil OnDeny (the zero AuthzOptions) must reproduce the default JSON-envelope
// behaviour exactly — same status, content-type AND reason string — so the
// WithOptions variants are safe drop-ins. This is the same contract the existing
// Middleware()/RequireRole() now delegate through. All four cells of the matrix
// (each middleware × {401 unauthenticated, 403 forbidden}) are covered.
func TestWithOptions_NilOnDeny_PreservesDefaultJSON(t *testing.T) {
	const jsonCT = "application/json; charset=utf-8"

	// assertDefaultEnvelope checks a denied response is the default JSON envelope
	// with the expected status and reason text in the body.
	assertDefaultEnvelope := func(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantReason string) {
		t.Helper()
		if w.Code != wantStatus {
			t.Errorf("status = %d, want %d", w.Code, wantStatus)
		}
		if ct := w.Header().Get("Content-Type"); ct != jsonCT {
			t.Errorf("Content-Type = %q, want JSON envelope %q", ct, jsonCT)
		}
		if !strings.Contains(w.Body.String(), wantReason) {
			t.Errorf("body = %q, want it to contain reason %q", w.Body.String(), wantReason)
		}
	}

	jwtMgr := auth.NewJWTManager(optsTestSecret, time.Hour)
	// role "user" — never satisfies a RequireRole("admin") and no policy grants
	// it /protected, so it is the "authenticated but forbidden" subject.
	token, _ := jwtMgr.Generate("user-1", "alice", "user")

	t.Run("RequireRole forbidden → 403 JSON", func(t *testing.T) {
		e := newTestEnforcer(t)
		handler := jwtMgr.Middleware()(
			e.RequireRoleWithOptions(AuthzOptions{}, "admin")(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("handler should not be called")
				}),
			),
		)
		r := httptest.NewRequest(http.MethodGet, "/admin", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertDefaultEnvelope(t, w, http.StatusForbidden, "insufficient role")
	})

	t.Run("RequireRole unauthenticated → 401 JSON", func(t *testing.T) {
		e := newTestEnforcer(t)
		// No JWT middleware → no claims in context.
		handler := e.RequireRoleWithOptions(AuthzOptions{}, "admin")(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			}),
		)
		r := httptest.NewRequest(http.MethodGet, "/admin", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertDefaultEnvelope(t, w, http.StatusUnauthorized, "authentication required")
	})

	t.Run("Middleware forbidden → 403 JSON", func(t *testing.T) {
		e := newTestEnforcer(t) // no policy for user-1 → Can() denies
		handler := jwtMgr.Middleware()(
			e.MiddlewareWithOptions(AuthzOptions{})(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("handler should not be called")
				}),
			),
		)
		r := httptest.NewRequest(http.MethodGet, "/protected", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertDefaultEnvelope(t, w, http.StatusForbidden, "you do not have permission to perform this action")
	})

	t.Run("Middleware unauthenticated → 401 JSON", func(t *testing.T) {
		e := newTestEnforcer(t)
		handler := e.MiddlewareWithOptions(AuthzOptions{})(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			}),
		)
		r := httptest.NewRequest(http.MethodGet, "/protected", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertDefaultEnvelope(t, w, http.StatusUnauthorized, "authentication required")
	})
}
