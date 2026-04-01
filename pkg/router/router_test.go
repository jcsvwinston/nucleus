package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
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
	err := gferrors.NotFound("User", "42")
	Error(w, err)

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

	if w.Code != 403 {
		t.Errorf("POST without token should be 403, got %d", w.Code)
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
	if r.Router == nil {
		t.Fatal("Router.Router is nil")
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

func TestTelemetryMiddleware_InjectsSpanIntoContext(t *testing.T) {
	orig := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		otel.SetTracerProvider(orig)
		_ = tp.Shutdown(context.Background())
	}()

	var spanValid bool
	handler := TelemetryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanValid = oteltrace.SpanFromContext(r.Context()).SpanContext().IsValid()
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
}
