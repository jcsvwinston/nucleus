package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDetectSessionRuntimeIdentity(t *testing.T) {
	t.Setenv("POD_NAME", "api-pod-01")
	t.Setenv("NODE_NAME", "node-a")
	t.Setenv("GOFRAME_INSTANCE_ID", "")

	identity := DetectSessionRuntimeIdentity()
	if identity.Pod != "api-pod-01" {
		t.Fatalf("expected pod api-pod-01, got %q", identity.Pod)
	}
	if identity.Host != "node-a" {
		t.Fatalf("expected host node-a, got %q", identity.Host)
	}
	if identity.Instance != "api-pod-01@node-a" {
		t.Fatalf("expected instance api-pod-01@node-a, got %q", identity.Instance)
	}
}

func TestRuntimeMetadataMiddleware_UpdatesExistingSession(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		Lifetime: time.Hour,
	})

	deadline := time.Now().UTC().Add(time.Hour)
	payload, err := sm.SCS().Codec.Encode(deadline, map[string]interface{}{})
	if err != nil {
		t.Fatalf("encode session payload: %v", err)
	}
	token := "existing-token-123"
	if err := sm.SCS().Store.Commit(token, payload, deadline); err != nil {
		t.Fatalf("commit seed payload: %v", err)
	}

	handler := sm.Middleware()(RuntimeMetadataMiddleware(sm, SessionRuntimeIdentity{
		Pod:      "pod-x",
		Host:     "node-y",
		Instance: "pod-x@node-y",
	}, time.Nanosecond)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sm.SCS().Cookie.Name, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	raw, found, err := sm.SCS().Store.Find(token)
	if err != nil {
		t.Fatalf("find session after request: %v", err)
	}
	if !found {
		t.Fatal("expected session to exist after metadata update")
	}

	_, values, err := sm.SCS().Codec.Decode(raw)
	if err != nil {
		t.Fatalf("decode session payload: %v", err)
	}
	if values[SessionMetaPodKey] != "pod-x" {
		t.Fatalf("expected pod metadata pod-x, got %#v", values[SessionMetaPodKey])
	}
	if values[SessionMetaHostKey] != "node-y" {
		t.Fatalf("expected host metadata node-y, got %#v", values[SessionMetaHostKey])
	}
	if values[SessionMetaInstanceKey] != "pod-x@node-y" {
		t.Fatalf("expected instance metadata pod-x@node-y, got %#v", values[SessionMetaInstanceKey])
	}
	if _, ok := values[SessionMetaLastSeenAtKey]; !ok {
		t.Fatalf("expected %s key in session values", SessionMetaLastSeenAtKey)
	}
}

func TestRuntimeMetadataMiddleware_DoesNotCreateSessionForAnonymousRequest(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})

	handler := sm.Middleware()(RuntimeMetadataMiddleware(sm, SessionRuntimeIdentity{
		Pod:  "pod-a",
		Host: "node-a",
	}, time.Nanosecond)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sm.SCS().Cookie.Name {
			t.Fatalf("did not expect session cookie to be issued for anonymous request")
		}
	}
}
