package metrics

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMetrics_DefaultRegistration(t *testing.T) {
	m := New()
	defer noop(m)

	// Bump every collector once to make sure none of them panic.
	m.Connected.WithLabelValues("a:8080").Set(1)
	m.ReconnectsTotal.Inc()
	m.EventsEmittedTotal.WithLabelValues("http_request").Inc()
	m.EventsDroppedTotal.WithLabelValues("sql_statement", "buffer_full").Inc()
	m.BufferSize.WithLabelValues("session_change").Set(7)
	m.ActiveSubscriptions.Set(2)
	m.HeartbeatsSent.Inc()
	m.StreamErrorsTotal.WithLabelValues("send").Inc()
}

func TestServer_ServesMetricsAndHealthz(t *testing.T) {
	m := New()
	m.EventsEmittedTotal.WithLabelValues("http_request").Inc()

	// Pick an ephemeral port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := NewServer(addr, m, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() { doneCh <- srv.Run(ctx) }()

	// Wait until the listener is up. ~50ms suffices in practice.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 50*time.Millisecond); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Run("metrics", func(t *testing.T) {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "admin_agent_events_emitted_total") {
			t.Errorf("metric missing in response:\n%s", string(body))
		}
	})

	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	cancel()
	if err := <-doneCh; err != nil {
		t.Errorf("server error: %v", err)
	}
}

func TestServer_EmptyAddr_IsNoOp(t *testing.T) {
	srv := NewServer("", New(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- srv.Run(ctx) }()

	cancel()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func noop(m *Metrics) {
	_ = m.Registry()
}
