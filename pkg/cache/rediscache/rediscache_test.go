// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package rediscache

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/testredis"
	"github.com/jcsvwinston/nucleus/pkg/cache"
)

// The unit tests run against miniredis, a server that speaks the protocol,
// started in a process of its own by internal/testredis;
// TestRedisLive_SharedBetweenInstances runs the same contract against a real
// server when NUCLEUS_CACHE_REDIS_URL names one (the "Module Jobs (real
// Redis)" lane sets it).

func openMini(t *testing.T, prefix string) (*Cache, *testredis.Server) {
	t.Helper()
	srv := testredis.Run(t)
	c, err := Open(t.Context(), "redis://"+srv.Addr()+"/0", Options{Prefix: prefix})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, srv
}

func TestCache_SetGetDelete(t *testing.T) {
	c, srv := openMini(t, "")
	ctx := t.Context()

	if _, ok, err := c.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) = ok=%v err=%v, want absent", ok, err)
	}
	if err := c.Set(ctx, "k", []byte("v1"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok, err := c.Get(ctx, "k"); err != nil || !ok || !bytes.Equal(got, []byte("v1")) {
		t.Fatalf("Get(k) = %q ok=%v err=%v", got, ok, err)
	}
	// The key is stored under the default namespace, with Redis's own TTL.
	if raw, err := srv.Get(cache.DefaultKeyPrefix + "k"); err != nil || raw != "v1" {
		t.Fatalf("the server holds %q (err=%v) under %sk", raw, err, cache.DefaultKeyPrefix)
	}
	if ttl := srv.TTL(cache.DefaultKeyPrefix + "k"); ttl <= 0 || ttl > time.Minute {
		t.Fatalf("the server's TTL for the key is %v, want (0, 1m]", ttl)
	}
	if err := c.Set(ctx, "k", []byte("v2"), time.Minute); err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if got, _, _ := c.Get(ctx, "k"); !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Get after replace = %q, want v2", got)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("Get after Delete still present")
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
	// An empty value is a value, not an absence.
	if err := c.Set(ctx, "empty", nil, time.Minute); err != nil {
		t.Fatalf("Set(nil): %v", err)
	}
	if got, ok, err := c.Get(ctx, "empty"); err != nil || !ok || len(got) != 0 {
		t.Fatalf("Get(empty) = %q ok=%v err=%v, want a present empty value", got, ok, err)
	}
}

func TestCache_ExpiryIsTheServers(t *testing.T) {
	c, srv := openMini(t, "")
	ctx := t.Context()
	if err := c.Set(ctx, "k", []byte("v"), 2*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}
	srv.FastForward(3 * time.Second)
	if _, ok, err := c.Get(ctx, "k"); err != nil || ok {
		t.Fatalf("Get after expiry = ok=%v err=%v, want absent", ok, err)
	}
}

func TestCache_RefusesWhatTheContractRefuses(t *testing.T) {
	c, _ := openMini(t, "")
	ctx := t.Context()
	if _, _, err := c.Get(ctx, " "); !errors.Is(err, cache.ErrEmptyKey) {
		t.Errorf("Get(empty key) = %v", err)
	}
	if err := c.Set(ctx, "", []byte("v"), time.Minute); !errors.Is(err, cache.ErrEmptyKey) {
		t.Errorf("Set(empty key) = %v", err)
	}
	if err := c.Delete(ctx, ""); !errors.Is(err, cache.ErrEmptyKey) {
		t.Errorf("Delete(empty key) = %v", err)
	}
	if err := c.Set(ctx, "k", []byte("v"), 0); !errors.Is(err, cache.ErrNonPositiveTTL) {
		t.Errorf("Set(ttl 0) = %v", err)
	}
}

// Two caches over one server are two instances of an application: what one
// writes the other reads, and a prefix keeps a neighbour's keys apart.
func TestCache_SharedBetweenInstancesAndApartByPrefix(t *testing.T) {
	srv := testredis.Run(t)
	url := "redis://" + srv.Addr() + "/0"
	open := func(prefix string) *Cache {
		c, err := Open(t.Context(), url, Options{Prefix: prefix})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	a, b, other := open("app:"), open("app:"), open("other:")
	ctx := t.Context()
	if err := a.Set(ctx, "greeting", []byte("hello"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok, err := b.Get(ctx, "greeting"); err != nil || !ok || string(got) != "hello" {
		t.Fatalf("the second instance reads %q ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := other.Get(ctx, "greeting"); ok {
		t.Fatal("a cache with another prefix read the key")
	}
	if err := b.Delete(ctx, "greeting"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := a.Get(ctx, "greeting"); ok {
		t.Fatal("a delete on one instance did not reach the other")
	}
}

func TestOpen_RefusesAServerThatDoesNotAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err = Open(ctx, "redis://:s3cret@"+addr+"/0", Options{})
	if err == nil || !strings.Contains(err.Error(), "does not answer") || !strings.Contains(err.Error(), addr) {
		t.Fatalf("Open(unreachable) = %v, want a refusal naming %s", err, addr)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("the refusal repeats the password: %v", err)
	}
	if _, err := Open(t.Context(), "http://"+addr, Options{}); err == nil {
		t.Fatal("Open(http://) succeeded")
	}
	if _, err := Open(t.Context(), "redis://:s3cret@[bad", Options{}); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("Open(malformed URL) = %v, want a refusal that does not repeat the password", err)
	}
}

// Importing the package is what makes cache.provider: redis selectable, and
// the framework's Open hands it the address and the prefix.
func TestRegistered_OpenSelectsItByName(t *testing.T) {
	found := false
	for _, name := range cache.RegisteredProviders() {
		found = found || name == ProviderName
	}
	if !found {
		t.Fatalf("redis is not registered: %v", cache.RegisteredProviders())
	}
	srv := testredis.Run(t)
	c, err := cache.Open(t.Context(), cache.Config{Provider: "redis", URL: "redis://" + srv.Addr() + "/0", Prefix: "p:"})
	if err != nil {
		t.Fatalf("cache.Open(redis): %v", err)
	}
	rc, ok := c.(*Cache)
	if !ok {
		t.Fatalf("cache.Open(redis) = %T", c)
	}
	defer func() { _ = rc.Close() }()
	if err := rc.Set(t.Context(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if !srv.Exists("p:k") {
		t.Fatalf("the key is not under the configured prefix: %v", srv.Keys())
	}
	if _, err := cache.Open(t.Context(), cache.Config{Provider: "redis"}); err == nil || !strings.Contains(err.Error(), "cache.redis_url") {
		t.Fatalf("cache.Open(redis) without an address = %v, want a refusal naming cache.redis_url", err)
	}
}

func TestRedisLive_SharedBetweenInstances(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("NUCLEUS_CACHE_REDIS_URL"))
	if url == "" {
		t.Skip("NUCLEUS_CACHE_REDIS_URL is not set; the Module Jobs (real Redis) lane runs this against a real server")
	}
	prefix := "nucleus-live-" + time.Now().Format("150405.000000") + ":"
	open := func() *Cache {
		c, err := Open(t.Context(), url, Options{Prefix: prefix})
		if err != nil {
			t.Fatalf("Open(%s): %v", url, err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	a, b := open(), open()
	ctx := t.Context()
	if err := a.Set(ctx, "k", []byte("from a"), 2*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok, err := b.Get(ctx, "k"); err != nil || !ok || string(got) != "from a" {
		t.Fatalf("the second instance reads %q ok=%v err=%v", got, ok, err)
	}
	if err := b.Set(ctx, "short", []byte("v"), 500*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, ok, err := a.Get(ctx, "short"); err != nil || ok {
		t.Fatalf("an expired key is still read: ok=%v err=%v", ok, err)
	}
	if err := a.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := b.Get(ctx, "k"); ok {
		t.Fatal("a delete on one instance did not reach the other")
	}
	if err := a.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
