// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package testredis_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/jcsvwinston/nucleus/internal/testredis"
)

// The server answers Redis, and the inspection methods answer what the
// in-process miniredis answered: a value, a TTL, existence, the key list, and
// a clock that FastForward moves.
func TestServerAnswersLikeMiniredis(t *testing.T) {
	srv := testredis.Run(t)
	c := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	if err := c.Set(ctx, "b", "2", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, "a", "1", 10*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	if v, err := srv.Get("a"); err != nil || v != "1" {
		t.Fatalf("Get(a) = %q, %v", v, err)
	}
	if _, err := srv.Get("missing"); !errors.Is(err, testredis.ErrKeyNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrKeyNotFound", err)
	}
	if ttl := srv.TTL("a"); ttl <= 0 || ttl > 10*time.Second {
		t.Fatalf("TTL(a) = %v", ttl)
	}
	if ttl := srv.TTL("b"); ttl != 0 {
		t.Fatalf("TTL(b) without expiry = %v, want 0", ttl)
	}
	if got := srv.Keys(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Keys() = %v", got)
	}
	srv.FastForward(11 * time.Second)
	if srv.Exists("a") {
		t.Fatal("a outlived its TTL after FastForward")
	}
	if !srv.Exists("b") {
		t.Fatal("b has no TTL and must survive FastForward")
	}
}

// Each Run is its own server.
func TestEachRunIsItsOwnServer(t *testing.T) {
	a, b := testredis.Run(t), testredis.Run(t)
	if a.Addr() == b.Addr() {
		t.Fatalf("two servers on one address %s", a.Addr())
	}
	ca := goredis.NewClient(&goredis.Options{Addr: a.Addr()})
	t.Cleanup(func() { _ = ca.Close() })
	if err := ca.Set(context.Background(), "k", "v", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if b.Exists("k") {
		t.Fatal("a key written to one server is visible in the other")
	}
}

// Close takes the server away: a client that could reach it cannot.
func TestCloseStopsTheServer(t *testing.T) {
	srv := testredis.Run(t)
	c := goredis.NewClient(&goredis.Options{Addr: srv.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping before Close: %v", err)
	}
	srv.Close()
	srv.Close() // idempotent, and the test's cleanup calls it again
	if err := c.Ping(context.Background()).Err(); err == nil {
		t.Fatal("the server still answers after Close")
	}
}

// The point of the package: the framework's go.mod does not list miniredis.
// A test that imports it again would put it, and the Lua interpreter it
// embeds, back into every application's build graph.
func TestTheFrameworkDoesNotRequireMiniredis(t *testing.T) {
	cmd := exec.Command("go", "list", "-m", "all")
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -m all: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "github.com/alicebob/miniredis") || strings.HasPrefix(line, "github.com/yuin/gopher-lua") {
			t.Fatalf("the framework's module graph holds %q.\n\n"+
				"A _test.go file imports miniredis again. Start the server with\n"+
				"testredis.Run(t) instead: it runs miniredis in a process of its own, built\n"+
				"from internal/testdeps, so the framework's go.mod does not list it (NU-106).", line)
		}
	}
}
