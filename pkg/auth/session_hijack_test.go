// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// A handler behind the session middleware can still take the connection over.
//
// It could not: the middleware handed every handler a writer that implemented
// Flush and Unwrap and not Hijack, so a websocket library's
// `w.(http.Hijacker)` panicked and the request answered 500. The session
// manager is mounted by default, so this removed websockets from every
// application that used the framework's defaults.
func TestSessionMiddlewareLetsAHandlerHijack(t *testing.T) {
	sessions := auth.NewSessionManager(auth.SessionConfig{})

	handler := sessions.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("the writer a handler receives is not an http.Hijacker (%T): "+
				"a websocket upgrade asserts exactly this and panics without it", w)
			http.Error(w, "not hijackable", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			http.Error(w, "hijack failed", http.StatusInternalServerError)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, err := buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\nspoken-raw"); err != nil {
			t.Errorf("write raw: %v", err)
			return
		}
		if err := buf.Flush(); err != nil {
			t.Errorf("flush raw: %v", err)
		}
	}))

	srv := httptest.NewServer(handler)
	defer srv.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: bench\r\n\r\n")); err != nil {
		t.Fatalf("request: %v", err)
	}

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("status line = %q, want the 101 the hijacking handler wrote", strings.TrimSpace(status))
	}
	rest := make([]byte, 64)
	n, _ := reader.Read(rest)
	if body := string(rest[:n]); !strings.Contains(body, "spoken-raw") {
		t.Fatalf("body = %q, want what the handler wrote on the raw connection", body)
	}
}

// The ability degrades rather than panicking when nothing underneath can
// hijack — a recorder, for instance, which is what a handler test uses.
func TestSessionMiddlewareHijackOnANonHijackableWriter(t *testing.T) {
	sessions := auth.NewSessionManager(auth.SessionConfig{})

	var got error
	handler := sessions.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("the writer is not an http.Hijacker (%T)", w)
		}
		_, _, got = hj.Hijack()
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("hijacking a recorder returned no error: the caller has no way to fall back")
	}
}
