// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// DefaultRelayChannel is the Redis pub/sub channel broadcasts travel on. Two
// applications sharing one Redis database must set distinct prefixes, or
// better, use distinct databases.
const DefaultRelayChannel = "nucleus:realtime"

// RedisRelayConfig configures the relay.
type RedisRelayConfig struct {
	// URL is the Redis connection string.
	URL string
	// Channel overrides DefaultRelayChannel.
	Channel string
	// Origin identifies this replica, so it can ignore its own messages
	// coming back. Empty derives one.
	Origin string
}

// RedisRelay carries broadcasts between replicas over Redis pub/sub.
//
// Pub/sub rather than a stream, deliberately: a broadcast is only useful to
// the clients connected RIGHT NOW, and a replica that was down did not have
// any. Persisting messages nobody can receive would be a queue, and this
// package already points at the one the framework has.
type RedisRelay struct {
	client  *redis.Client
	channel string
	origin  string
}

// relayEnvelope is what travels. Data is base64 by virtue of being []byte in
// JSON, which is fine: a broadcast payload is bytes, not necessarily text.
type relayEnvelope struct {
	Origin string `json:"origin"`
	Topic  string `json:"topic"`
	Event  string `json:"event,omitempty"`
	ID     string `json:"id,omitempty"`
	Data   []byte `json:"data,omitempty"`
}

// NewRedisRelay builds the relay.
func NewRedisRelay(cfg RedisRelayConfig) (*RedisRelay, error) {
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, fmt.Errorf("realtime: the relay needs a redis url")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("realtime: relay redis url: %w", err)
	}
	channel := strings.TrimSpace(cfg.Channel)
	if channel == "" {
		channel = DefaultRelayChannel
	}
	origin := strings.TrimSpace(cfg.Origin)
	if origin == "" {
		origin = uuid.NewString()
	}
	return &RedisRelay{client: redis.NewClient(opts), channel: channel, origin: origin}, nil
}

// Publish sends a broadcast to the other replicas.
func (r *RedisRelay) Publish(ctx context.Context, msg Message) error {
	payload, err := json.Marshal(relayEnvelope{
		Origin: r.origin,
		Topic:  msg.Topic,
		Event:  msg.Event,
		ID:     msg.ID,
		Data:   msg.Data,
	})
	if err != nil {
		return fmt.Errorf("realtime: encode relayed message: %w", err)
	}
	if err := r.client.Publish(ctx, r.channel, payload).Err(); err != nil {
		return fmt.Errorf("realtime: relay publish: %w", err)
	}
	return nil
}

// Subscribe delivers what other replicas broadcast, until ctx is done.
//
// Messages this replica published are skipped by origin: the hub already
// delivered them locally before publishing, and delivering them again would
// show every client every message twice.
func (r *RedisRelay) Subscribe(ctx context.Context, deliver func(Message)) error {
	sub := r.client.Subscribe(ctx, r.channel)
	defer func() { _ = sub.Close() }()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case raw, ok := <-ch:
			if !ok {
				return nil
			}
			var envelope relayEnvelope
			if err := json.Unmarshal([]byte(raw.Payload), &envelope); err != nil {
				continue
			}
			if envelope.Origin == r.origin {
				continue
			}
			deliver(Message{
				Topic: envelope.Topic,
				Event: envelope.Event,
				ID:    envelope.ID,
				Data:  envelope.Data,
			})
		}
	}
}

// Close releases the Redis client.
func (r *RedisRelay) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}
