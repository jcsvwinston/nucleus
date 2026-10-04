// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sleeper answers 200 after d unless the request's context ends first, and
// reports what the context said.
func sleeper(d time.Duration, saw chan<- error) Handler {
	return func(c *Context) error {
		select {
		case <-time.After(d):
			return c.JSON(http.StatusOK, map[string]string{"slept": d.String()})
		case <-c.Request.Context().Done():
			if saw != nil {
				saw <- c.Request.Context().Err()
			}
			return nil
		}
	}
}

// timeoutRouter has a router-wide timeout of global (via the option the
// application uses, in seconds, so it is driven through the middleware
// directly here to keep the test fast).
func timeoutRouter(global time.Duration, setup func(r *Router)) http.Handler {
	r := New(quietLogger(), WithTimeout(0))
	setup(r)
	return TimeoutMiddleware(global)(r)
}

func TestTimeout_ALongerRouteTimeoutOutlivesTheGlobalOne(t *testing.T) {
	h := timeoutRouter(60*time.Millisecond, func(r *Router) {
		r.Get("/fast", sleeper(150*time.Millisecond, nil))
		r.With(Timeout(time.Second)).Get("/export", sleeper(150*time.Millisecond, nil))
	})
	if rec := doShape(t, h, http.MethodGet, "/fast", "application/json"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/fast under the 60ms global: %d, want 503", rec.Code)
	}
	if rec := doShape(t, h, http.MethodGet, "/export", "application/json"); rec.Code != http.StatusOK {
		t.Fatalf("/export with its own 1s: %d %s, want 200", rec.Code, rec.Body)
	}
}

func TestTimeout_AShorterRouteTimeoutFiresFirst(t *testing.T) {
	saw := make(chan error, 1)
	h := timeoutRouter(time.Second, func(r *Router) {
		r.With(Timeout(40*time.Millisecond)).Get("/lookup", sleeper(500*time.Millisecond, saw))
	})
	start := time.Now()
	rec := doShape(t, h, http.MethodGet, "/lookup", "application/json")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/lookup: %d, want 503", rec.Code)
	}
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Fatalf("the 40ms route timeout took %v to fire", took)
	}
	env := decodeEnvelope(t, rec)
	if env.Error.Code != "TIMEOUT" {
		t.Fatalf("timeout body: %+v", env.Error)
	}
	select {
	case err := <-saw:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the handler's context ended with %v, want DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the handler's context never ended")
	}
}

func TestTimeout_WithNoGlobalTimeoutTheRouteEnforcesItsOwn(t *testing.T) {
	r := New(quietLogger(), WithTimeout(0))
	r.With(Timeout(30*time.Millisecond)).Get("/slow", sleeper(300*time.Millisecond, nil))
	r.Get("/free", sleeper(50*time.Millisecond, nil))
	if rec := doShape(t, r, http.MethodGet, "/slow", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/slow: %d", rec.Code)
	}
	if rec := doShape(t, r, http.MethodGet, "/free", ""); rec.Code != http.StatusOK {
		t.Fatalf("/free: %d", rec.Code)
	}
}

func TestTimeout_TheAnswerIsTheErrorShapeTheClientAskedFor(t *testing.T) {
	h := timeoutRouter(20*time.Millisecond, func(r *Router) {
		r.Get("/slow", sleeper(200*time.Millisecond, nil))
	})
	p := decodeProblem(t, doShape(t, h, http.MethodGet, "/slow", "application/problem+json"))
	if p.Status != http.StatusServiceUnavailable || p.Code != "TIMEOUT" {
		t.Fatalf("timeout as a problem: %+v", p)
	}
}

func TestTimeout_TheDeadlineTheHandlerSeesIsTheRoutes(t *testing.T) {
	var deadline time.Time
	h := timeoutRouter(50*time.Millisecond, func(r *Router) {
		r.With(Timeout(time.Hour)).Get("/d", func(c *Context) error {
			deadline, _ = c.Request.Context().Deadline()
			return c.NoContent()
		})
	})
	doShape(t, h, http.MethodGet, "/d", "")
	if time.Until(deadline) < 50*time.Minute {
		t.Fatalf("the handler saw the deadline %v, not the route's hour", deadline)
	}
}

func TestTimeout_APanicReachesTheRecoverer(t *testing.T) {
	h := RecovererWithLogger(quietLogger())(timeoutRouter(time.Second, func(r *Router) {
		r.Get("/panic", func(c *Context) error { panic("kaboom") })
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic under the timeout: %d", rec.Code)
	}
}

func TestTimeout_StreamingRequestsAreLeftAlone(t *testing.T) {
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, canFlush := w.(http.Flusher)
		time.Sleep(60 * time.Millisecond)
		if canFlush {
			w.Header().Set("X-Flusher", "yes")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Flusher") != "yes" {
		t.Fatalf("SSE under a route timeout: %d flusher=%q", rec.Code, rec.Header().Get("X-Flusher"))
	}
	if !strings.Contains(rec.Header().Get("X-Flusher"), "yes") {
		t.Fatal("unreachable")
	}
}
