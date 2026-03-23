package auth

import (
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
