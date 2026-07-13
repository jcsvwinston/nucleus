package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

const testSecret = "test-secret-key-at-least-32-chars!"

func TestJWT_GenerateAndValidate(t *testing.T) {
	mgr := NewJWTManager(testSecret, time.Hour)

	token, err := mgr.Generate("user-1", "alice", "admin")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}

	claims, err := mgr.Validate(token)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("expected user-1, got %s", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Errorf("expected alice, got %s", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("expected admin, got %s", claims.Role)
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	mgr := NewJWTManager(testSecret, -time.Hour) // Already expired
	token, _ := mgr.Generate("user-1", "alice", "admin")

	_, err := mgr.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWT_InvalidSecret(t *testing.T) {
	mgr1 := NewJWTManager("secret-one-is-here-1234567890-xx!", time.Hour)
	mgr2 := NewJWTManager("secret-two-is-here-1234567890-yy!", time.Hour)

	token, _ := mgr1.Generate("user-1", "alice", "admin")
	_, err := mgr2.Validate(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestJWT_InvalidTokenString(t *testing.T) {
	mgr := NewJWTManager(testSecret, time.Hour)
	_, err := mgr.Validate("not.a.valid.token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestJWT_Middleware_ValidToken(t *testing.T) {
	mgr := NewJWTManager(testSecret, time.Hour)
	token, _ := mgr.Generate("user-1", "alice", "admin")

	handler := mgr.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			t.Error("expected claims in context")
			return
		}
		if claims.UserID != "user-1" {
			t.Errorf("expected user-1, got %s", claims.UserID)
		}
		if got := observe.UserIDFromCtx(r.Context()); got != "user-1" {
			t.Errorf("expected user id in observe context, got %q", got)
		}
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWT_Middleware_MissingHeader(t *testing.T) {
	mgr := NewJWTManager(testSecret, time.Hour)
	handler := mgr.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWT_Middleware_InvalidFormat(t *testing.T) {
	mgr := NewJWTManager(testSecret, time.Hour)
	handler := mgr.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWT_OptionalMiddleware_NoToken(t *testing.T) {
	mgr := NewJWTManager(testSecret, time.Hour)
	handler := mgr.OptionalJWTMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := ClaimsFromContext(r.Context())
		if ok {
			t.Error("expected no claims without token")
		}
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestClaimsFromContext_Empty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok := ClaimsFromContext(r.Context())
	if ok {
		t.Error("expected no claims in empty context")
	}
}

// TestContextWithClaims_RoundTripAndNil pins the exported claims
// injector (the session→authz bridge; fleetdesk finding #21): injected
// claims must be retrievable via ClaimsFromContext exactly as if
// JWTManager.Middleware had stored them, the observability user id must
// ride along, and nil claims must be a no-op.
func TestContextWithClaims_RoundTripAndNil(t *testing.T) {
	base := context.Background()

	if got := ContextWithClaims(base, nil); got != base {
		t.Fatal("ContextWithClaims(ctx, nil) must return the original context unchanged")
	}

	claims := &Claims{UserID: "user-1", Username: "ops", Role: "operator"}
	ctx := ContextWithClaims(base, claims)

	got, ok := ClaimsFromContext(ctx)
	if !ok || got != claims {
		t.Fatalf("ClaimsFromContext = (%v, %v), want the injected claims", got, ok)
	}
	if uid := observe.UserIDFromCtx(ctx); uid != "user-1" {
		t.Fatalf("observability user id = %q, want %q", uid, "user-1")
	}
}
