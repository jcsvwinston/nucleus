// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// http.Server.WriteTimeout is a deadline on the connection, counted from the
// moment the request arrived. A stream outlives it by definition, so left in
// place it severs every SSE connection when it expires — and the keep-alive
// does not help, because the keep-alive proves the stream is alive and the
// deadline does not care. The framework ships write_timeout at 60s, so every
// live view died after a minute with an unexpected EOF.
//
// One second stands in for the sixty here. Remove the SetWriteDeadline call in
// ServeSSE and this test fails with the stream cut short.
func TestSSEOutlivesTheServerWriteTimeout(t *testing.T) {
	hub := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(func() { hub.Close() })

	const want = 8
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		go func() {
			for i := 0; i < want; i++ {
				time.Sleep(200 * time.Millisecond)
				hub.Broadcast(context.Background(), Message{Topic: "orders", Data: []byte("tick")})
			}
		}()
		_ = ServeSSE(w, r, SSEConfig{Hub: hub, Topics: []string{"orders"}})
	})

	srv := httptest.NewUnstartedServer(handler)
	// The shipped default is 60s; one second measures the same thing in a
	// test that finishes.
	srv.Config.WriteTimeout = time.Second
	srv.Start()
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	got := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data:") {
			got++
			if got == want {
				break
			}
		}
	}
	if got < want {
		t.Fatalf("the stream carried %d of %d messages before it was cut (scanner: %v) — the server's write deadline is still on the connection, so every SSE stream dies at write_timeout", got, want, scanner.Err())
	}
}
