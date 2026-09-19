// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/realtime"
)

// The real-time probes measure what an application can serve over a long-lived
// connection. Two of them drive a real client against a real route; the rest
// ask the contract a module is handed — `nucleus.Runtime` — because a capability
// a module would receive from the framework is exactly the set of methods the
// framework publishes on it. Reading that interface is not reading the
// implementation: it is the whole public answer to "what do I get?".

// realtimeServer boots an application with one module that streams, one that
// tries to speak WebSocket, and one that holds a connection open.
func realtimeServer(t *testing.T) *nucleustest.Server {
	t.Helper()
	mod := nucleus.Module[struct{}]{
		Name:       "benchrt",
		CSRFExempt: []string{"/sse", "/ws"},
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/sse", Action: "read"},
			{Subject: "anonymous", Object: "/ws", Action: "read"},
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/sse", func(c *nucleus.Context) error {
				c.Writer.Header().Set("Content-Type", "text/event-stream")
				c.Writer.Header().Set("Cache-Control", "no-cache")
				c.Writer.WriteHeader(http.StatusOK)
				flusher, ok := c.Writer.(http.Flusher)
				if !ok {
					return fmt.Errorf("no flusher")
				}
				for i := 0; i < 3; i++ {
					_, _ = fmt.Fprintf(c.Writer, "data: tick-%d\n\n", i)
					flusher.Flush()
					time.Sleep(50 * time.Millisecond)
				}
				return nil
			})
			r.Get("/ws", func(c *nucleus.Context) error {
				// What the framework offers for an upgrade is what this
				// handler can use. There is no upgrader, so the only thing
				// left is the raw connection — if the writer even yields one.
				hj, ok := c.Writer.(http.Hijacker)
				if !ok {
					return c.JSON(http.StatusOK, map[string]string{"hijack": "unsupported"})
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					return c.JSON(http.StatusOK, map[string]string{"hijack": "failed"})
				}
				defer func() { _ = conn.Close() }()
				// A raw 101 without the accept-key computation is not a
				// handshake; it only shows the socket is reachable.
				_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
				return nil
			})
		},
	}
	return nucleustest.StartApp(t, nucleus.App{
		Config:  benchConfig(t),
		Modules: map[string]nucleus.ModuleSpec{"benchrt": mod.Build()},
	})
}

// probeWebSocketUpgrade measures whether an application can complete a
// WebSocket handshake with what the framework gives it.
func probeWebSocketUpgrade(t *testing.T, _ *env) verdict {
	hub := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler)})
	defer func() { _ = hub.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = realtime.ServeWS(w, r, realtime.WSConfig{
			Hub: hub, Topics: []string{"bench"}, PingInterval: time.Hour,
		})
	}))
	defer srv.Close()

	conn, status, accept, err := benchDialWS(srv.URL, nil)
	if err != nil {
		t.Logf("the upgrade failed: %v", err)
		return absent
	}
	defer func() { _ = conn.Close() }()
	t.Logf("handshake: %s, Sec-WebSocket-Accept present: %v", status, accept != "")
	if !strings.Contains(status, "101") {
		return absent
	}
	if accept == "" {
		// The socket is reachable but the framework computed no accept key:
		// the application still owes itself the protocol.
		return partial
	}
	return present
}

// probeSSEStream measures a server-sent-event stream reaching a client
// incrementally, with the framework doing the streaming rather than the
// application hand-writing headers and flushes.
func probeSSEStream(t *testing.T, _ *env) verdict {
	hub := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler)})
	defer func() { _ = hub.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = realtime.ServeSSE(w, r, realtime.SSEConfig{
			Hub: hub, Topics: []string{"bench"}, ClientID: "bench-sse", KeepAlive: time.Hour,
		})
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("the stream request failed: %v", err)
		return absent
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Logf("the stream answered %d", resp.StatusCode)
		return absent
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Logf("content-type %q", ct)
		return partial
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hub.Count("bench") == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	hub.Broadcast(context.Background(), realtime.Message{Topic: "bench", Event: "tick", Data: []byte(`{"n":1}`)})

	reader := bufio.NewReader(resp.Body)
	got := ""
	for i := 0; i < 8; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "data:") {
			got = strings.TrimSpace(line)
			break
		}
	}
	t.Logf("the framework streamed %q to an EventSource client", got)
	if !strings.Contains(got, `{"n":1}`) {
		return absent
	}
	return present
}

// probeStreamSurvivesTimeout measures that the default write timeout does not
// cut a long stream — the exemption the router already carries.
func probeStreamSurvivesTimeout(t *testing.T, _ *env) verdict {
	srv := realtimeServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL("/sse"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Logf("the stream request failed: %v", err)
		return absent
	}
	defer func() { _ = resp.Body.Close() }()
	reader := bufio.NewReader(resp.Body)
	seen := 0
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "data:") {
			seen++
		}
		_, _ = reader.ReadString('\n') // blank separator
	}
	t.Logf("%d of 3 events survived the middleware stack", seen)
	if seen == 3 {
		return present
	}
	return absent
}

// runtimeMethods lists what a module receives from the framework. It is the
// measurement behind the channel controls: a capability the framework hands a
// module is a method on this interface, and there is no other door.
func runtimeMethods() []string {
	rt := reflect.TypeOf((*nucleus.Runtime)(nil)).Elem()
	out := make([]string, 0, rt.NumMethod())
	for i := 0; i < rt.NumMethod(); i++ {
		out = append(out, rt.Method(i).Name)
	}
	return out
}

// runtimeHas reports whether the module-facing contract offers any method
// whose name contains one of the given fragments.
func runtimeHas(fragments ...string) (string, bool) {
	for _, m := range runtimeMethods() {
		lower := strings.ToLower(m)
		for _, f := range fragments {
			if strings.Contains(lower, f) {
				return m, true
			}
		}
	}
	return "", false
}

// probeChannelBroadcast measures a way to push one message to every client
// subscribed to a topic — Phoenix Channels, Action Cable, Django Channels.
func probeChannelBroadcast(t *testing.T, _ *env) verdict {
	hub := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler)})
	defer func() { _ = hub.Close() }()

	a, err := hub.Subscribe("a", "ana", "room")
	if err != nil {
		t.Logf("subscribe: %v", err)
		return absent
	}
	b, err := hub.Subscribe("b", "ben", "room")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	hub.Broadcast(context.Background(), realtime.Message{Topic: "room", Data: []byte("hi")})

	for _, client := range []*realtime.Client{a, b} {
		select {
		case <-client.Send():
		case <-time.After(time.Second):
			t.Log("a subscriber did not receive the broadcast")
			return absent
		}
	}
	t.Log("one broadcast reached both subscribers of the topic")
	return present
}

// probeChannelAuth measures authenticating a subscriber the way a route is
// authenticated — the handler decides, and what it decides travels with the
// connection.
func probeChannelAuth(t *testing.T, _ *env) verdict {
	hub := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler)})
	defer func() { _ = hub.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exactly what a route does: decide first, then serve.
		user := r.Header.Get("X-Bench-User")
		if user == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		_ = realtime.ServeWS(w, r, realtime.WSConfig{
			Hub: hub, Topics: []string{"private"}, User: user, PingInterval: time.Hour,
		})
	}))
	defer srv.Close()

	// Refused without identity.
	_, status, _, err := benchDialWS(srv.URL, nil)
	if err == nil && strings.Contains(status, "101") {
		t.Log("an unauthenticated client was allowed to join a private channel")
		return absent
	}
	// Accepted with it, and presence knows who it is.
	conn, status, _, err := benchDialWS(srv.URL, map[string]string{"X-Bench-User": "ana"})
	if err != nil || !strings.Contains(status, "101") {
		t.Logf("an authenticated client was refused: %v %s", err, status)
		return absent
	}
	defer func() { _ = conn.Close() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hub.Count("private") == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	joined := hub.Presence("private")
	if len(joined) != 1 || joined[0].User != "ana" {
		t.Logf("presence after an authenticated join: %+v", joined)
		return partial
	}
	t.Log("the channel refused an anonymous join and carried the identity of the authenticated one")
	return present
}

// probeChannelPresence measures knowing who is connected to a topic.
func probeChannelPresence(t *testing.T, _ *env) verdict {
	hub := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler)})
	defer func() { _ = hub.Close() }()

	for _, spec := range []struct{ id, user string }{
		{"tab-1", "ana"}, {"tab-2", "ana"}, {"tab-3", "ben"},
	} {
		if _, err := hub.Subscribe(spec.id, spec.user, "room"); err != nil {
			t.Logf("subscribe: %v", err)
			return absent
		}
	}
	present := hub.Presence("room")
	users := map[string]int{}
	for _, entry := range present {
		users[entry.User]++
	}
	t.Logf("presence on the topic: %d connections, %d distinct people (%v)", len(present), len(users), users)
	if len(present) != 3 || users["ana"] != 2 {
		return absent
	}
	return present3(users)
}

// present3 keeps the verdict decision next to what it is about: presence is
// per CONNECTION, so two tabs of one person must be two entries — that is what
// a device list needs, and collapsing them would be a different answer.
func present3(users map[string]int) verdict {
	if users["ana"] == 2 && users["ben"] == 1 {
		return present
	}
	return partial
}

// probeChannelRelay measures a broadcast reaching clients attached to ANOTHER
// replica — the thing that makes real time work behind a load balancer.
func probeChannelRelay(t *testing.T, _ *env) verdict {
	server := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relayA, err := realtime.NewRedisRelay(realtime.RedisRelayConfig{URL: "redis://" + server.Addr(), Origin: "replica-a"})
	if err != nil {
		t.Logf("building the relay: %v", err)
		return absent
	}
	relayB, err := realtime.NewRedisRelay(realtime.RedisRelayConfig{URL: "redis://" + server.Addr(), Origin: "replica-b"})
	if err != nil {
		t.Fatalf("building the second relay: %v", err)
	}
	hubA := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler), Relay: relayA})
	defer func() { _ = hubA.Close() }()
	hubB := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler), Relay: relayB})
	defer func() { _ = hubB.Close() }()
	if err := hubA.StartRelay(ctx); err != nil {
		t.Fatalf("start relay A: %v", err)
	}
	if err := hubB.StartRelay(ctx); err != nil {
		t.Fatalf("start relay B: %v", err)
	}

	// Connected to one replica, broadcast from the other.
	client, err := hubB.Subscribe("b-1", "ana", "orders")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	hubA.Broadcast(ctx, realtime.Message{Topic: "orders", Data: []byte(`{"id":1}`)})

	select {
	case msg := <-client.Send():
		t.Logf("a broadcast on replica A reached the client on replica B: %s", msg.Data)
		return present
	case <-time.After(3 * time.Second):
		t.Log("with no relay, live works until the second replica comes up and then works for half the users")
		return absent
	}
}

// probeChannelTestKit measures a test helper for a long-lived connection —
// what nucleustest offers a stream, next to what it offers a request.
func probeChannelTestKit(t *testing.T, _ *env) verdict {
	rt := reflect.TypeOf(&nucleustest.Server{})
	var names []string
	for i := 0; i < rt.NumMethod(); i++ {
		names = append(names, rt.Method(i).Name)
	}
	for _, n := range names {
		lower := strings.ToLower(n)
		if strings.Contains(lower, "stream") || strings.Contains(lower, "socket") || strings.Contains(lower, "channel") {
			t.Logf("nucleustest.Server offers %s", n)
			return present
		}
	}
	t.Logf("nucleustest.Server has no stream helper: %s", strings.Join(names, ", "))
	return absent
}
