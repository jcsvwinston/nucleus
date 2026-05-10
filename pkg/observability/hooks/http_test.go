package hooks

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

func newBus(t *testing.T) *observability.Bus {
	t.Helper()
	return observability.NewBus(slog.New(slog.DiscardHandler))
}

// TestHTTPMiddleware_NoSubscribers_PassThrough verifies the middleware does
// not allocate or mutate the response when nobody is subscribed.
func TestHTTPMiddleware_NoSubscribers_PassThrough(t *testing.T) {
	bus := newBus(t)

	called := false
	mw := NewHTTPMiddleware(HTTPMiddlewareConfig{Bus: bus, NodeID: "node-a"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(204)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler not called")
	}
	if rr.Code != 204 {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if got := bus.Stats(observability.KindHTTPRequest); got.Emitted != 0 {
		t.Fatalf("emitted = %d, want 0 when nobody subscribed", got.Emitted)
	}
}

// TestHTTPMiddleware_Emits_OnSubscriber verifies one full round-trip with
// a subscriber, including correct method/path/status/duration.
func TestHTTPMiddleware_Emits_OnSubscriber(t *testing.T) {
	bus := newBus(t)
	sub, cancel := bus.Subscribe(observability.Filter{Kinds: []observability.EventKind{observability.KindHTTPRequest}}, nil)
	defer func() {
		cancel()
		// Drain.
		for {
			select {
			case ev := <-sub.Ch():
				ev.Release()
			default:
				return
			}
		}
	}()

	mw := NewHTTPMiddleware(HTTPMiddlewareConfig{Bus: bus, NodeID: "node-a"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(201)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/things?key=ABC&name=foo", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != 201 {
		t.Fatalf("status = %d", rr.Code)
	}

	select {
	case ev := <-sub.Ch():
		http, ok := ev.(*observability.HTTPRequestEvent)
		if !ok {
			t.Fatalf("got %T, want *HTTPRequestEvent", ev)
		}
		if http.Method != "POST" {
			t.Errorf("method = %q", http.Method)
		}
		if http.Path != "/api/things" {
			t.Errorf("path = %q", http.Path)
		}
		if http.Status != 201 {
			t.Errorf("status = %d", http.Status)
		}
		if http.Duration <= 0 {
			t.Errorf("duration not measured: %v", http.Duration)
		}
		if http.NodeID() != "node-a" {
			t.Errorf("node = %q", http.NodeID())
		}
		// PayloadPreview should NOT contain the raw "key=ABC". Body preview
		// for non-GET/DELETE without body should be "body:redacted".
		if http.PayloadPreview != "body:redacted" {
			t.Errorf("payload preview = %q (POST without body)", http.PayloadPreview)
		}
		http.Release()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for emit")
	}
}

// TestHTTPMiddleware_GETQuery_RedactsSensitiveKeys verifies the GET payload
// preview redacts key/secret/password/token query parameters.
func TestHTTPMiddleware_GETQuery_RedactsSensitiveKeys(t *testing.T) {
	bus := newBus(t)
	sub, cancel := bus.Subscribe(observability.Filter{}, nil)
	defer func() {
		cancel()
		for {
			select {
			case ev := <-sub.Ch():
				ev.Release()
			default:
				return
			}
		}
	}()

	mw := NewHTTPMiddleware(HTTPMiddlewareConfig{Bus: bus, NodeID: "n"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?api_key=secretvalue&name=foo&password=hunter2", nil)
	h.ServeHTTP(rr, req)

	ev := <-sub.Ch()
	http, _ := ev.(*observability.HTTPRequestEvent)
	defer http.Release()

	// url.Values.Encode percent-encodes "*" as "%2A", so the redacted marker
	// becomes "%2A%2A%2A" on the wire. Order is map-iteration dependent;
	// assert by substring.
	const redactedEnc = "%2A%2A%2A"
	if !contains(http.PayloadPreview, "api_key="+redactedEnc) {
		t.Errorf("api_key not redacted: %q", http.PayloadPreview)
	}
	if !contains(http.PayloadPreview, "password="+redactedEnc) {
		t.Errorf("password not redacted: %q", http.PayloadPreview)
	}
	if !contains(http.PayloadPreview, "name=foo") {
		t.Errorf("non-sensitive name should be visible: %q", http.PayloadPreview)
	}
	if contains(http.PayloadPreview, "secretvalue") {
		t.Errorf("raw value leaked: %q", http.PayloadPreview)
	}
	if contains(http.PayloadPreview, "hunter2") {
		t.Errorf("raw password leaked: %q", http.PayloadPreview)
	}
}

// TestHTTPMiddleware_ExcludePaths verifies excluded paths are not emitted.
func TestHTTPMiddleware_ExcludePaths(t *testing.T) {
	bus := newBus(t)
	sub, cancel := bus.Subscribe(observability.Filter{}, nil)
	defer func() {
		cancel()
		for {
			select {
			case ev := <-sub.Ch():
				ev.Release()
			default:
				return
			}
		}
	}()

	mw := NewHTTPMiddleware(HTTPMiddlewareConfig{
		Bus:          bus,
		NodeID:       "n",
		ExcludePaths: []string{"/healthz", "/admin/*"},
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	for _, p := range []string{"/healthz", "/admin/things", "/admin/things/1"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
	}

	// One observed: /api/foo
	req := httptest.NewRequest(http.MethodGet, "/api/foo", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// Drain and count.
	count := 0
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case ev := <-sub.Ch():
			ev.Release()
			count++
		case <-timeout:
			break loop
		}
	}
	if count != 1 {
		t.Fatalf("emitted %d events, want 1 (only /api/foo)", count)
	}
}

// TestHTTPMiddleware_NilBus_PassesThrough verifies the safety net.
func TestHTTPMiddleware_NilBus_PassesThrough(t *testing.T) {
	mw := NewHTTPMiddleware(HTTPMiddlewareConfig{Bus: nil})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)
	if !called {
		t.Fatal("inner handler not called")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || index(haystack, needle) >= 0)
}

func index(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Sanity check: the unused error import does not break compilation.
var _ = errors.New
