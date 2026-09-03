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
	t.Setenv("NUCLEUS_INSTANCE_ID", "")

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

func TestDetectSessionRuntimeIdentity_StandaloneDoesNotUseHostnameAsPod(t *testing.T) {
	t.Setenv("POD_NAME", "")
	t.Setenv("K8S_POD_NAME", "")
	t.Setenv("POD", "")
	t.Setenv("POD_NAMESPACE", "")
	t.Setenv("NODE_NAME", "")
	t.Setenv("K8S_NODE_NAME", "")
	t.Setenv("POD_NODE_NAME", "")
	t.Setenv("HOST_NODE_NAME", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_PORT", "")
	t.Setenv("HOSTNAME", "dev-host-01")
	t.Setenv("NUCLEUS_INSTANCE_ID", "")

	identity := DetectSessionRuntimeIdentity()
	if identity.Pod != "" {
		t.Fatalf("expected empty pod in standalone runtime, got %q", identity.Pod)
	}
	if identity.Host == "" {
		t.Fatal("expected non-empty host in standalone runtime")
	}
	if identity.Instance == "" {
		t.Fatal("expected non-empty instance in standalone runtime")
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
	req.RemoteAddr = "203.0.113.10:4567"
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
	if values[SessionMetaRemoteIPKey] != "203.0.113.10" {
		t.Fatalf("expected remote ip metadata 203.0.113.10, got %#v", values[SessionMetaRemoteIPKey])
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

// Forwarding headers are client-controlled unless a trusted proxy rewrote
// them, and the router's RealIP middleware already folds those into
// RemoteAddr for the proxies in trusted_proxies. Reading the headers here
// would let any client pick the IP recorded against its session.
func TestClientIPFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.8, 10.0.0.1")
	req.RemoteAddr = "203.0.113.10:2222"
	if got := ClientIPFromRequest(req); got != "203.0.113.10" {
		t.Fatalf("X-Forwarded-For must not override RemoteAddr, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "192.0.2.44")
	req.RemoteAddr = "203.0.113.10:3333"
	if got := ClientIPFromRequest(req); got != "203.0.113.10" {
		t.Fatalf("X-Real-IP must not override RemoteAddr, got %q", got)
	}

	// RemoteAddr as the RealIP middleware leaves it for a trusted proxy:
	// a bare host, no port.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.8"
	if got := ClientIPFromRequest(req); got != "198.51.100.8" {
		t.Fatalf("expected the bare RemoteAddr host, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:4444"
	if got := ClientIPFromRequest(req); got != "203.0.113.10" {
		t.Fatalf("expected remote addr host, got %q", got)
	}
}
