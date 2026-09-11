package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// signIn drives one request cycle that writes a session and returns its
// cookie, so a test can hold several "devices" at once.
func signIn(t *testing.T, sm *SessionManager, userID, agent string) *http.Cookie {
	t.Helper()
	h := sm.Middleware()(RuntimeMetadataMiddleware(sm, SessionRuntimeIdentity{Instance: "node"}, 1)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sm.Put(r.Context(), "user_id", userID)
			w.WriteHeader(http.StatusNoContent)
		})))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", agent)
	h.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie was issued")
	}
	return cookies[0]
}

// stillSignedIn reports whether a cookie still resolves to its session.
func stillSignedIn(t *testing.T, sm *SessionManager, cookie *http.Cookie) bool {
	t.Helper()
	var found string
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		found = sm.GetString(r.Context(), "user_id")
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), req)
	return found != ""
}

func TestSessionManager_RevokeEndsAnotherSession(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	phone := signIn(t, sm, "ana", "phone/1.0")
	laptop := signIn(t, sm, "ana", "laptop/1.0")

	sessions, err := sm.ActiveSessions(t.Context())
	if err != nil {
		t.Fatalf("active sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %d", len(sessions))
	}

	if err := sm.Revoke(t.Context(), phone.Value); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if stillSignedIn(t, sm, phone) {
		t.Error("the revoked session still resolves")
	}
	if !stillSignedIn(t, sm, laptop) {
		t.Error("revoking one session ended the other")
	}
}

// "Sign out everywhere except here" is the shape that actually gets built.
func TestSessionManager_RevokeWhereKeepsTheCurrentSession(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	phone := signIn(t, sm, "ana", "phone/1.0")
	tablet := signIn(t, sm, "ana", "tablet/1.0")
	other := signIn(t, sm, "beto", "laptop/1.0")

	n, err := sm.RevokeWhere(t.Context(), func(info SessionInfo) bool {
		return info.Values["user_id"] == "ana" && info.Token != phone.Value
	})
	if err != nil {
		t.Fatalf("revoke where: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one revocation, got %d", n)
	}
	if !stillSignedIn(t, sm, phone) {
		t.Error("the current session was revoked")
	}
	if stillSignedIn(t, sm, tablet) {
		t.Error("the other device is still signed in")
	}
	if !stillSignedIn(t, sm, other) {
		t.Error("another user's session was revoked")
	}
}

// Revoking a token nobody holds is not an error: the outcome already holds,
// and an error would tell the caller whether a token existed.
func TestSessionManager_RevokeUnknownTokenIsNotAnError(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	if err := sm.Revoke(t.Context(), "a-token-that-was-never-issued"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
}

func TestSessionManager_RevokeEmptyTokenIsRefused(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	if err := sm.Revoke(t.Context(), "   "); err == nil {
		t.Fatal("an empty token was accepted")
	}
}

// A device list needs to say from WHAT, not only from where and when.
func TestRuntimeMetadata_RecordsTheUserAgent(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	cookie := signIn(t, sm, "ana", "Mozilla/5.0 (authtest)")
	// A second request through the middleware is what records metadata:
	// the first has no committed session yet.
	_ = stillSignedInWithAgent(t, sm, cookie, "Mozilla/5.0 (authtest)")

	sessions, err := sm.ActiveSessions(t.Context())
	if err != nil || len(sessions) == 0 {
		t.Fatalf("no session to inspect (%v)", err)
	}
	if got := sessions[0].Values[SessionMetaUserAgentKey]; got != "Mozilla/5.0 (authtest)" {
		t.Fatalf("user agent recorded as %v", got)
	}
	if sessions[0].Values[SessionMetaRemoteIPKey] == nil {
		t.Error("the client address was not recorded")
	}
}

// A user agent is attacker-controlled text: control characters are stripped
// and the value is capped before it lands in a session payload.
func TestRuntimeMetadata_UserAgentIsSanitized(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	hostile := "curl/8.0\r\nX-Injected: yes" + string(make([]byte, 0))
	cookie := signIn(t, sm, "ana", hostile)
	_ = stillSignedInWithAgent(t, sm, cookie, hostile)

	sessions, _ := sm.ActiveSessions(t.Context())
	stored, _ := sessions[0].Values[SessionMetaUserAgentKey].(string)
	if stored == "" {
		t.Skip("the transport dropped the header entirely")
	}
	for _, r := range stored {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("a control character survived into the session: %q", stored)
		}
	}
}

func stillSignedInWithAgent(t *testing.T, sm *SessionManager, cookie *http.Cookie, agent string) bool {
	t.Helper()
	var found string
	h := sm.Middleware()(RuntimeMetadataMiddleware(sm, SessionRuntimeIdentity{Instance: "node"}, 1)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			found = sm.GetString(r.Context(), "user_id")
			w.WriteHeader(http.StatusNoContent)
		})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", agent)
	req.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), req)
	return found != ""
}

// A context that never passed through the middleware must ANSWER, not
// panic: a handler mounted outside the session middleware is a mounting
// mistake, and a panic makes it look like a crash in unrelated code.
func TestHasSession(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	if sm.HasSession(t.Context()) {
		t.Error("a bare context reported a session")
	}

	var inside bool
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inside = sm.HasSession(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !inside {
		t.Error("a request through the middleware reported no session")
	}

	var nilManager *SessionManager
	if nilManager.HasSession(t.Context()) {
		t.Error("a nil manager reported a session")
	}
}
