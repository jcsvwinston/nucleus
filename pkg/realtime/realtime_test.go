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
	"sync"
	"testing"
	"time"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// One message reaches every subscriber of the topic, and nobody else.
func TestHub_BroadcastReachesTheTopic(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()

	a, err := hub.Subscribe("a", "ana", "orders")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hub.Subscribe("b", "ben", "orders")
	if err != nil {
		t.Fatal(err)
	}
	other, err := hub.Subscribe("c", "carl", "invoices")
	if err != nil {
		t.Fatal(err)
	}

	hub.Broadcast(context.Background(), Message{Topic: "orders", Event: "created", Data: []byte(`{"id":1}`)})

	for name, client := range map[string]*Client{"a": a, "b": b} {
		select {
		case msg := <-client.Send():
			if msg.Event != "created" {
				t.Errorf("%s received %q", name, msg.Event)
			}
		case <-time.After(time.Second):
			t.Errorf("%s never received the broadcast", name)
		}
	}
	select {
	case msg := <-other.Send():
		t.Errorf("a subscriber of another topic received %v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

// A client that cannot keep up is disconnected, not allowed to block the
// broadcaster or grow the process.
func TestHub_SlowClientIsDisconnected(t *testing.T) {
	hub := New(Config{Logger: quiet(), ClientBuffer: 2})
	defer func() { _ = hub.Close() }()

	client, err := hub.Subscribe("slow", "ana", "firehose")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			hub.Broadcast(context.Background(), Message{Topic: "firehose", Data: []byte("x")})
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Broadcast blocked on a client that stopped reading")
	}
	select {
	case <-client.Closed():
	case <-time.After(time.Second):
		t.Fatal("the slow client was not disconnected")
	}
	if client.Dropped() == 0 {
		t.Error("the disconnected client reports no dropped messages")
	}
}

// Presence answers who is connected, one entry per connection: two tabs of one
// person are two entries, which is what a device list needs.
func TestHub_Presence(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()

	if _, err := hub.Subscribe("tab-1", "ana", "room"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Subscribe("tab-2", "ana", "room"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Subscribe("tab-3", "ben", "room"); err != nil {
		t.Fatal(err)
	}

	present := hub.Presence("room")
	if len(present) != 3 {
		t.Fatalf("presence reports %d connections, want 3", len(present))
	}
	users := map[string]int{}
	for _, c := range present {
		users[c.User]++
	}
	if users["ana"] != 2 || users["ben"] != 1 {
		t.Errorf("presence by user: %v", users)
	}
	if hub.Count("room") != 3 {
		t.Errorf("count=%d", hub.Count("room"))
	}

	hub.Unsubscribe("tab-2")
	if got := hub.Count("room"); got != 2 {
		t.Errorf("count after a disconnect: %d", got)
	}
}

// SSE: the stream reaches a real HTTP client, incrementally, with the headers
// that keep a proxy from buffering it.
func TestSSE_StreamsToAClient(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ServeSSE(w, r, SSEConfig{
			Hub: hub, Topics: []string{"orders"}, ClientID: "sse-1", User: "ana",
			KeepAlive: time.Hour,
			OnConnect: func(send func(Message)) {
				send(Message{Event: "hello", Data: []byte(`{"state":"current"}`)})
			},
		})
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("the response does not tell nginx to stop buffering, so the stream arrives in one lump")
	}

	reader := bufio.NewReader(resp.Body)
	// The state sent on connect, before anything was broadcast.
	if line := readEvent(t, reader); !strings.Contains(line, "hello") {
		t.Fatalf("first event: %q", line)
	}

	waitFor(t, time.Second, func() bool { return hub.Count("orders") == 1 })
	hub.Broadcast(context.Background(), Message{Topic: "orders", Event: "created", Data: []byte(`{"id":7}`)})
	if line := readEvent(t, reader); !strings.Contains(line, `{"id":7}`) {
		t.Fatalf("broadcast event: %q", line)
	}
}

// A multi-line payload arrives whole: every line needs its own data: prefix,
// and a client that gets one line of JSON gets a parse error.
func TestSSE_MultiLinePayload(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ServeSSE(w, r, SSEConfig{Hub: hub, Topics: []string{"logs"}, ClientID: "sse-2", KeepAlive: time.Hour})
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	reader := bufio.NewReader(resp.Body)

	waitFor(t, time.Second, func() bool { return hub.Count("logs") == 1 })
	hub.Broadcast(context.Background(), Message{Topic: "logs", Data: []byte("line one\nline two")})

	event := readEvent(t, reader)
	if !strings.Contains(event, "line one") || !strings.Contains(event, "line two") {
		t.Fatalf("a multi-line payload arrived as %q", event)
	}
}

// readEvent reads until the blank line that ends an SSE event, skipping the
// keep-alive comments.
func readEvent(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var event strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		trimmed := strings.TrimRight(line, "\n")
		if strings.HasPrefix(trimmed, ":") {
			continue
		}
		if trimmed == "" {
			if event.Len() > 0 {
				return event.String()
			}
			continue
		}
		event.WriteString(trimmed)
		event.WriteString("\n")
	}
	t.Fatal("no event arrived")
	return ""
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", limit)
}

var _ = sync.Mutex{}
