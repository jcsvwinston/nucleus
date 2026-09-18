// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"bufio"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
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
	srv := realtimeServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL("/ws"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Logf("the upgrade request failed outright: %v", err)
		return absent
	}
	defer func() { _ = resp.Body.Close() }()
	t.Logf("a route that hijacks the connection answers %d; the framework computed no accept key", resp.StatusCode)
	if resp.StatusCode == http.StatusSwitchingProtocols {
		// The socket is reachable, but the handshake was hand-rolled by the
		// probe: the framework contributed a predicate, not an upgrader.
		return partial
	}
	return absent
}

// probeSSEStream measures a server-sent-event stream reaching a client
// incrementally — the first event before the handler returns.
func probeSSEStream(t *testing.T, _ *env) verdict {
	srv := realtimeServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL("/sse"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	start := time.Now()
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Logf("the stream request failed: %v", err)
		return absent
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Logf("the stream route answered %d", resp.StatusCode)
		return absent
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Logf("reading the first event: %v", err)
		return absent
	}
	t.Logf("first event %q arrived after %v (hand-written handler: header, Flush, loop)",
		strings.TrimSpace(line), time.Since(start).Round(time.Millisecond))
	if !strings.HasPrefix(line, "data:") {
		return absent
	}
	// It streams, but every line of it is the application's: there is no
	// channel, no helper, no broadcast.
	return partial
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
	if m, ok := runtimeHas("channel", "broadcast", "publish"); ok {
		t.Logf("the module runtime offers %s", m)
		return present
	}
	t.Logf("nothing on the module runtime broadcasts: %s", strings.Join(runtimeMethods(), ", "))
	return absent
}

// probeChannelAuth measures authenticating a subscriber the way a route is
// authenticated — by session or API key — before it joins a topic.
func probeChannelAuth(t *testing.T, _ *env) verdict {
	if _, ok := runtimeHas("channel", "broadcast"); ok {
		return partial
	}
	t.Log("there is no channel to authorise joining")
	return absent
}

// probeChannelPresence measures knowing who is connected to a topic.
func probeChannelPresence(t *testing.T, _ *env) verdict {
	if m, ok := runtimeHas("presence"); ok {
		t.Logf("the module runtime offers %s", m)
		return present
	}
	return absent
}

// probeChannelRelay measures a broadcast reaching clients attached to ANOTHER
// replica — the thing that makes real time work behind a load balancer.
func probeChannelRelay(t *testing.T, _ *env) verdict {
	if _, ok := runtimeHas("channel", "broadcast"); ok {
		return partial
	}
	t.Log("with no channel in the framework there is nothing to relay between replicas")
	return absent
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
