// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func wsServer(t *testing.T, hub *Hub, cfg WSConfig) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := cfg
		c.Hub = hub
		_ = ServeWS(w, r, c)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The handshake is a real one: 101, and an accept key computed from the
// client's — which is the whole point of the key, and what a framework that
// only offers a hijackable writer leaves the application to do.
func TestWS_HandshakeIsComplete(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{Topics: []string{"orders"}, PingInterval: time.Hour})

	client, line := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()

	if !strings.Contains(line.Raw, "101") {
		t.Fatalf("handshake answered %q", line.Raw)
	}
	if line.Accept == "" {
		t.Fatal("the server sent no Sec-WebSocket-Accept")
	}
	if line.Accept != line.ExpectedAccept {
		t.Fatalf("accept key %q, want %q computed from the client's key", line.Accept, line.ExpectedAccept)
	}
}

// A broadcast reaches a connected browser.
func TestWS_BroadcastReachesTheClient(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{Topics: []string{"orders"}, PingInterval: time.Hour})

	client, _ := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()
	waitFor(t, 2*time.Second, func() bool { return hub.Count("orders") == 1 })

	hub.Broadcast(context.Background(), Message{Topic: "orders", Data: []byte(`{"id":7}`)})
	opcode, payload, err := client.read(3 * time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != opText {
		t.Fatalf("opcode %d", opcode)
	}
	if string(payload) != `{"id":7}` {
		t.Fatalf("payload %q", payload)
	}
}

// A ping is answered with a pong carrying the same payload, which is what
// keeps a connection alive through a proxy that closes idle ones.
func TestWS_PingIsAnswered(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{Topics: []string{"orders"}, PingInterval: time.Hour})

	client, _ := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()
	waitFor(t, 2*time.Second, func() bool { return hub.Count("orders") == 1 })

	if err := client.write(opPing, []byte("are you there")); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err := client.read(3 * time.Second)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if opcode != opPong {
		t.Fatalf("answered opcode %d, want a pong", opcode)
	}
	if string(payload) != "are you there" {
		t.Fatalf("the pong carried %q", payload)
	}
}

// What a client sends reaches the application, unmasked.
func TestWS_InboundMessage(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	got := make(chan string, 1)
	srv := wsServer(t, hub, WSConfig{
		Topics: []string{"chat"}, PingInterval: time.Hour,
		OnMessage: func(_ *Client, data []byte) error {
			got <- string(data)
			return nil
		},
	})

	client, _ := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()
	waitFor(t, 2*time.Second, func() bool { return hub.Count("chat") == 1 })

	if err := client.write(opText, []byte("hello from the browser")); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg != "hello from the browser" {
			t.Fatalf("the handler received %q — unmasking is wrong", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("what the client sent never reached the application")
	}
}

// A cross-origin handshake is refused by default. A browser sends cookies with
// a WebSocket handshake and does not apply CORS to it, so a permissive upgrade
// is a cross-site request carrying the user's session.
func TestWS_CrossOriginIsRefusedByDefault(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{Topics: []string{"orders"}, PingInterval: time.Hour})

	_, line := dialWS(t, srv.URL, map[string]string{"Origin": "https://evil.example"})
	if strings.Contains(line.Raw, "101") {
		t.Fatal("a cross-origin handshake was accepted with the default policy")
	}
	if !strings.Contains(line.Raw, "403") {
		t.Fatalf("cross-origin handshake answered %q, want 403", line.Raw)
	}
}

// An application that genuinely serves another origin says so, and then it
// works.
func TestWS_CrossOriginAllowedExplicitly(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{
		Topics: []string{"orders"}, PingInterval: time.Hour,
		Upgrade: UpgradeConfig{CheckOrigin: func(r *http.Request) bool {
			return r.Header.Get("Origin") == "https://app.example"
		}},
	})

	_, line := dialWS(t, srv.URL, map[string]string{"Origin": "https://app.example"})
	if !strings.Contains(line.Raw, "101") {
		t.Fatalf("an explicitly allowed origin was refused: %q", line.Raw)
	}
}

// A frame announcing more than the limit is refused BEFORE the bytes are
// allocated: the length is attacker controlled.
func TestWS_OversizedFrameIsRefused(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{
		Topics: []string{"orders"}, PingInterval: time.Hour,
		Upgrade: UpgradeConfig{MaxMessageBytes: 1024},
	})

	client, _ := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()
	waitFor(t, 2*time.Second, func() bool { return hub.Count("orders") == 1 })

	// Announce a gigabyte without sending it.
	header := []byte{0x80 | opText, 0x80 | 127, 0, 0, 0, 0, 0x40, 0, 0, 0}
	mask := maskKeyForClient()
	header = append(header, mask[:]...)
	if _, err := client.conn.Write(header); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err := client.read(3 * time.Second)
	if err != nil {
		// A closed connection is also an acceptable refusal.
		return
	}
	if opcode != opClose {
		t.Fatalf("the server answered opcode %d to an oversized frame, want a close", opcode)
	}
	if len(payload) >= 2 {
		code := int(payload[0])<<8 | int(payload[1])
		if code != 1009 {
			t.Errorf("close code %d, want 1009 (message too big)", code)
		}
	}
}

// An unmasked client frame is a protocol error: every frame from a client MUST
// be masked, and accepting one means accepting something that is not a browser
// pretending to be one.
func TestWS_UnmaskedClientFrameIsRefused(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	srv := wsServer(t, hub, WSConfig{Topics: []string{"orders"}, PingInterval: time.Hour})

	client, _ := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()
	waitFor(t, 2*time.Second, func() bool { return hub.Count("orders") == 1 })

	// A text frame with no mask bit.
	if _, err := client.conn.Write([]byte{0x80 | opText, 5, 'h', 'e', 'l', 'l', 'o'}); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err := client.read(3 * time.Second)
	if err != nil {
		return // closed outright: also a refusal
	}
	if opcode != opClose {
		t.Fatalf("the server answered opcode %d to an unmasked frame", opcode)
	}
	if len(payload) >= 2 {
		if code := int(payload[0])<<8 | int(payload[1]); code != 1002 {
			t.Errorf("close code %d, want 1002 (protocol error)", code)
		}
	}
}

// A fragmented message is reassembled: a client that splits a payload gets it
// delivered whole, not in pieces.
func TestWS_FragmentedMessageIsReassembled(t *testing.T) {
	hub := New(Config{Logger: quiet()})
	defer func() { _ = hub.Close() }()
	got := make(chan string, 1)
	srv := wsServer(t, hub, WSConfig{
		Topics: []string{"chat"}, PingInterval: time.Hour,
		OnMessage: func(_ *Client, data []byte) error {
			got <- string(data)
			return nil
		},
	})

	client, _ := dialWS(t, srv.URL, nil)
	defer func() { _ = client.conn.Close() }()
	waitFor(t, 2*time.Second, func() bool { return hub.Count("chat") == 1 })

	writeFragment := func(opcode byte, fin bool, payload []byte) {
		first := opcode
		if fin {
			first |= 0x80
		}
		header := []byte{first, byte(0x80 | len(payload))}
		mask := maskKeyForClient()
		header = append(header, mask[:]...)
		masked := make([]byte, len(payload))
		for i := range payload {
			masked[i] = payload[i] ^ mask[i%4]
		}
		if _, err := client.conn.Write(append(header, masked...)); err != nil {
			t.Fatal(err)
		}
	}
	writeFragment(opText, false, []byte("one "))
	writeFragment(opContinuation, false, []byte("two "))
	writeFragment(opContinuation, true, []byte("three"))

	select {
	case msg := <-got:
		if msg != "one two three" {
			t.Fatalf("reassembled as %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a fragmented message never arrived whole")
	}
}
