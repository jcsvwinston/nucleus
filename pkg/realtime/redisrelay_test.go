// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// A broadcast on one replica reaches the clients connected to another. Without
// it, "live" works until the second pod comes up and then works for half the
// users — the failure that looks like a flake and is not.
func TestRedisRelay_BroadcastCrossesReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relayA, err := NewRedisRelay(RedisRelayConfig{URL: "redis://" + server.Addr(), Origin: "replica-a"})
	if err != nil {
		t.Fatal(err)
	}
	relayB, err := NewRedisRelay(RedisRelayConfig{URL: "redis://" + server.Addr(), Origin: "replica-b"})
	if err != nil {
		t.Fatal(err)
	}

	hubA := New(Config{Logger: quiet(), Relay: relayA})
	defer func() { _ = hubA.Close() }()
	hubB := New(Config{Logger: quiet(), Relay: relayB})
	defer func() { _ = hubB.Close() }()

	if err := hubA.StartRelay(ctx); err != nil {
		t.Fatal(err)
	}
	if err := hubB.StartRelay(ctx); err != nil {
		t.Fatal(err)
	}

	// The client is on replica B; the broadcast happens on replica A.
	onB, err := hubB.Subscribe("b-1", "ana", "orders")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	hubA.Broadcast(ctx, Message{Topic: "orders", Event: "created", Data: []byte(`{"id":1}`)})

	select {
	case msg := <-onB.Send():
		if string(msg.Data) != `{"id":1}` {
			t.Fatalf("the relayed message arrived as %q", msg.Data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a broadcast on one replica never reached the client on the other")
	}
}

// A replica does not deliver its own broadcast twice: the hub delivers
// locally and then relays, so echoing the relayed copy back would show every
// client every message twice.
func TestRedisRelay_DoesNotEchoItsOwnBroadcast(t *testing.T) {
	server := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relay, err := NewRedisRelay(RedisRelayConfig{URL: "redis://" + server.Addr(), Origin: "replica-a"})
	if err != nil {
		t.Fatal(err)
	}
	hub := New(Config{Logger: quiet(), Relay: relay})
	defer func() { _ = hub.Close() }()
	if err := hub.StartRelay(ctx); err != nil {
		t.Fatal(err)
	}

	client, err := hub.Subscribe("a-1", "ana", "orders")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	hub.Broadcast(ctx, Message{Topic: "orders", Data: []byte("once")})

	select {
	case <-client.Send():
	case <-time.After(2 * time.Second):
		t.Fatal("the local delivery never happened")
	}
	select {
	case msg := <-client.Send():
		t.Fatalf("the same broadcast arrived twice: %q", msg.Data)
	case <-time.After(500 * time.Millisecond):
	}
}
