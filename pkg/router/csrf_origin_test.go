package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AN-06: the Sec-Fetch-Site layer (layer 1 of the two-layer CSRF design,
// enabled by csrf_enabled through router.WithCSRF) shipped without a single
// security test — neither the cross-site rejection nor the deliberate
// same-origin bypass of the token layer was pinned. These tests freeze both
// behaviours so a refactor cannot silently invert them.

func originCheckHandler(t *testing.T, opts CSRFOptions) http.Handler {
	t.Helper()
	mw, err := NewCSRFMiddleware(opts)
	if err != nil {
		t.Fatalf("NewCSRFMiddleware: %v", err)
	}
	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

// A cross-site POST without a token must be rejected: 419 when the token
// fallback is active, 403 in origin-only mode.
func TestCSRFOriginCheckRejectsCrossSite(t *testing.T) {
	cases := []struct {
		name       string
		opts       CSRFOptions
		wantStatus int
	}{
		{
			name:       "token fallback answers 419",
			opts:       CSRFOptions{EnableOriginCheck: true, InsecureCookie: true},
			wantStatus: 419,
		},
		{
			name:       "origin-only answers 403",
			opts:       CSRFOptions{EnableOriginCheck: true, OriginOnly: true, InsecureCookie: true},
			wantStatus: http.StatusForbidden,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := originCheckHandler(t, tc.opts)
			req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("cross-site POST status = %d; want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// A same-origin POST bypasses the token layer entirely — that bypass is the
// design (browsers set Sec-Fetch-Site themselves and same-origin requests
// cannot be forged cross-site by a browser), and it is pinned here ON
// PURPOSE: if a refactor ever makes same-origin requests demand a token, or
// makes the bypass apply to values other than exactly "same-origin", this
// test is the alarm.
func TestCSRFOriginCheckSameOriginBypassesTokenLayer(t *testing.T) {
	handler := originCheckHandler(t, CSRFOptions{EnableOriginCheck: true, InsecureCookie: true})

	req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST without token status = %d; want 200 (layer-1 bypass)", rec.Code)
	}

	// The bypass is EXACT-VALUE: "same-site" is not "same-origin" and must
	// fall through to the token layer unless AllowSameSite is set.
	req = httptest.NewRequest(http.MethodPost, "/transfer", nil)
	req.Header.Set("Sec-Fetch-Site", "same-site")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 419 {
		t.Fatalf("same-site POST without token and without AllowSameSite status = %d; want 419", rec.Code)
	}
}

func TestCSRFOriginCheckAllowSameSite(t *testing.T) {
	handler := originCheckHandler(t, CSRFOptions{EnableOriginCheck: true, AllowSameSite: true, InsecureCookie: true})
	req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
	req.Header.Set("Sec-Fetch-Site", "same-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-site POST with AllowSameSite status = %d; want 200", rec.Code)
	}
}

// A request with NO Sec-Fetch-Site header (curl, an old proxy, a non-browser
// client) is not treated as same-origin: it falls through to the token
// layer. The header is a browser promise, and its absence must not be an
// allow.
func TestCSRFOriginCheckMissingHeaderFallsToTokenLayer(t *testing.T) {
	handler := originCheckHandler(t, CSRFOptions{EnableOriginCheck: true, InsecureCookie: true})
	req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 419 {
		t.Fatalf("POST without Sec-Fetch-Site and without token status = %d; want 419", rec.Code)
	}
}

// A non-browser attacker CAN forge Sec-Fetch-Site: same-origin and skip the
// token layer — pinned deliberately, because layer 1 defends against the
// browser-based CSRF class only (a client that can set arbitrary headers
// already holds the ambient credentials problem by definition). This test
// documents the boundary rather than pretending it does not exist.
func TestCSRFOriginCheckForgedHeaderIsAcceptedByDesign(t *testing.T) {
	handler := originCheckHandler(t, CSRFOptions{EnableOriginCheck: true, InsecureCookie: true})
	req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin") // forged by a non-browser client
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("forged same-origin POST status = %d; want 200 (layer-1 scope is browser CSRF only)", rec.Code)
	}
}

// NC-11: a CSRF rejection leaves a hint in the log, like the authz 403s do.
func TestCSRFRejectionLogsHint(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	t.Run("419 token missing", func(t *testing.T) {
		buf.Reset()
		handler := originCheckHandler(t, CSRFOptions{InsecureCookie: true, Logger: logger})
		req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 419 {
			t.Fatalf("status = %d; want 419", rec.Code)
		}
		logged := buf.String()
		if !strings.Contains(logged, "csrf denied") || !strings.Contains(logged, "hint") {
			t.Fatalf("419 rejection left no hint in the log:\n%s", logged)
		}
		if !strings.Contains(logged, "/transfer") {
			t.Fatalf("419 rejection log does not name the path:\n%s", logged)
		}
	})

	t.Run("403 origin-only", func(t *testing.T) {
		buf.Reset()
		handler := originCheckHandler(t, CSRFOptions{EnableOriginCheck: true, OriginOnly: true, InsecureCookie: true, Logger: logger})
		req := httptest.NewRequest(http.MethodPost, "/transfer", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d; want 403", rec.Code)
		}
		logged := buf.String()
		if !strings.Contains(logged, "csrf denied") || !strings.Contains(logged, "hint") {
			t.Fatalf("403 rejection left no hint in the log:\n%s", logged)
		}
	})
}
