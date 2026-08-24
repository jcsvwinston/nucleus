// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package nucleustest boots a nucleus application inside the test process
// (DX-22). Before it existed, every E2E suite re-invented the same
// scaffolding — `go build`, exec.Command, /healthz polling, duplicated
// start helpers — because Start() blocks and exposes no programmatic
// shutdown. The kit reduces that to one call:
//
//	func TestMyAPI(t *testing.T) {
//		srv := nucleustest.Start(t, nucleus.New().
//			FromConfigFile("testdata/nucleus.yml").
//			Mount(notes.Module()))
//
//		resp, err := srv.Client().Get(srv.URL("/notes"))
//		...
//	}
//
// Start assigns a free loopback port, runs the full framework startup
// sequence (nucleus.RunContext) in a goroutine, waits for /healthz, and
// registers a t.Cleanup that shuts the application down gracefully and
// fails the test if the run returned an unexpected error. MintToken issues
// bearer tokens against the application's own jwt_secret for exercising
// protected routes.
package nucleustest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Server is a nucleus application running in-process for the duration of a
// test. Construct it with Start or StartApp; it stops automatically via
// t.Cleanup (or earlier, with an explicit Stop).
type Server struct {
	// BaseURL is the loopback origin the application listens on, e.g.
	// "http://127.0.0.1:49521".
	BaseURL string

	app    nucleus.App
	probe  *runtimeProbe
	tb     testing.TB
	cancel context.CancelFunc
	done   chan error
	client *http.Client

	stopped bool
}

// Start builds the application from the builder and boots it in-process.
// The builder's configured port is replaced with a free loopback port so
// parallel tests never collide. Fails the test on any build or boot error.
func Start(tb testing.TB, b *nucleus.AppBuilder) *Server {
	tb.Helper()
	a, err := b.Build()
	if err != nil {
		tb.Fatalf("nucleustest: build application: %v", err)
	}
	return StartApp(tb, a)
}

// StartApp boots an already-constructed nucleus.App in-process. The
// direct-struct counterpart of Start.
func StartApp(tb testing.TB, a nucleus.App) *Server {
	tb.Helper()

	if a.Config.Host == "" || a.Config.Host == "0.0.0.0" {
		a.Config.Host = "127.0.0.1"
	}
	port, err := freePort(a.Config.Host)
	if err != nil {
		tb.Fatalf("nucleustest: reserve port: %v", err)
	}
	a.Config.Port = port

	// The kit's runtime-capture module rides the same Mount surface every
	// module uses (see runtime.go). The modules map is copied so the
	// caller's App value is never mutated.
	if _, taken := a.Modules[probeModuleName]; taken {
		tb.Fatalf("nucleustest: module name %q is reserved for the kit's runtime probe", probeModuleName)
	}
	probe := &runtimeProbe{}
	modules := make(map[string]nucleus.ModuleSpec, len(a.Modules)+1)
	for name, spec := range a.Modules {
		modules[name] = spec
	}
	modules[probeModuleName] = probe.spec()
	a.Modules = modules

	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		BaseURL: fmt.Sprintf("http://%s:%d", a.Config.Host, port),
		app:     a,
		probe:   probe,
		tb:      tb,
		cancel:  cancel,
		done:    make(chan error, 1),
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	go func() { s.done <- nucleus.RunContext(ctx, a) }()

	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-s.done:
			tb.Fatalf("nucleustest: application exited during startup: %v", err)
			return nil
		default:
		}
		resp, err := s.client.Get(s.BaseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			tb.Fatalf("nucleustest: application did not become ready on %s within 15s", s.BaseURL)
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}

	tb.Cleanup(s.Stop)
	return s
}

// Stop shuts the application down gracefully and waits for the run loop to
// exit. Idempotent; registered automatically as a test cleanup. An
// unexpected run error fails the test.
func (s *Server) Stop() {
	if s.stopped {
		return
	}
	s.stopped = true
	s.cancel()
	select {
	case err := <-s.done:
		if err != nil {
			s.tb.Errorf("nucleustest: application exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		s.tb.Errorf("nucleustest: application did not shut down within 15s")
	}
}

// Client returns an HTTP client suitable for talking to the server.
func (s *Server) Client() *http.Client { return s.client }

// URL joins path onto the server's base URL.
func (s *Server) URL(path string) string {
	if path == "" {
		return s.BaseURL
	}
	return s.BaseURL + "/" + strings.TrimLeft(path, "/")
}

// MintToken issues a bearer token signed with the application's configured
// jwt_secret — the same material the framework's JWT middleware validates —
// so a test can exercise protected routes without standing up a login flow.
// Applications configured with jwt_keys (asymmetric keysets) should build
// their own auth.JWTManager via auth.NewJWTManagerFromKeys instead.
func (s *Server) MintToken(userID, username, role string) string {
	s.tb.Helper()
	secret := strings.TrimSpace(s.app.Config.JWTSecret)
	if secret == "" {
		s.tb.Fatalf("nucleustest: MintToken needs jwt_secret in the application config (jwt_keys keysets: use auth.NewJWTManagerFromKeys)")
	}
	expiry := s.app.Config.JWTExpiry
	if expiry <= 0 {
		expiry = time.Hour
	}
	manager := auth.NewJWTManager(secret, expiry, s.app.Config.JWTIssuer)
	token, err := manager.Generate(userID, username, role)
	if err != nil {
		s.tb.Fatalf("nucleustest: mint token: %v", err)
	}
	return token
}

// freePort reserves an ephemeral TCP port on host and releases it for the
// application to bind. The classic race (another process grabbing it in
// between) is possible but vanishingly rare on loopback in test runs.
func freePort(host string) (int, error) {
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
