// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"fmt"
	"net/http"
	"time"
)

// WSConfig configures one WebSocket channel.
type WSConfig struct {
	// Hub is where the client subscribes.
	Hub *Hub
	// Topics the client subscribes to.
	Topics []string
	// ClientID identifies the connection; empty derives one.
	ClientID string
	// User is who is behind it.
	User string
	// PingInterval overrides DefaultPingInterval.
	PingInterval time.Duration
	// Upgrade configures the handshake, including the origin check.
	Upgrade UpgradeConfig
	// OnMessage receives what the client sends. Nil ignores inbound messages,
	// which is the right default for a one-way channel: a client that can
	// push into the server needs the application to say what that means.
	OnMessage func(client *Client, data []byte) error
	// OnConnect runs once subscribed, before the first broadcast.
	OnConnect func(send func(Message))
}

// ServeWS upgrades the request and streams a hub topic over WebSocket.
//
// Authorisation happens BEFORE this is called, in the handler, the way it does
// for any other route — a channel is a route, and giving it a second
// authorisation mechanism is how the two drift apart. What the handler decides,
// it passes in as User.
func ServeWS(w http.ResponseWriter, r *http.Request, cfg WSConfig) error {
	if cfg.Hub == nil {
		http.Error(w, "realtime: no hub configured", http.StatusInternalServerError)
		return fmt.Errorf("realtime: ServeWS needs a hub")
	}
	if len(cfg.Topics) == 0 {
		http.Error(w, "a channel needs a topic", http.StatusBadRequest)
		return ErrNoTopics
	}

	conn, err := Upgrade(w, r, cfg.Upgrade)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	clientID := cfg.ClientID
	if clientID == "" {
		clientID = newID()
	}
	client, err := cfg.Hub.Subscribe(clientID, cfg.User, cfg.Topics...)
	if err != nil {
		return err
	}
	defer cfg.Hub.Unsubscribe(clientID)

	if cfg.OnConnect != nil {
		cfg.OnConnect(func(msg Message) {
			_ = conn.WriteMessage(msg.Data)
		})
	}

	// Inbound: its own goroutine, because a read blocks. It also answers the
	// peer's pings, which is why it runs even when the application ignores
	// what the client sends.
	inbound := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				inbound <- err
				return
			}
			if cfg.OnMessage != nil {
				if err := cfg.OnMessage(client, data); err != nil {
					inbound <- err
					return
				}
			}
		}
	}()

	pingInterval := cfg.PingInterval
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-inbound:
			// The peer closed, or the connection broke. Either way this
			// client is gone.
			return nil
		case <-client.Closed():
			return nil
		case msg := <-client.Send():
			if err := conn.WriteMessage(msg.Data); err != nil {
				return nil
			}
		case <-ticker.C:
			if err := conn.Ping(); err != nil {
				return nil
			}
		}
	}
}
