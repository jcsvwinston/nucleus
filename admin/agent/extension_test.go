package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/nucleus/admin/agent/internal/testserver"
)

// TestExtension_NoEndpoints_NoOp verifies the fail-open path: with no
// admin endpoints configured, Attach must succeed and Shutdown must be
// a no-op.
func TestExtension_NoEndpoints_NoOp(t *testing.T) {
	ext := NewExtension(app.AdminAgentConfig{}, t.TempDir(), "v0.0.0-test")

	a := &app.App{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Observability: observability.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	if err := ext.Attach(a); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := ext.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestExtension_NilObservability_Errors verifies the wiring error.
func TestExtension_NilObservability_Errors(t *testing.T) {
	ext := NewExtension(app.AdminAgentConfig{
		Endpoints: []string{"http://127.0.0.1:1"},
	}, t.TempDir(), "v0.0.0-test")

	a := &app.App{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Observability: nil,
	}

	err := ext.Attach(a)
	if err == nil || !errors.Is(err, errors.New("admin agent extension: app.Observability is nil")) && !contains(err.Error(), "Observability is nil") {
		t.Fatalf("expected nil Observability error, got %v", err)
	}
}

// TestExtension_RequireConnection_FailsBootWhenNoEndpointReachable
// verifies the --require-admin behaviour: Attach must return an error
// when no admin endpoint can be reached within the deadline.
func TestExtension_RequireConnection_FailsBootWhenNoEndpointReachable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := observability.NewBus(logger)

	ext := NewExtension(app.AdminAgentConfig{
		Endpoints:                []string{"http://127.0.0.1:1"}, // refuses
		RequireConnection:        true,
		RequireConnectionTimeout: 200 * time.Millisecond,
	}, t.TempDir(), "v0.0.0-test")

	a := &app.App{
		Logger:        logger,
		Observability: bus,
	}

	if err := ext.Attach(a); err == nil {
		t.Fatal("expected error when require_connection is true and no endpoint reachable")
	}
}

// TestExtension_RequireConnection_PassesBootWhenServerReachable verifies
// the happy path: Attach returns nil quickly when the agent reaches the
// server within the deadline.
func TestExtension_RequireConnection_PassesBootWhenServerReachable(t *testing.T) {
	srv := testserver.Start()
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := observability.NewBus(logger)

	ext := NewExtension(app.AdminAgentConfig{
		Endpoints:                []string{srv.URL()},
		RequireConnection:        true,
		RequireConnectionTimeout: 2 * time.Second,
	}, t.TempDir(), "v0.0.0-test")

	a := &app.App{
		Logger:        logger,
		Observability: bus,
	}

	if err := ext.Attach(a); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer func() {
		_ = ext.Shutdown(context.Background())
	}()

	if _, err := srv.WaitForRegistration(2 * time.Second); err != nil {
		t.Fatalf("server did not see registration: %v", err)
	}
}

// TestExtension_StartsAgent_AndShutsDown is a small integration test for
// the extension wrapper around an actual fake admin server.
func TestExtension_StartsAgent_AndShutsDown(t *testing.T) {
	srv := testserver.Start()
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := observability.NewBus(logger)

	ext := NewExtension(app.AdminAgentConfig{
		Endpoints:         []string{srv.URL()},
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      500 * time.Millisecond,
	}, t.TempDir(), "v0.0.0-test")

	a := &app.App{
		Logger:        logger,
		Observability: bus,
	}

	if err := ext.Attach(a); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if _, err := srv.WaitForRegistration(2 * time.Second); err != nil {
		t.Fatalf("WaitForRegistration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ext.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
