package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultSessionRedisPrefix = "goframe:sessions:"

// RedisSessionStore persists sessions in Redis with key TTL.
type RedisSessionStore struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewRedisSessionStore creates a Redis-backed session store from an existing client.
func NewRedisSessionStore(client redis.UniversalClient, keyPrefix string) (*RedisSessionStore, error) {
	if client == nil {
		return nil, fmt.Errorf("new redis session store: nil redis client")
	}
	if strings.TrimSpace(keyPrefix) == "" {
		keyPrefix = defaultSessionRedisPrefix
	}

	return &RedisSessionStore{
		client:    client,
		keyPrefix: keyPrefix,
	}, nil
}

// NewRedisSessionStoreFromURL creates a redis.Client and a Redis session store.
func NewRedisSessionStoreFromURL(rawURL, keyPrefix string) (*RedisSessionStore, *redis.Client, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("new redis session store: parse redis url: %w", err)
	}

	client := redis.NewClient(options)
	store, err := NewRedisSessionStore(client, keyPrefix)
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}

	return store, client, nil
}

// Delete removes the session token from the store.
func (s *RedisSessionStore) Delete(token string) error {
	return s.DeleteCtx(context.Background(), token)
}

// Find retrieves the session payload for token.
func (s *RedisSessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

// Commit stores the session payload for token with absolute expiry.
func (s *RedisSessionStore) Commit(token string, b []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, b, expiry)
}

// DeleteCtx removes the session token from Redis.
func (s *RedisSessionStore) DeleteCtx(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.client.Del(ctx, s.key(token)).Err(); err != nil {
		return fmt.Errorf("redis session delete: %w", err)
	}
	return nil
}

// FindCtx retrieves the session payload from Redis.
func (s *RedisSessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	if token == "" {
		return nil, false, nil
	}
	value, err := s.client.Get(ctx, s.key(token)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis session find: %w", err)
	}

	result := make([]byte, len(value))
	copy(result, value)
	return result, true, nil
}

// CommitCtx stores the session payload in Redis with a TTL derived from expiry.
func (s *RedisSessionStore) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	if token == "" {
		return fmt.Errorf("redis session commit: empty token")
	}
	if expiry.IsZero() {
		return fmt.Errorf("redis session commit: zero expiry")
	}

	ttl := time.Until(expiry)
	if ttl <= 0 {
		return s.DeleteCtx(ctx, token)
	}

	if err := s.client.Set(ctx, s.key(token), b, ttl).Err(); err != nil {
		return fmt.Errorf("redis session commit: %w", err)
	}
	return nil
}

func (s *RedisSessionStore) key(token string) string {
	return s.keyPrefix + token
}
