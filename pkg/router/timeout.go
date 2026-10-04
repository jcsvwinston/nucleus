// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// A request timeout is a deadline on the request — and a deadline a route
// can move. The router-wide timeout (WithTimeout, TimeoutMiddleware) sets
// it when the request enters the stack; a route that declares its own with
// Timeout moves it, longer or shorter, when the request reaches the route.
// Before the deadline could move, a slow export and a fast lookup shared
// one limit, and the only way to give the export more time was to take its
// timeout away entirely (WithTimeoutExempt).
//
// The handler runs on its own goroutine against a buffered writer, the way
// http.TimeoutHandler runs it; when the deadline passes first, the client
// gets a 503 in the framework's error shape (TIMEOUT), the request context
// is done with context.DeadlineExceeded, and whatever the handler writes
// afterwards is discarded (its Write returns http.ErrHandlerTimeout). The
// buffered writer cannot Flush or Hijack, which is why WebSocket upgrades,
// clients asking for text/event-stream and the exempt prefixes get the raw
// writer and no deadline.

// timeoutError is what a request answers when its deadline passes.
var timeoutError = &gferrors.DomainError{
	Code:       "TIMEOUT",
	Message:    "request timeout",
	StatusCode: http.StatusServiceUnavailable,
}

// Timeout gives the routes it wraps their own request timeout, in place of
// the router-wide one:
//
//	r.With(router.Timeout(2 * time.Minute)).Get("/export", export)  // longer than the global 30s
//	r.With(router.Timeout(2 * time.Second)).Get("/lookup", lookup)  // shorter: fires first
//
// The duration counts from the moment the request reaches the route. Under
// a router-wide timeout it moves that deadline — later or earlier — so a
// longer route timeout is really longer, not cut short by the global one;
// under no router-wide timeout (WithTimeout(0), an exempt prefix) it is the
// request's only deadline. d <= 0 leaves the route as it was. WebSocket
// upgrades and text/event-stream requests are not given a deadline, as
// under the router-wide timeout.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsWebSocketUpgrade(r) || acceptsEventStream(r) {
				next.ServeHTTP(w, r)
				return
			}
			if dl, ok := r.Context().Value(deadlineKey{}).(*deadline); ok && dl.move(d) {
				next.ServeHTTP(w, r)
				return
			}
			serveWithDeadline(w, r, next, d, true)
		})
	}
}

type deadlineKey struct{}

// deadline is one request's deadline: done closes when it passes (err is
// context.DeadlineExceeded) or when the request's own context ends first
// (err is that context's error). move re-arms it until then.
type deadline struct {
	mu    sync.Mutex
	at    time.Time
	gen   int
	timer *time.Timer
	done  chan struct{}
	err   error
	stop  func() bool
	// w is the response writer the deadline's answer goes to, and first
	// where the deadline was set: a route that moves the deadline later
	// moves the connection's write deadline with it (see writeDeadline).
	w     http.ResponseWriter
	first time.Time
}

// writeGrace is how long past a request's deadline the connection may
// still take to write the answer.
const writeGrace = 10 * time.Second

func newDeadline(parent context.Context, w http.ResponseWriter, d time.Duration) *deadline {
	dl := &deadline{done: make(chan struct{}), w: w}
	dl.mu.Lock()
	dl.arm(d)
	dl.first = dl.at
	dl.mu.Unlock()
	dl.stop = context.AfterFunc(parent, func() { dl.finish(-1, parent.Err()) })
	return dl
}

// writeDeadline makes the connection's write deadline (the server's
// write_timeout) no earlier than the request deadline plus writeGrace, so a
// route that was given more time than write_timeout is not cut off by the
// server instead. A writer chain that cannot reach the connection is left
// as it is. mu must be held.
func (dl *deadline) writeDeadline() {
	_ = http.NewResponseController(dl.w).SetWriteDeadline(dl.at.Add(writeGrace))
}

// arm sets the deadline d from now; mu must be held. A timer armed before
// carries an older generation and expires into nothing.
func (dl *deadline) arm(d time.Duration) {
	dl.gen++
	gen := dl.gen
	dl.at = time.Now().Add(d)
	if dl.timer != nil {
		dl.timer.Stop()
	}
	dl.timer = time.AfterFunc(d, func() { dl.finish(gen, context.DeadlineExceeded) })
}

// move re-arms the deadline d from now; false when it has already passed.
func (dl *deadline) move(d time.Duration) bool {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if dl.err != nil {
		return false
	}
	dl.arm(d)
	if dl.at.After(dl.first) {
		dl.writeDeadline()
	}
	return true
}

// finish ends the deadline with err. gen -1 is the request's own context
// ending, which no re-arming supersedes.
func (dl *deadline) finish(gen int, err error) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if dl.err != nil || (gen >= 0 && gen != dl.gen) {
		return
	}
	dl.err = err
	close(dl.done)
}

func (dl *deadline) release() {
	dl.mu.Lock()
	if dl.timer != nil {
		dl.timer.Stop()
	}
	dl.mu.Unlock()
	dl.stop()
}

// deadlineCtx is the request context under a movable deadline: Deadline
// reports where it is now, Done and Err follow it, and values come from the
// request's context. Contexts derived from it see the deadline's error,
// context.DeadlineExceeded, as http.TimeoutHandler's do.
type deadlineCtx struct {
	parent context.Context
	dl     *deadline
}

func (c *deadlineCtx) Deadline() (time.Time, bool) {
	c.dl.mu.Lock()
	defer c.dl.mu.Unlock()
	return c.dl.at, true
}

func (c *deadlineCtx) Done() <-chan struct{} { return c.dl.done }

func (c *deadlineCtx) Err() error {
	c.dl.mu.Lock()
	defer c.dl.mu.Unlock()
	return c.dl.err
}

func (c *deadlineCtx) Value(key any) any {
	if _, ok := key.(deadlineKey); ok {
		return c.dl
	}
	return c.parent.Value(key)
}

// serveWithDeadline runs next under a deadline d from now, which a Timeout
// further down may move. own is a route's own deadline (Timeout with no
// router-wide one to move), whose connection write deadline follows it.
func serveWithDeadline(w http.ResponseWriter, r *http.Request, next http.Handler, d time.Duration, own bool) {
	dl := newDeadline(r.Context(), w, d)
	defer dl.release()
	if own {
		dl.mu.Lock()
		dl.writeDeadline()
		dl.mu.Unlock()
	}
	r = r.WithContext(&deadlineCtx{parent: r.Context(), dl: dl})

	done := make(chan struct{})
	panicked := make(chan any, 1)
	tw := &timeoutWriter{header: make(http.Header)}
	go func() {
		defer func() {
			if p := recover(); p != nil {
				panicked <- p
			}
		}()
		next.ServeHTTP(tw, r)
		close(done)
	}()
	select {
	case p := <-panicked:
		// Re-panic on the serving goroutine, where the Recoverer in front
		// of this middleware catches it.
		panic(p)
	case <-done:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		dst := w.Header()
		for k, vv := range tw.header {
			dst[k] = vv
		}
		if !tw.wroteHeader {
			tw.code = http.StatusOK
		}
		w.WriteHeader(tw.code)
		_, _ = w.Write(tw.buf.Bytes())
	case <-dl.done:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		if errors.Is(dl.err, context.DeadlineExceeded) {
			writeDomainError(w, r, timeoutError)
			tw.err = http.ErrHandlerTimeout
		} else {
			// The client went away: nobody reads a body.
			w.WriteHeader(http.StatusServiceUnavailable)
			tw.err = dl.err
		}
	}
}

// timeoutWriter buffers what the handler writes until it returns; after
// the deadline its writes fail with http.ErrHandlerTimeout.
type timeoutWriter struct {
	mu          sync.Mutex
	header      http.Header
	buf         bytes.Buffer
	err         error
	wroteHeader bool
	code        int
}

func (tw *timeoutWriter) Header() http.Header { return tw.header }

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.err != nil {
		return 0, tw.err
	}
	if !tw.wroteHeader {
		tw.wroteHeader, tw.code = true, http.StatusOK
	}
	return tw.buf.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	if code < 100 || code > 999 {
		panic(fmt.Sprintf("router: invalid WriteHeader code %d", code))
	}
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.err != nil || tw.wroteHeader {
		return
	}
	tw.wroteHeader, tw.code = true, code
}

// WroteHeader lets the Recoverer tell whether a panicking handler had
// started its response.
func (tw *timeoutWriter) WroteHeader() bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.wroteHeader
}
