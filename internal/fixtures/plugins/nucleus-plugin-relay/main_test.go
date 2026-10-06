// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/cli"
	"github.com/jcsvwinston/nucleus/internal/testsqlite"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

func init() { testsqlite.Register() }

// The example plugin is tested the way an application runs it: built into
// an executable, put on PATH, and reached through the real runtime — an
// application whose outbox has a bridge of type plugin — and through
// `nucleus plugin test --execute`. Nothing here calls publish or deliver
// directly.

// buildPlugin builds this package into dir/nucleus-plugin-relay and puts
// dir first on PATH, with RELAY_QUEUE_DIR pointing at a fresh queue it
// returns.
func buildPlugin(t *testing.T) (bin, queue string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the PATH lookup below assumes a POSIX executable name")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, plugins.GenericBinaryPrefix+"relay")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build the example plugin: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	queue = filepath.Join(t.TempDir(), "queue")
	t.Setenv("RELAY_QUEUE_DIR", queue)
	return bin, queue
}

// startApp starts an application whose outbox routes every message to the
// relay through a bridge of type plugin, listed in its allowlist.
func startApp(t *testing.T, bridge map[string]interface{}) *nucleustest.Server {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.LogLevel = "error"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.Storage.Provider = "memory"
	cfg.Outbox = app.OutboxConfig{
		Enabled:       true,
		TableName:     "nucleus_outbox",
		LeaseDuration: 30 * time.Second,
		MaxRetries:    5,
		RetryBackoff:  time.Hour, // a retried message stays retried while the test reads it
		Bridges:       []app.BridgeConfig{{Name: "relay", Type: "plugin", Config: bridge}},
	}
	cfg.Plugins = app.PluginsConfig{Allowed: []app.PluginAllowance{{
		Provider: "relay", Capabilities: []string{plugins.CapabilityQueuePublish, plugins.CapabilityWebhookDeliver},
	}}}
	return nucleustest.StartApp(t, nucleus.App{Config: cfg, Options: []app.Option{app.WithOpenAuthz()}})
}

// waitFor polls the outbox row until it reaches status.
func waitFor(t *testing.T, db *sql.DB, id string, status outbox.Status) (attempts int, lastError string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		var last sql.NullString
		if err := db.QueryRow("SELECT status, attempts, last_error FROM nucleus_outbox WHERE id = ?", id).Scan(&got, &attempts, &last); err == nil && got == string(status) {
			return attempts, last.String
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("message %s is %q, never %q", id, got, status)
	return 0, ""
}

func queuedFiles(t *testing.T, queue, topic string) []queued {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(queue, topic, "new"))
	if err != nil {
		t.Fatalf("read the queue: %v", err)
	}
	var out []queued
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(queue, topic, "new", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var q queued
		if err := json.Unmarshal(raw, &q); err != nil {
			t.Fatalf("%s is not a queued message: %v", e.Name(), err)
		}
		out = append(out, q)
	}
	if left, _ := os.ReadDir(filepath.Join(queue, topic, "tmp")); len(left) != 0 {
		t.Errorf("tmp/ still holds %d file(s): a publish must end in new/", len(left))
	}
	return out
}

func TestAdvertisesTheOutboxCapabilities(t *testing.T) {
	bin, _ := buildPlugin(t)
	caps, err := plugins.ProbeCapabilities(context.Background(), bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(caps, ",") != "queue.publish,webhook.deliver" {
		t.Fatalf("capabilities = %v", caps)
	}
}

// TestPublishesWhatTheOutboxSends is the path an application takes: a
// message enqueued in the application's outbox is handed to the plugin by
// the bridge, lands in the directory queue, and the row is delivered.
func TestPublishesWhatTheOutboxSends(t *testing.T) {
	_, queue := buildPlugin(t)
	srv := startApp(t, map[string]interface{}{"provider": "relay", "capability": "queue.publish", "pattern": "orders.*"})
	rt := srv.Runtime()
	msg, err := rt.Outbox().Enqueue(context.Background(), outbox.Entry{Topic: "orders.created", Payload: map[string]any{"order_id": 42}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, rt.DB(), msg.ID, outbox.StatusDelivered)

	got := queuedFiles(t, queue, "orders.created")
	if len(got) != 1 {
		t.Fatalf("%d message(s) in the queue, want 1", len(got))
	}
	q := got[0]
	var body bytes.Buffer
	_ = json.Compact(&body, q.Body)
	if q.Topic != "orders.created" || q.Key != msg.ID || body.String() != `{"order_id":42}` || q.Metadata["outbox_message_id"] != msg.ID {
		t.Fatalf("queued %+v (body %s)", q, q.Body)
	}
}

// TestRedeliveryReplacesItsOwnMessage: delivery is at least once, and the
// key the outbox sends is the message id — so the same message delivered
// twice is one file in the queue.
func TestRedeliveryReplacesItsOwnMessage(t *testing.T) {
	bin, queue := buildPlugin(t)
	b, err := outbox.NewPluginBridge(outbox.PluginConfig{Name: "relay", Provider: "relay", Capability: plugins.CapabilityQueuePublish, Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	msg := outbox.Message{ID: "0b7f6c1e-redelivered", Topic: "orders.created", Payload: []byte(`{"order_id":1}`), Attempts: 1}
	for attempt := 1; attempt <= 2; attempt++ {
		msg.Attempts = attempt
		if err := b.Send(context.Background(), msg); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if got := queuedFiles(t, queue, "orders.created"); len(got) != 1 || got[0].Metadata["outbox_attempt"] != "2" {
		t.Fatalf("queue after two deliveries of one message: %+v", got)
	}
}

// webhookEndpoint answers status and records what it received.
func webhookEndpoint(t *testing.T, status int) (*httptest.Server, chan *http.Request, chan []byte) {
	t.Helper()
	reqs, bodies := make(chan *http.Request, 8), make(chan []byte, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs <- r.Clone(context.Background())
		bodies <- body
		w.WriteHeader(status)
		_, _ = w.Write([]byte("endpoint says hi"))
	}))
	t.Cleanup(srv.Close)
	return srv, reqs, bodies
}

// TestDeliversSignedWebhooks: with capability webhook.deliver the plugin
// sends the request the outbox built — the webhook bridge's body and its
// signature — and the endpoint verifies it as it would a direct delivery.
func TestDeliversSignedWebhooks(t *testing.T) {
	buildPlugin(t)
	endpoint, reqs, bodies := webhookEndpoint(t, http.StatusNoContent)
	srv := startApp(t, map[string]interface{}{
		"provider": "relay", "capability": "webhook.deliver",
		"url": endpoint.URL + "/hooks/orders", "secret": "relay-secret", "payload_encoding": "json",
		"headers": map[string]interface{}{"Authorization": "Bearer relay"},
	})
	rt := srv.Runtime()
	msg, err := rt.Outbox().Enqueue(context.Background(), outbox.Entry{Topic: "orders.created", Payload: map[string]any{"order_id": 7}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, rt.DB(), msg.ID, outbox.StatusDelivered)

	r, body := <-reqs, <-bodies
	if r.Method != http.MethodPost || r.URL.Path != "/hooks/orders" || r.Header.Get("Authorization") != "Bearer relay" {
		t.Errorf("request %s %s, Authorization %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
	}
	if got, want := r.Header.Get(outbox.WebhookSignatureHeader), nucleus.SignWebhookBody("relay-secret", body); got != want {
		t.Errorf("signature %q, want %q over the body received", got, want)
	}
	if enc := r.Header.Get(outbox.WebhookPayloadEncodingHeader); enc != outbox.PayloadEncodingJSON {
		t.Errorf("payload encoding %q", enc)
	}
	var delivery struct {
		ID      string          `json:"id"`
		Topic   string          `json:"topic"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &delivery); err != nil || delivery.ID != msg.ID || delivery.Topic != "orders.created" || string(delivery.Payload) != `{"order_id":7}` {
		t.Errorf("body %s (%v)", body, err)
	}
}

// TestEndpointAnswersDecideRetryOrDeadLetter: a 503 is transient — the
// message goes back to pending with an attempt spent; a 410 is a rejection
// — the message goes to the dead letter on that attempt.
func TestEndpointAnswersDecideRetryOrDeadLetter(t *testing.T) {
	buildPlugin(t)
	for _, tc := range []struct {
		status   int
		want     outbox.Status
		inReason string
	}{
		{http.StatusServiceUnavailable, outbox.StatusPending, "exit=20"},
		{http.StatusGone, outbox.StatusFailed, "exit=30"},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			endpoint, _, _ := webhookEndpoint(t, tc.status)
			srv := startApp(t, map[string]interface{}{"provider": "relay", "capability": "webhook.deliver", "url": endpoint.URL, "secret": "s"})
			rt := srv.Runtime()
			msg, err := rt.Outbox().Enqueue(context.Background(), outbox.Entry{Topic: "orders.created", Payload: map[string]any{"order_id": 1}})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(20 * time.Second)
			var attempts int
			var status string
			var last sql.NullString
			for time.Now().Before(deadline) {
				_ = rt.DB().QueryRow("SELECT status, attempts, last_error FROM nucleus_outbox WHERE id = ?", msg.ID).Scan(&status, &attempts, &last)
				if attempts == 1 && status == string(tc.want) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if status != string(tc.want) || attempts != 1 || !strings.Contains(last.String, tc.inReason) || !strings.Contains(last.String, "answered "+itoa(tc.status)) {
				t.Fatalf("after one attempt the message is %q (%d attempt(s)): %q", status, attempts, last.String)
			}
		})
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestPassesPluginTestExecute: the CLI's contract check sends the plugin a
// real envelope per capability — the sample for queue.publish, and for
// webhook.deliver a payload addressed to a test endpoint.
func TestPassesPluginTestExecute(t *testing.T) {
	_, queue := buildPlugin(t)
	t.Chdir(t.TempDir()) // no nucleus.yml: the defaults, no allowlist
	run := func(args ...string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := cli.Run(append([]string{"plugin", "test", "--provider", "relay", "--execute", "--timeout", "30s", "--json"}, args...), strings.NewReader(""), &out, &errb)
		var report struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(out.Bytes(), &report)
		if code != 0 || report.Status != "ok" {
			t.Fatalf("nucleus plugin test --execute %v exited %d:\n%s%s", args, code, out.String(), errb.String())
		}
	}
	run("--capability", plugins.CapabilityQueuePublish)
	if got := queuedFiles(t, queue, "nucleus.plugin_test"); len(got) != 1 {
		t.Fatalf("the sample message was not queued: %+v", got)
	}

	endpoint, _, bodies := webhookEndpoint(t, http.StatusOK)
	payload, _ := json.Marshal(plugins.WebhookDeliverPayload{URL: endpoint.URL, Method: http.MethodPost, Body: `{"hello":"relay"}`})
	file := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(file, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	run("--capability", plugins.CapabilityWebhookDeliver, "--payload", file)
	if body := <-bodies; string(body) != `{"hello":"relay"}` {
		t.Fatalf("the endpoint received %q", body)
	}
}

// TestRefusesWhatCannotNameAFile: a topic with a path separator is a
// validation failure, exit 10, and nothing is written.
func TestRefusesWhatCannotNameAFile(t *testing.T) {
	bin, queue := buildPlugin(t)
	req, err := plugins.NewRequestEnvelope("relay", plugins.CapabilityQueuePublish, 10*time.Second,
		plugins.QueuePublishPayload{Topic: "../escape", Key: "k", Body: json.RawMessage(`{}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugins.ExecuteRequest(context.Background(), bin, req, 10*time.Second)
	var execErr *plugins.ExecutionError
	if !errors.As(err, &execErr) || execErr.ExitCode != plugins.ExitCodeValidation || execErr.Retriable {
		t.Fatalf("want exit 10, not retriable; got %v", err)
	}
	if _, statErr := os.Stat(queue); statErr == nil {
		t.Fatalf("the refused message created %s", queue)
	}
}
