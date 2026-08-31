// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"
)

// TestRunLogsListeningAddress pins NC-03: `go run .` — the documented
// golden path, which calls App.Run — must say where the server listens.
// The boot log used to end with the last subsystem line and go silent;
// the only way to learn the port was reading nucleus.yml.
func TestRunLogsListeningAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	cfg := DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Databases = map[string]DatabaseConfig{"default": {URL: "sqlite://:memory:"}}

	a, err := New(&cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	var logBuf bytes.Buffer
	a.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// Wait until the port answers, so the log line demonstrably precedes
	// (or accompanies) a serving socket.
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 250*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up on port %d", port)
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	out := logBuf.String()
	if !bytes.Contains([]byte(out), []byte("server listening")) {
		t.Fatalf("Run must log where it listens, got:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte(fmt.Sprintf("127.0.0.1:%d", port))) {
		t.Fatalf("the listening log must carry the configured address, got:\n%s", out)
	}
}
