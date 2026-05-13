package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	JSON(w, http.StatusOK, data)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %s", ct)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["key"] != "value" {
		t.Errorf("expected value, got %s", result["key"])
	}
}

func TestError_DomainError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	err := gferrors.NotFound("User", "42")
	Error(w, r, err)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestNoContent(t *testing.T) {
	w := httptest.NewRecorder()
	NoContent(w)
	if w.Code != 204 {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestCreated(t *testing.T) {
	w := httptest.NewRecorder()
	Created(w, map[string]int{"id": 1})
	if w.Code != 201 {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

func TestBind_Valid(t *testing.T) {
	type input struct {
		Name string `json:"name" validate:"required"`
	}
	body := bytes.NewBufferString(`{"name":"Alice"}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	r.Header.Set("Content-Type", "application/json")

	var in input
	if err := Bind(r, &in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Name != "Alice" {
		t.Errorf("expected Alice, got %s", in.Name)
	}
}

func TestBind_InvalidJSON(t *testing.T) {
	body := bytes.NewBufferString(`{invalid}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	err := Bind(r, &struct{}{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBind_ValidationFailed(t *testing.T) {
	type input struct {
		Email string `json:"email" validate:"required,email"`
	}
	body := bytes.NewBufferString(`{"email":"not-email"}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	err := Bind(r, &input{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestBind_NilBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = nil
	err := Bind(r, &struct{}{})
	if err == nil {
		t.Fatal("expected error for nil body")
	}
}

func TestPaginate_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	page, size := Paginate(r, 25)
	if page != 1 || size != 25 {
		t.Errorf("expected page=1 size=25, got page=%d size=%d", page, size)
	}
}

func TestPaginate_QueryParams(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=3&page_size=50", nil)
	page, size := Paginate(r, 25)
	if page != 3 || size != 50 {
		t.Errorf("expected page=3 size=50, got page=%d size=%d", page, size)
	}
}

func TestPaginate_MaxPageSize(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page_size=500", nil)
	_, size := Paginate(r, 25)
	if size != 100 {
		t.Errorf("expected max size=100, got %d", size)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "0",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for name, expected := range headers {
		if got := w.Header().Get(name); got != expected {
			t.Errorf("header %s: expected %s, got %s", name, expected, got)
		}
	}
}

func TestCSRFMiddleware_GetRequest(t *testing.T) {
	handler := CSRFMiddleware(CSRFOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("GET should pass, got %d", w.Code)
	}
}

func TestCSRFMiddleware_PostWithoutToken(t *testing.T) {
	handler := CSRFMiddleware(CSRFOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 419 {
		t.Errorf("POST without token should be 419, got %d", w.Code)
	}
}

func TestCSRFMiddleware_PostWithValidToken(t *testing.T) {
	mw := CSRFMiddleware(CSRFOptions{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// First, do a GET to get the token cookie
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)

	// Extract the CSRF cookie
	cookies := getW.Result().Cookies()
	var csrfToken string
	for _, c := range cookies {
		if c.Name == "_csrf" {
			csrfToken = c.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("expected CSRF cookie to be set")
	}

	// POST with the token
	postReq := httptest.NewRequest(http.MethodPost, "/", nil)
	postReq.Header.Set("X-CSRF-Token", csrfToken)
	postReq.AddCookie(&http.Cookie{Name: "_csrf", Value: csrfToken})
	postW := httptest.NewRecorder()
	handler.ServeHTTP(postW, postReq)

	if postW.Code != 200 {
		t.Errorf("POST with valid token should be 200, got %d", postW.Code)
	}
}

func TestCSRFMiddleware_PostWithMismatchedToken(t *testing.T) {
	handler := CSRFMiddleware(CSRFOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-CSRF-Token", "token-a")
	r.AddCookie(&http.Cookie{Name: "_csrf", Value: "token-b"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 419 {
		t.Errorf("expected 419 for mismatched csrf token, got %d", w.Code)
	}
}

func TestCSRFMiddleware_ExemptPath(t *testing.T) {
	handler := CSRFMiddleware(CSRFOptions{ExemptPaths: []string{"/api/"}})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)

	r := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("exempt path should pass, got %d", w.Code)
	}
}

func TestNewRouter(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger)
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.Mux == nil {
		t.Fatal("Router.Mux is nil")
	}
}

func TestRateLimitMiddleware_BlocksAfterLimit(t *testing.T) {
	mw := RateLimitMiddleware(RateLimitOptions{
		Requests: 2,
		Window:   time.Minute,
		KeyFunc: func(*http.Request) string {
			return "same-client"
		},
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request #%d expected 200, got %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after limit, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRateLimitKeyFromRequest_UsesUserIDFromContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := observe.CtxWithUserID(req.Context(), "user-42")
	req = req.WithContext(ctx)

	key := rateLimitKeyFromRequest(req)
	if key != "user:user-42" {
		t.Fatalf("expected user key, got %q", key)
	}
}

func TestRateLimitKeyFromRequest_PrependsTenantWhenPresent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := observe.CtxWithTenantID(req.Context(), "acme")
	ctx = observe.CtxWithUserID(ctx, "user-42")
	req = req.WithContext(ctx)

	key := rateLimitKeyFromRequest(req)
	if key != "tenant:acme|user:user-42" {
		t.Fatalf("expected tenant-prefixed user key, got %q", key)
	}
}

func TestRateLimitKeyFromRequest_PrependsTenantOverIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:50000"
	ctx := observe.CtxWithTenantID(req.Context(), "acme")
	req = req.WithContext(ctx)

	key := rateLimitKeyFromRequest(req)
	if key != "tenant:acme|ip:203.0.113.7" {
		t.Fatalf("expected tenant-prefixed ip key, got %q", key)
	}
}

// TestRateLimitMiddleware_SameUserDifferentTenantsHaveSeparateBuckets is the
// regression test for audit discrepancy D5: the README promises a
// "rate-limit per-tenant" surface, so two requests carrying the same
// user_id but distinct tenant_ids must not share a bucket. Before the fix
// the bucket key was "user:<id>" regardless of tenant, which silently
// merged traffic across tenants.
func TestRateLimitMiddleware_SameUserDifferentTenantsHaveSeparateBuckets(t *testing.T) {
	mw := RateLimitMiddleware(RateLimitOptions{
		Requests: 1,
		Window:   time.Minute,
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	send := func(tenant string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := observe.CtxWithTenantID(req.Context(), tenant)
		ctx = observe.CtxWithUserID(ctx, "shared-user")
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := send("tenant-a"); got != http.StatusOK {
		t.Fatalf("tenant-a first request: expected 200, got %d", got)
	}
	if got := send("tenant-b"); got != http.StatusOK {
		t.Fatalf("tenant-b first request: expected 200 (separate bucket), got %d", got)
	}
	if got := send("tenant-a"); got != http.StatusTooManyRequests {
		t.Fatalf("tenant-a second request: expected 429 (bucket exhausted), got %d", got)
	}
	if got := send("tenant-b"); got != http.StatusTooManyRequests {
		t.Fatalf("tenant-b second request: expected 429 (bucket exhausted), got %d", got)
	}
}

func TestCORSMiddleware_DisallowsUnknownOriginWhenOriginsConfigured(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger, WithCORSOrigins("https://allowed.example"))
	r.Get("/test", func(c *Context) error {
		c.Writer.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header for disallowed origin, got %q", got)
	}
}

func TestCORSMiddleware_AllowsConfiguredOrigin(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	r := New(logger, WithCORSOrigins("https://allowed.example"))
	r.Get("/test", func(c *Context) error {
		c.Writer.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://allowed.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://allowed.example" {
		t.Fatalf("expected ACAO header for allowed origin, got %q", got)
	}
}

func TestRateLimitMiddleware_BurstAllowsTemporarySpike(t *testing.T) {
	mw := RateLimitMiddleware(RateLimitOptions{
		Requests: 1,
		Window:   time.Minute,
		Burst:    2,
		KeyFunc: func(*http.Request) string {
			return "burst-client"
		},
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request #%d expected 200, got %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected request beyond burst to be 429, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_ScopeByRoute(t *testing.T) {
	mw := RateLimitMiddleware(RateLimitOptions{
		Requests:     1,
		Window:       time.Minute,
		ScopeByRoute: true,
		KeyFunc: func(*http.Request) string {
			return "same-client"
		},
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/projects/100", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected first projects request 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/users/100", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected users route to have separate budget, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/projects/200", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second projects request to be limited, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_ScopeByRole(t *testing.T) {
	jwtMgr := auth.NewJWTManager("router-test-rate-limit-secret-123456", time.Hour)
	adminToken, err := jwtMgr.Generate("1", "alice", "admin")
	if err != nil {
		t.Fatalf("generate admin token failed: %v", err)
	}
	userToken, err := jwtMgr.Generate("2", "bob", "user")
	if err != nil {
		t.Fatalf("generate user token failed: %v", err)
	}

	rateMW := RateLimitMiddleware(RateLimitOptions{
		Requests:    1,
		Window:      time.Minute,
		ScopeByRole: true,
		KeyFunc: func(*http.Request) string {
			return "same-client"
		},
	})

	base := rateMW(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := jwtMgr.OptionalJWTMiddleware()(base)

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin request 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/reports", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected user request to have separate role budget, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/reports", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second admin request to be limited, got %d", rec.Code)
	}
}

func TestTelemetryMiddleware_InjectsSpanIntoContext(t *testing.T) {
	orig := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		otel.SetTracerProvider(orig)
		_ = tp.Shutdown(context.Background())
	}()

	var spanValid bool
	var observedTraceID string
	handler := TelemetryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanValid = oteltrace.SpanFromContext(r.Context()).SpanContext().IsValid()
		observedTraceID = observe.TraceIDFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/telemetry", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !spanValid {
		t.Fatal("expected valid span in request context")
	}
	if observedTraceID == "" {
		t.Fatal("expected trace id in observe context")
	}
}

func TestWithCSRF(t *testing.T) {
	opts := []Option{WithCSRF("/api/")}
	o := &routerOpts{}
	for _, opt := range opts {
		opt(o)
	}
	if !o.enableCSRF {
		t.Error("expected CSRF to be enabled")
	}
	if len(o.csrfExempt) != 1 || o.csrfExempt[0] != "/api/" {
		t.Errorf("expected exempt path /api/, got %v", o.csrfExempt)
	}
}

func TestWithTimeout(t *testing.T) {
	opts := []Option{WithTimeout(60)}
	o := &routerOpts{}
	for _, opt := range opts {
		opt(o)
	}
	if o.timeoutSeconds != 60 {
		t.Errorf("expected timeout 60, got %d", o.timeoutSeconds)
	}
}

func TestWithRateLimit(t *testing.T) {
	opts := []Option{WithRateLimit(100, time.Minute)}
	o := &routerOpts{}
	for _, opt := range opts {
		opt(o)
	}
	if o.rateLimitReqs != 100 {
		t.Errorf("expected 100 requests, got %d", o.rateLimitReqs)
	}
	if o.rateLimitWin != time.Minute {
		t.Errorf("expected 1 minute window, got %v", o.rateLimitWin)
	}
}

func TestWithRateLimitPolicy(t *testing.T) {
	policy := RateLimitPolicy{
		Requests: 50,
		Window:   time.Minute,
		Burst:    10,
		ByRoute:  true,
		ByRole:   true,
	}
	opts := []Option{WithRateLimitPolicy(policy)}
	o := &routerOpts{}
	for _, opt := range opts {
		opt(o)
	}
	if o.rateLimitReqs != 50 {
		t.Errorf("expected 50 requests, got %d", o.rateLimitReqs)
	}
	if o.rateLimitBurst != 10 {
		t.Errorf("expected burst 10, got %d", o.rateLimitBurst)
	}
	if !o.rateLimitRoute {
		t.Error("expected ByRoute to be true")
	}
	if !o.rateLimitRole {
		t.Error("expected ByRole to be true")
	}
}
