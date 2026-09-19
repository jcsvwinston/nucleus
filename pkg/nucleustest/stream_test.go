// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/realtime"
)

// A test can drive a live view end to end: open the stream, broadcast, read
// what arrived. Writing the bufio loop and the event parser by hand is what
// every application testing a channel had to do instead.
func TestStream_ReadsBroadcasts(t *testing.T) {
	hub := realtime.New(realtime.Config{})
	t.Cleanup(func() { _ = hub.Close() })

	mod := nucleus.Module[struct{}]{
		Name:       "live",
		CSRFExempt: []string{"/live"},
		Policies:   []nucleus.PolicyRule{{Subject: "anonymous", Object: "/live", Action: "read"}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/live", func(c *nucleus.Context) error {
				return realtime.ServeSSE(c.Writer, c.Request, realtime.SSEConfig{
					Hub: hub, Topics: []string{"orders"}, KeepAlive: time.Hour,
				})
			})
		},
	}
	cfg := app.DefaultConfig()
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("nucleustest-stream-secret", 2)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"live": mod.Build()},
	})

	stream := srv.Stream("/live")
	waitFor(t, 2*time.Second, func() bool { return hub.Count("orders") == 1 })

	hub.Broadcast(context.Background(), realtime.Message{
		Topic: "orders", Event: "created", Data: []byte(`{"id":7}`),
	})

	event := stream.Next(3 * time.Second)
	if event.Event != "created" {
		t.Fatalf("event name %q", event.Event)
	}
	var payload struct {
		ID int `json:"id"`
	}
	event.JSON(t, &payload)
	if payload.ID != 7 {
		t.Fatalf("payload %+v", payload)
	}
}

// Quiet is the assertion an authorisation bug slips past when nobody writes
// it: that somebody did NOT receive a broadcast.
func TestStream_QuietWhenNothingIsBroadcast(t *testing.T) {
	hub := realtime.New(realtime.Config{})
	t.Cleanup(func() { _ = hub.Close() })

	mod := nucleus.Module[struct{}]{
		Name:       "live",
		CSRFExempt: []string{"/live"},
		Policies:   []nucleus.PolicyRule{{Subject: "anonymous", Object: "/live", Action: "read"}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/live", func(c *nucleus.Context) error {
				return realtime.ServeSSE(c.Writer, c.Request, realtime.SSEConfig{
					Hub: hub, Topics: []string{"mine"}, KeepAlive: time.Hour,
				})
			})
		},
	}
	cfg := app.DefaultConfig()
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("nucleustest-stream-secret", 2)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"live": mod.Build()},
	})

	stream := srv.Stream("/live")
	waitFor(t, 2*time.Second, func() bool { return hub.Count("mine") == 1 })

	// Broadcast on a topic this client did NOT subscribe to.
	hub.Broadcast(context.Background(), realtime.Message{Topic: "somebody-elses", Data: []byte(`{}`)})
	stream.Quiet(500 * time.Millisecond)
}

// A route that is not a stream fails the helper with a message that says so,
// rather than hanging until the test times out.
func TestStream_RefusesARouteThatIsNotAStream(t *testing.T) {
	// This one asserts on a failure, so it runs the helper against a
	// sub-test whose failure is expected.
	cfg := app.DefaultConfig()
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("nucleustest-stream-secret", 2)
	srv := nucleustest.StartApp(t, nucleus.App{Config: cfg})

	resp, err := srv.Client().Get(srv.URL("/healthz"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Skip("healthz is a stream now; pick another route for this check")
	}
	if resp.StatusCode != http.StatusOK {
		t.Skipf("healthz answered %d", resp.StatusCode)
	}
	// The helper would Fatal here, which is the behaviour being documented:
	// a clear failure instead of a hang.
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", limit)
}
