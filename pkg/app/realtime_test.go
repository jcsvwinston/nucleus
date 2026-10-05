// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/realtime"
)

func realtimeApp(t *testing.T, opts ...Option) (*App, *httptest.Server) {
	t.Helper()
	a, err := New(testAppConfig(), append(opts, WithRealtime())...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(func() {
		srv.Close()
		_ = a.Shutdown(context.Background())
	})
	if a.Realtime == nil {
		t.Fatal("WithRealtime built no hub")
	}
	return a, srv
}

// wsDial completes a WebSocket handshake against path and returns the
// connection, its reader and the status line.
func wsDial(t *testing.T, base, path string, header http.Header) (net.Conn, *bufio.Reader, string) {
	t.Helper()
	u, _ := url.Parse(base)
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var key [16]byte
	_, _ = rand.Read(key[:])
	var extra strings.Builder
	for k, vs := range header {
		for _, v := range vs {
			fmt.Fprintf(&extra, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n%s\r\n",
		path, u.Host, base64.StdEncoding.EncodeToString(key[:]), extra.String())
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read the handshake: %v", err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
	return conn, reader, strings.TrimSpace(status)
}

// wsFrame reads one unmasked text frame of under 126 bytes.
func wsFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	head := make([]byte, 2)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatalf("read a frame: %v", err)
	}
	payload := make([]byte, int(head[1]&0x7f))
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatalf("read the payload: %v", err)
	}
	return string(payload)
}

func waitSubscribed(t *testing.T, hub *realtime.Hub, topic string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for hub.Count(topic) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("nobody subscribed to %q", topic)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The api starter's stack: a WebSocket on /realtime/<topic> receives what is
// broadcast to the topic.
func TestWithRealtime_WebSocketChannel(t *testing.T) {
	a, srv := realtimeApp(t, WithoutDefaults())
	_, reader, status := wsDial(t, srv.URL, RealtimeChannelPath("news"), nil)
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake: %q, want 101", status)
	}
	waitSubscribed(t, a.Realtime, "news")
	a.Realtime.Broadcast(context.Background(), realtime.Message{Topic: "news", Event: "posted", Data: []byte(`{"id":1}`)})
	if got := wsFrame(t, reader); got != `{"id":1}` {
		t.Fatalf("frame %q", got)
	}
}

// The same route streams server-sent events to an EventSource.
func TestWithRealtime_EventStream(t *testing.T) {
	a, srv := realtimeApp(t, WithoutDefaults())
	req, _ := http.NewRequest(http.MethodGet, srv.URL+RealtimeChannelPath("news"), nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream answered %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	waitSubscribed(t, a.Realtime, "news")
	a.Realtime.Broadcast(context.Background(), realtime.Message{Topic: "news", Event: "posted", Data: []byte(`{"id":2}`)})
	lines := bufio.NewScanner(resp.Body)
	var got []string
	for lines.Scan() {
		got = append(got, lines.Text())
		if strings.HasPrefix(lines.Text(), "data:") {
			break
		}
	}
	if strings.Join(got, "\n") != "event: posted\ndata: {\"id\":2}" {
		t.Fatalf("stream %q", got)
	}
}

// What the route refuses: a topic a policy row could not spell, and a request
// that is neither an upgrade nor an EventSource.
func TestWithRealtime_Refusals(t *testing.T) {
	_, srv := realtimeApp(t, WithoutDefaults())
	for path, want := range map[string]int{
		RealtimeChannelPath("news"):                   http.StatusNotAcceptable,
		RealtimeChannelPath("-bad"):                   http.StatusBadRequest,
		RealtimeChannelPath("a%20b"):                  http.StatusBadRequest,
		RealtimeChannelPath(strings.Repeat("x", 129)): http.StatusBadRequest,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s: %d, want %d", path, resp.StatusCode, want)
		}
	}
}

// A channel is a route: on the default stack the default-deny layer decides
// who may subscribe, by the topic's path, and the identity it resolves is
// who the hub reports as present.
func TestWithRealtime_DefaultStackAuthorisesByPath(t *testing.T) {
	a, srv := realtimeApp(t, WithAPIKeys())
	if _, _, status := wsDial(t, srv.URL, RealtimeChannelPath("orders"), nil); !strings.Contains(status, "403") {
		t.Fatalf("an anonymous subscription with no policy: %q, want 403", status)
	}
	if err := a.Authorizer.AddPolicy("svc-dash", "/realtime/orders", "read"); err != nil {
		t.Fatal(err)
	}
	_, presented, err := apikeys.Issue(context.Background(), a.apiKeys, apikeys.Key{Name: "dash", OwnerID: "svc-dash"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, status := wsDial(t, srv.URL, RealtimeChannelPath("orders"), http.Header{apikeys.HeaderName: {presented}})
	if !strings.Contains(status, "101") {
		t.Fatalf("a subscription by the owner the policy names: %q, want 101", status)
	}
	waitSubscribed(t, a.Realtime, "orders")
	if presence := a.Realtime.Presence("orders"); len(presence) != 1 || presence[0].User != "svc-dash" {
		t.Fatalf("presence %+v, want svc-dash", presence)
	}
}

func TestWithRealtime_OffByDefault(t *testing.T) {
	a, err := New(testAppConfig(), WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.Realtime != nil {
		t.Fatal("a hub without WithRealtime")
	}
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, RealtimeChannelPath("news"), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the channel route without WithRealtime answered %d", rec.Code)
	}
}
