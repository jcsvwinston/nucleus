// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultKeepAlive is how often an idle stream sends a comment, so proxies and
// load balancers do not close a connection that is merely quiet.
const DefaultKeepAlive = 25 * time.Second

// SSEConfig configures one server-sent-events stream.
type SSEConfig struct {
	// Hub is where the client subscribes.
	Hub *Hub
	// Topics the client subscribes to. Empty means the handler answers 400:
	// a stream to nothing is a bug in the caller, not an empty stream.
	Topics []string
	// ClientID identifies the connection. Empty derives one.
	ClientID string
	// User is who is behind it, for presence and authorisation.
	User string
	// KeepAlive overrides DefaultKeepAlive.
	KeepAlive time.Duration
	// OnConnect runs once the client is subscribed, before the first message.
	// It is where an application sends the current state, so a client that
	// connects mid-stream is not left waiting for the next change.
	OnConnect func(send func(Message))
}

// ServeSSE streams a hub topic to one client over server-sent events.
//
// SSE rather than WebSocket wherever it fits: it is plain HTTP, so it crosses
// proxies that mangle upgrades, browsers reconnect on their own, and it needs
// no framing. The application writes none of it — the headers, the flush
// discipline, the keep-alive and the disconnect were what every live view had
// to hand-roll.
//
//	r.Get("/events", func(c *nucleus.Context) error {
//	        return realtime.ServeSSE(c.Writer, c.Request, realtime.SSEConfig{
//	                Hub: hub, Topics: []string{"orders"}, User: currentUser(c),
//	        })
//	})
func ServeSSE(w http.ResponseWriter, r *http.Request, cfg SSEConfig) error {
	if cfg.Hub == nil {
		http.Error(w, "realtime: no hub configured", http.StatusInternalServerError)
		return fmt.Errorf("realtime: ServeSSE needs a hub")
	}
	if len(cfg.Topics) == 0 {
		http.Error(w, "a stream needs a topic", http.StatusBadRequest)
		return ErrNoTopics
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return fmt.Errorf("realtime: the response writer cannot flush")
	}

	// http.Server.WriteTimeout is a deadline on the CONNECTION, counted from
	// the moment the request arrived — not a limit on how long a single write
	// may take. Left in place it severs every stream when it expires, whatever
	// the keep-alive does, because the keep-alive proves the stream is alive
	// and the deadline does not care. The framework defaults write_timeout to
	// 60s, so without this an SSE stream is cut after a minute and the client
	// sees an unexpected EOF it can only recover from by reconnecting.
	//
	// The deadline is cleared here rather than in the application's
	// configuration because a process usually serves streams AND ordinary
	// requests, and the ordinary ones want the timeout. If the writer does not
	// support it — a middleware that wraps without Unwrap, or a test recorder —
	// streaming still works; it is the deadline that stays.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("realtime: clearing the write deadline for the stream: %w", err)
	}

	clientID := cfg.ClientID
	if clientID == "" {
		clientID = newID()
	}
	client, err := cfg.Hub.Subscribe(clientID, cfg.User, cfg.Topics...)
	if err != nil {
		http.Error(w, "could not subscribe", http.StatusConflict)
		return err
	}
	defer cfg.Hub.Unsubscribe(clientID)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Nginx buffers proxied responses by default, which turns a live stream
	// into a batch that arrives when the connection closes.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	write := func(msg Message) {
		if msg.ID != "" {
			fmt.Fprintf(w, "id: %s\n", msg.ID)
		}
		if msg.Event != "" {
			fmt.Fprintf(w, "event: %s\n", msg.Event)
		}
		// Every line of a multi-line payload needs its own data: prefix, or
		// the client sees a truncated message.
		for _, line := range strings.Split(string(msg.Data), "\n") {
			fmt.Fprintf(w, "data: %s\n", line)
		}
		fmt.Fprint(w, "\n")
		flusher.Flush()
	}

	if cfg.OnConnect != nil {
		cfg.OnConnect(write)
	}

	keepAlive := cfg.KeepAlive
	if keepAlive <= 0 {
		keepAlive = DefaultKeepAlive
	}
	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-client.Closed():
			// Disconnected by the hub — usually because this client fell too
			// far behind. Saying so is better than a stream that just ends.
			if dropped := client.Dropped(); dropped > 0 {
				fmt.Fprintf(w, "event: nucleus.disconnected\ndata: {\"reason\":\"too slow\",\"dropped\":%d}\n\n", dropped)
				flusher.Flush()
			}
			return nil
		case msg := <-client.Send():
			write(msg)
		case <-ticker.C:
			// A comment: valid SSE, ignored by clients, and enough to keep a
			// proxy from deciding the connection is dead.
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// StreamContext is a convenience for a handler that wants the stream to end
// when its own context does.
func StreamContext(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg SSEConfig) error {
	return ServeSSE(w, r.WithContext(ctx), cfg)
}
