// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// sessionRoundTrip drives a real request cycle through the session
// middleware and hands the handler the request context, so a probe asserts
// on what the session actually stored rather than on the manager's shape.
func sessionRoundTrip(t *testing.T, sm *auth.SessionManager, handler func(r *http.Request)) *http.Response {
	t.Helper()
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Result()
}

// SES-01 — a value put in the session comes back on the next request under
// the cookie the first one set.
func probeSessionRoundTrip(t *testing.T, _ *env) verdict {
	sm := auth.NewSessionManager(auth.SessionConfig{})
	var got string
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := sm.GetString(r.Context(), "who"); v != "" {
			got = v
		} else {
			sm.Put(r.Context(), "who", "ana")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Log("no session cookie was set")
		return absent
	}

	second := httptest.NewRequest(http.MethodGet, "/", nil)
	second.AddCookie(cookies[0])
	h.ServeHTTP(httptest.NewRecorder(), second)
	if got != "ana" {
		t.Logf("the session did not survive the round trip (got %q)", got)
		return partial
	}
	return present
}

// SES-02 — the session token rotates on demand, which is what a login has to
// do to close session fixation.
//
// The probe drives two request cycles, because a token only exists once a
// session has been committed: the first request creates one and gets its
// cookie, the second renews and must hand back a DIFFERENT cookie. Asking
// for the token inside a single anonymous request measures nothing — it is
// empty either way.
func probeSessionRenew(t *testing.T, _ *env) verdict {
	sm := auth.NewSessionManager(auth.SessionConfig{})
	renew := false
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if renew {
			if err := sm.RenewToken(r.Context()); err != nil {
				t.Fatalf("RenewToken: %v", err)
			}
		} else {
			sm.Put(r.Context(), "who", "ana")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	before := first.Result().Cookies()
	if len(before) == 0 {
		t.Log("no session cookie after the first request")
		return absent
	}

	renew = true
	second := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(before[0])
	h.ServeHTTP(second, req)
	after := second.Result().Cookies()
	if len(after) == 0 || after[0].Value == before[0].Value {
		t.Logf("the cookie did not change across RenewToken")
		return partial
	}
	return present
}

// SES-03 — the active sessions of the whole store can be enumerated, which
// is the half of "sessions per device" that exists.
func probeSessionEnumerate(t *testing.T, _ *env) verdict {
	sm := auth.NewSessionManager(auth.SessionConfig{})
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", "ana")
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	sessions, err := sm.ActiveSessions(t.Context())
	if err != nil {
		t.Logf("ActiveSessions: %v", err)
		return absent
	}
	if len(sessions) == 0 {
		t.Log("the store enumerated no session after one was written")
		return partial
	}
	return present
}

// SES-04 — revoking ANOTHER session (the "sign out my other devices"
// button) through the framework's own API. The probe holds two sessions,
// revokes one and checks the other survives: a revocation that ends
// everything would pass a weaker test and fail the button.
func probeSessionRevokeOther(t *testing.T, _ *env) verdict {
	sm := auth.NewSessionManager(auth.SessionConfig{})
	revoker, ok := any(sm).(interface {
		Revoke(context.Context, string) error
	})
	if !ok {
		return absent
	}

	phone := benchSignIn(t, sm, "ana")
	laptop := benchSignIn(t, sm, "ana")
	if err := revoker.Revoke(t.Context(), phone.Value); err != nil {
		t.Logf("Revoke: %v", err)
		return partial
	}
	if benchSignedIn(t, sm, phone) {
		t.Log("the revoked session still resolves")
		return partial
	}
	if !benchSignedIn(t, sm, laptop) {
		t.Log("revoking one session ended another")
		return partial
	}
	return present
}

// SES-05 — what a session records about the device it belongs to.
//
// Two cycles again, and for a reason the middleware states: it skips a
// request with no committed session so anonymous traffic does not create
// rows. So the first request creates the session and the second is the one
// that carries device information.
func probeSessionDeviceMetadata(t *testing.T, _ *env) verdict {
	sm := auth.NewSessionManager(auth.SessionConfig{})
	mw := auth.RuntimeMetadataMiddleware(sm, auth.SessionRuntimeIdentity{
		Pod: "pod-1", Host: "host-1", Instance: "instance-1",
	}, time.Nanosecond)

	h := sm.Middleware()(mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", "ana")
		w.WriteHeader(http.StatusNoContent)
	})))

	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "authbench/1.0")
	req.RemoteAddr = "203.0.113.7:1234"
	h.ServeHTTP(first, req)
	cookies := first.Result().Cookies()
	if len(cookies) == 0 {
		t.Log("no session cookie after the first request")
		return absent
	}

	second := httptest.NewRequest(http.MethodGet, "/", nil)
	second.Header.Set("User-Agent", "authbench/1.0")
	second.RemoteAddr = "203.0.113.7:1234"
	second.AddCookie(cookies[0])
	h.ServeHTTP(httptest.NewRecorder(), second)

	sessions, err := sm.ActiveSessions(t.Context())
	if err != nil || len(sessions) == 0 {
		t.Logf("no session to inspect (err=%v, n=%d)", err, len(sessions))
		return absent
	}

	var addr, agent bool
	for _, v := range sessions[0].Values {
		s, _ := v.(string)
		if strings.Contains(s, "203.0.113.7") {
			addr = true
		}
		if strings.Contains(s, "authbench/1.0") {
			agent = true
		}
	}
	t.Logf("session records address=%v user-agent=%v keys=%d", addr, agent, len(sessions[0].Values))
	switch {
	case addr && agent:
		return present
	case addr:
		return partial
	default:
		return absent
	}
}

// SES-06 — the inactivity timeout an ASVS L2 session needs. The knob exists;
// the probe reads the DEFAULT the framework ships, which is what an
// application that never sets it gets.
func probeSessionIdleTimeout(t *testing.T, _ *env) verdict {
	cfg := app.DefaultConfig()
	if cfg.SessionIdleTimeout > 0 {
		return present
	}
	sm := auth.NewSessionManager(auth.SessionConfig{IdleTimeout: time.Minute})
	if sm.SCS().IdleTimeout != time.Minute {
		t.Log("the idle timeout is not even honoured when set")
		return absent
	}
	return partial
}

// benchSignIn drives one request cycle that writes a session and returns
// its cookie — one "device".
func benchSignIn(t *testing.T, sm *auth.SessionManager, userID string) *http.Cookie {
	t.Helper()
	h := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", userID)
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie was issued")
	}
	return cookies[0]
}

// benchSignedIn reports whether a cookie still resolves to its session.
func benchSignedIn(t *testing.T, sm *auth.SessionManager, cookie *http.Cookie) bool {
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
