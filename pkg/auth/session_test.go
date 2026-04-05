package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		Lifetime: 24 * time.Hour,
		Secure:   false,
	})
	if sm == nil {
		t.Fatal("expected non-nil session manager")
	}
	if sm.SCS() == nil {
		t.Fatal("expected non-nil underlying SCS")
	}
}

func TestNewSessionManager_Defaults(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	if sm.SCS().Lifetime != 72*time.Hour {
		t.Errorf("expected 72h default lifetime, got %v", sm.SCS().Lifetime)
	}
}

func TestSessionManager_Middleware(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	mw := sm.Middleware()
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestNewSessionManager_CookieSettings(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		Secure:     true,
		Path:       "/app",
		Domain:     "example.com",
		CookieName: "goframe_session",
		SameSite:   "strict",
	})

	if !sm.SCS().Cookie.Secure {
		t.Fatal("expected secure cookie")
	}
	if sm.SCS().Cookie.Path != "/app" {
		t.Fatalf("expected /app cookie path, got %q", sm.SCS().Cookie.Path)
	}
	if sm.SCS().Cookie.Domain != "example.com" {
		t.Fatalf("expected cookie domain example.com, got %q", sm.SCS().Cookie.Domain)
	}
	if sm.SCS().Cookie.Name != "goframe_session" {
		t.Fatalf("expected cookie name goframe_session, got %q", sm.SCS().Cookie.Name)
	}
	if sm.SCS().Cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected strict same-site, got %v", sm.SCS().Cookie.SameSite)
	}
}
