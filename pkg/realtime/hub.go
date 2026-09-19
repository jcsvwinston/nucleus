// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package realtime pushes messages to connected clients: topics, broadcast,
// presence, and the two transports a browser understands.
//
// It exists because an application had to bring all of it. The framework
// exempted long responses from the write timeout and stopped there, so
// anything live — a progress bar, a notification, a dashboard that updates —
// meant hand-rolling the upgrade, the framing, the ping/pong, the map of
// connections and the fan-out, in every application that wanted it. Rails has
// Action Cable, Phoenix has Channels, Django has Channels; this is that.
//
// The shape is deliberately small: a Hub owns topics, a client subscribes to
// one, and a broadcast reaches every subscriber of that topic in this process.
// Reaching the subscribers of OTHER replicas is a relay, which is a separate
// piece by design — most applications run one process until they do not.
package realtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// DefaultClientBuffer is how many messages a slow client may fall behind
// before it is disconnected.
//
// Disconnecting is the policy, and it is deliberate: the alternative is
// blocking the broadcaster — which makes one stalled browser everybody's
// problem — or growing without bound, which makes it the process's. A client
// that cannot keep up reconnects and resubscribes, which is what every
// browser-side client library already does.
const DefaultClientBuffer = 64

// Message is what a broadcast carries.
type Message struct {
	// Topic is the channel this belongs to.
	Topic string
	// Event names the kind of message inside the topic. It reaches an
	// EventSource client as the SSE event name.
	Event string
	// Data is the payload, already encoded.
	Data []byte
	// ID is optional; an SSE client sends it back as Last-Event-ID after a
	// reconnection.
	ID string
}

// Client is one connected subscriber.
type Client struct {
	// ID identifies this connection. Two connections of the same user have
	// different ids.
	ID string
	// User is who is behind the connection, when the application knows. It is
	// what presence reports and what authorisation is decided on.
	User string
	// Topics is what this client subscribed to.
	Topics []string

	send      chan Message
	closeOnce sync.Once
	closed    chan struct{}
	dropped   int64
	mu        sync.Mutex
}

// Send returns the channel a transport reads to write to this client.
func (c *Client) Send() <-chan Message { return c.send }

// Closed is closed when the client is disconnected, so a transport can stop
// without polling.
func (c *Client) Closed() <-chan struct{} { return c.closed }

// Dropped reports how many messages this client missed because it could not
// keep up. A transport can report it on disconnect; a silent drop is how a
// live view becomes subtly wrong.
func (c *Client) Dropped() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped
}

func (c *Client) close() {
	c.closeOnce.Do(func() { close(c.closed) })
}

// Hub owns the topics and the clients subscribed to them.
type Hub struct {
	logger *slog.Logger

	mu      sync.RWMutex
	clients map[string]*Client            // by client id
	topics  map[string]map[string]*Client // topic -> client id -> client

	// relay carries broadcasts to and from other replicas. Nil means this
	// process only.
	relay Relay

	buffer int
}

// Config configures a Hub.
type Config struct {
	Logger *slog.Logger
	// ClientBuffer overrides DefaultClientBuffer.
	ClientBuffer int
	// Relay, when set, carries broadcasts between replicas.
	Relay Relay
}

// New builds a Hub.
func New(cfg Config) *Hub {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	buffer := cfg.ClientBuffer
	if buffer <= 0 {
		buffer = DefaultClientBuffer
	}
	h := &Hub{
		logger:  logger,
		clients: map[string]*Client{},
		topics:  map[string]map[string]*Client{},
		relay:   cfg.Relay,
		buffer:  buffer,
	}
	return h
}

// ErrNoTopics reports a subscription to nothing.
var ErrNoTopics = errors.New("realtime: a client must subscribe to at least one topic")

// Subscribe registers a client on the given topics and returns it.
func (h *Hub) Subscribe(id, user string, topics ...string) (*Client, error) {
	if len(topics) == 0 {
		return nil, ErrNoTopics
	}
	if id == "" {
		return nil, errors.New("realtime: a client needs an id")
	}
	client := &Client{
		ID:     id,
		User:   user,
		Topics: append([]string(nil), topics...),
		send:   make(chan Message, h.buffer),
		closed: make(chan struct{}),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.clients[id]; exists {
		return nil, fmt.Errorf("realtime: client %q is already connected", id)
	}
	h.clients[id] = client
	for _, topic := range topics {
		if h.topics[topic] == nil {
			h.topics[topic] = map[string]*Client{}
		}
		h.topics[topic][id] = client
	}
	return client, nil
}

// Unsubscribe disconnects a client and forgets it.
func (h *Hub) Unsubscribe(id string) {
	h.mu.Lock()
	client, ok := h.clients[id]
	if ok {
		delete(h.clients, id)
		for _, topic := range client.Topics {
			if subs := h.topics[topic]; subs != nil {
				delete(subs, id)
				if len(subs) == 0 {
					delete(h.topics, topic)
				}
			}
		}
	}
	h.mu.Unlock()
	if ok {
		client.close()
	}
}

// Broadcast sends a message to every subscriber of its topic, here and — when
// a relay is configured — on every other replica.
//
// It never blocks on a slow client: a client whose buffer is full is
// disconnected, because the alternative is one stalled browser holding up
// everybody's messages.
func (h *Hub) Broadcast(ctx context.Context, msg Message) {
	h.deliverLocal(msg)
	if h.relay != nil {
		if err := h.relay.Publish(ctx, msg); err != nil {
			h.logger.Error("realtime: could not relay a broadcast to the other replicas",
				"error", err, "topic", msg.Topic)
		}
	}
}

// deliverLocal is the half that reaches this process's clients. The relay
// calls it for messages that arrive from elsewhere, so a relayed message is
// not published back out.
func (h *Hub) deliverLocal(msg Message) {
	h.mu.RLock()
	subs := make([]*Client, 0, len(h.topics[msg.Topic]))
	for _, c := range h.topics[msg.Topic] {
		subs = append(subs, c)
	}
	h.mu.RUnlock()

	var slow []string
	for _, client := range subs {
		select {
		case client.send <- msg:
		default:
			client.mu.Lock()
			client.dropped++
			client.mu.Unlock()
			slow = append(slow, client.ID)
		}
	}
	for _, id := range slow {
		h.logger.Warn("realtime: disconnecting a client that cannot keep up",
			"client", id, "topic", msg.Topic, "buffer", h.buffer)
		h.Unsubscribe(id)
	}
}

// PresenceEntry is one connection, as presence reports it. It is a plain value
// on purpose: a Client owns channels and locks, and handing those out would
// invite a caller to copy them.
type PresenceEntry struct {
	// ID is the connection.
	ID string
	// User is who is behind it, when the application said so.
	User string
	// Topics is what this connection subscribed to.
	Topics []string
}

// Presence reports who is connected to a topic, one entry per CONNECTION.
// Two tabs of one person are two entries, which is what a "3 devices" badge
// needs; count distinct users for "who is here".
func (h *Hub) Presence(topic string) []PresenceEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	subs := h.topics[topic]
	out := make([]PresenceEntry, 0, len(subs))
	for _, c := range subs {
		out = append(out, PresenceEntry{ID: c.ID, User: c.User, Topics: append([]string(nil), c.Topics...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Topics lists the topics with at least one subscriber here.
func (h *Hub) Topics() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.topics))
	for topic := range h.topics {
		out = append(out, topic)
	}
	sort.Strings(out)
	return out
}

// Count reports how many clients are subscribed to a topic here.
func (h *Hub) Count(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic])
}

// Close disconnects everybody.
func (h *Hub) Close() error {
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = map[string]*Client{}
	h.topics = map[string]map[string]*Client{}
	h.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
	if h.relay != nil {
		return h.relay.Close()
	}
	return nil
}

// Relay carries broadcasts between replicas.
//
// It is an interface, not a Redis client, because a hub with one process needs
// none and an application that already runs a broker should not be made to run
// a second one.
type Relay interface {
	// Publish sends a message to the other replicas.
	Publish(ctx context.Context, msg Message) error
	// Subscribe delivers messages from other replicas to deliver, until ctx
	// is done. It must not echo back what this replica published.
	Subscribe(ctx context.Context, deliver func(Message)) error
	// Close releases the relay.
	Close() error
}

// StartRelay wires an incoming relay into the hub: messages from other
// replicas are delivered locally, and not published back out.
func (h *Hub) StartRelay(ctx context.Context) error {
	if h.relay == nil {
		return nil
	}
	go func() {
		if err := h.relay.Subscribe(ctx, h.deliverLocal); err != nil && ctx.Err() == nil {
			h.logger.Error("realtime: the relay stopped", "error", err)
		}
	}()
	// A moment for the subscription to establish, so a broadcast issued
	// immediately after is not missed by the replicas.
	time.Sleep(10 * time.Millisecond)
	return nil
}
