package app

import (
	"strings"
	"testing"
)

// TestBuildSessionManager_RejectsSameSiteNoneWithoutSecure is the FW-4
// production-strictness guard: buildSessionManager must fail startup when
// session_cookie_samesite=none is paired with session_cookie_secure=false,
// because browsers silently drop such cookies and the session would never
// persist. The validation runs before any database access, so a nil *db.DB
// is sufficient to reach it.
func TestBuildSessionManager_RejectsSameSiteNoneWithoutSecure(t *testing.T) {
	cfg := &Config{
		SessionCookieSameSite: "none",
		SessionCookieSecure:   false,
	}

	sm, shutdown, err := buildSessionManager(cfg, nil)
	if err == nil {
		t.Fatal("expected error for SameSite=None without Secure, got nil")
	}
	if sm != nil || shutdown != nil {
		t.Fatalf("expected nil manager and shutdown on error, got sm=%v shutdown=%v", sm, shutdown)
	}
	if !strings.Contains(err.Error(), "session_cookie_secure") {
		t.Fatalf("expected error to mention session_cookie_secure, got %q", err.Error())
	}
}

// TestBuildSessionManager_AcceptsSameSiteNoneWithSecure confirms the valid
// combination (None + Secure) is accepted and yields a manager. SessionStore
// is left empty, which normalises to the in-memory store (no DB required).
func TestBuildSessionManager_AcceptsSameSiteNoneWithSecure(t *testing.T) {
	cfg := &Config{
		SessionCookieSameSite: "none",
		SessionCookieSecure:   true,
	}

	sm, _, err := buildSessionManager(cfg, nil)
	if err != nil {
		t.Fatalf("expected no error for SameSite=None with Secure, got %v", err)
	}
	if sm == nil {
		t.Fatal("expected non-nil session manager")
	}
}

// TestBuildSessionManager_AcceptsSameSiteLaxInsecure confirms the validation is
// scoped to None only — a Lax cookie without Secure is a valid (if relaxed)
// configuration and must not fail startup.
func TestBuildSessionManager_AcceptsSameSiteLaxInsecure(t *testing.T) {
	cfg := &Config{
		SessionCookieSameSite: "lax",
		SessionCookieSecure:   false,
	}

	sm, _, err := buildSessionManager(cfg, nil)
	if err != nil {
		t.Fatalf("expected no error for SameSite=Lax insecure, got %v", err)
	}
	if sm == nil {
		t.Fatal("expected non-nil session manager")
	}
}
