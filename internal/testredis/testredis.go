// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package testredis gives the framework's tests a Redis server: miniredis,
// in a process of its own.
//
// The tests that need Redis used to start miniredis in-process, and a
// module's go.mod lists every module its own tests import — so miniredis and
// the Lua interpreter it embeds were in the build graph of every application
// that requires the framework (NU-106, A12 N1). The server now lives in the
// internal/testdeps module, whose go.mod is never published. The first Run
// in a test process builds it there (the go build cache makes that cheap
// after the first time); every Run starts a process on a free port that
// stops with the test.
//
// Server answers what the tests asked of *miniredis.Miniredis — its address,
// a key's value and TTL, the key list, and FastForward — over the wire.
// Nothing outside a _test.go file imports this package.
package testredis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Server is one miniredis process, started by Run and stopped when the test
// that started it ends.
type Server struct {
	addr   string
	client *goredis.Client
	stop   func()
}

// Run starts a server for the test and registers its shutdown. It fails the
// test when the server cannot be built or started: a skip would let every
// Redis test pass by not running.
func Run(t testing.TB) *Server {
	t.Helper()
	bin, err := binary()
	if err != nil {
		t.Fatalf("testredis: %v", err)
	}

	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("testredis: %v", err)
	}
	first := &firstLine{line: make(chan string, 1)}
	cmd.Stdout = first
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("testredis: start %s: %v", bin, err)
	}

	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	// Closing standard input is the server's signal to stop; a server that
	// does not within five seconds is killed.
	stop := func() {
		_ = stdin.Close()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	}

	var s Server
	select {
	case line := <-first.line:
		s.addr = strings.TrimSpace(strings.TrimPrefix(line, "ADDR "))
	case <-exited:
	case <-time.After(30 * time.Second):
	}
	if s.addr == "" {
		stop()
		t.Fatalf("testredis: the server did not report its address\n%s", stderr.String())
	}

	s.client = goredis.NewClient(&goredis.Options{Addr: s.addr})
	var once sync.Once
	s.stop = func() {
		once.Do(func() {
			_ = s.client.Close()
			stop()
		})
	}
	t.Cleanup(s.stop)
	return &s
}

// Close stops the server before the test ends, the way a test takes Redis
// away from the code under test. The test's cleanup stops it otherwise.
func (s *Server) Close() { s.stop() }

// firstLine hands the first line the server writes to Run and discards the
// rest.
type firstLine struct {
	mu   sync.Mutex
	buf  []byte
	sent bool
	line chan string
}

func (w *firstLine) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sent {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	if i := bytes.IndexByte(w.buf, '\n'); i >= 0 {
		w.line <- string(w.buf[:i])
		w.sent, w.buf = true, nil
	}
	return len(p), nil
}

// Addr is the server's host:port.
func (s *Server) Addr() string { return s.addr }

// ErrKeyNotFound is what Get returns for a key that does not exist.
var ErrKeyNotFound = errors.New("testredis: key not found")

// Get returns the string value of key in database 0.
func (s *Server) Get(key string) (string, error) {
	v, err := s.client.Get(context.Background(), key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", ErrKeyNotFound
	}
	return v, err
}

// TTL returns key's remaining time to live in database 0, or 0 when the key
// has none or does not exist — what miniredis's TTL answered.
func (s *Server) TTL(key string) time.Duration {
	d, err := s.client.PTTL(context.Background(), key).Result()
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// Exists reports whether key exists in database 0.
func (s *Server) Exists(key string) bool {
	n, err := s.client.Exists(context.Background(), key).Result()
	return err == nil && n == 1
}

// Keys returns every key in database 0, sorted.
func (s *Server) Keys() []string {
	keys, _ := s.client.Keys(context.Background(), "*").Result()
	slices.Sort(keys)
	return keys
}

// FastForward advances the server's clock, expiring keys whose TTL runs out.
func (s *Server) FastForward(d time.Duration) {
	if err := s.client.Do(context.Background(), "FASTFORWARD", d.Milliseconds()).Err(); err != nil {
		panic(fmt.Sprintf("testredis: FASTFORWARD: %v", err))
	}
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// binary builds cmd/miniredis from the internal/testdeps module once per
// process, into a path keyed by the sources, so concurrent test binaries and
// later runs reuse one file.
func binary() (string, error) {
	buildOnce.Do(func() { builtBin, buildErr = build() })
	return builtBin, buildErr
}

func build() (string, error) {
	root, err := frameworkRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "internal", "testdeps")
	h := sha256.New()
	for _, f := range []string{"go.mod", "go.sum", filepath.Join("cmd", "miniredis", "main.go")} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", fmt.Errorf("the miniredis server's sources are not in this tree (%v); "+
				"the framework's Redis tests run from a checkout of the repository", err)
		}
		h.Write(b)
	}
	fmt.Fprint(h, runtime.GOOS, runtime.GOARCH, runtime.Version())
	key := hex.EncodeToString(h.Sum(nil))[:16]

	name := "miniredis"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(os.TempDir(), "nucleus-testredis", key, name)
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	// Build beside the target and rename onto it: a test binary running in
	// parallel never executes a half-written file.
	tmp, err := os.CreateTemp(filepath.Dir(out), name+".*")
	if err != nil {
		return "", err
	}
	_ = tmp.Close()
	defer os.Remove(tmp.Name())

	// `go test` puts its own toolchain first on PATH for the test process.
	cmd := exec.Command("go", "build", "-o", tmp.Name(), "./cmd/miniredis")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/miniredis in %s: %v\n%s", dir, err, b)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return "", err
	}
	return out, nil
}

// frameworkRoot walks up from the working directory — the directory of the
// package under test — to the framework's go.mod.
func frameworkRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && modulePath(string(b)) == "github.com/jcsvwinston/nucleus" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no github.com/jcsvwinston/nucleus go.mod above the working directory")
		}
		dir = parent
	}
}

func modulePath(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}
