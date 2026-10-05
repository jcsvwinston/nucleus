// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// scriptPlugin puts `nucleus-plugin-<provider>` on PATH: a shell script
// that advertises caps, appends every request envelope it is sent to
// capture, and exits with exit (answering ok only when exit is 0).
func scriptPlugin(t *testing.T, provider string, exit int, caps ...string) (capture string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the plugin below is a POSIX shell script")
	}
	dir := t.TempDir()
	capture = filepath.Join(dir, "capture.jsonl")
	quoted := make([]string, len(caps))
	for i, c := range caps {
		quoted[i] = `"` + c + `"`
	}
	answer := `{"version":"v1","ok":true,"output":{"accepted":true}}`
	if exit != 0 {
		answer = `{"version":"v1","ok":false,"error":{"code":"REFUSED","message":"no"}}`
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"capabilities\" ]; then echo '{\"capabilities\":[" + strings.Join(quoted, ",") + "]}'; exit 0; fi\n" +
		"cat >> \"" + capture + "\"\necho >> \"" + capture + "\"\n" +
		"echo '" + answer + "'\nexit " + itoa(exit) + "\n"
	path := filepath.Join(dir, plugins.GenericBinaryPrefix+provider)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func captured(t *testing.T, capture string) []plugins.RequestEnvelope {
	t.Helper()
	raw, _ := os.ReadFile(capture)
	var out []plugins.RequestEnvelope
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var env plugins.RequestEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("the plugin received something that is not an envelope: %q", line)
		}
		out = append(out, env)
	}
	return out
}

func pluginBridgeConfig(config map[string]interface{}) *Config {
	cfg := testAppConfig()
	cfg.Outbox = OutboxConfig{
		Enabled:       true,
		TableName:     "nucleus_outbox",
		LeaseDuration: 30 * time.Second,
		MaxRetries:    5,
		RetryBackoff:  time.Hour, // a retried message stays retried for the test
		Bridges:       []BridgeConfig{{Name: "events", Type: "plugin", Config: config}},
	}
	return cfg
}

// waitForStatus polls the outbox until the message is in status.
func waitForStatus(t *testing.T, a *App, id string, status outbox.Status) (attempts int, lastError string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var got string
		var last *string
		sqlDB, err := a.DB.SqlDB()
		if err != nil {
			t.Fatal(err)
		}
		err = sqlDB.QueryRowContext(context.Background(),
			"SELECT status, attempts, last_error FROM nucleus_outbox WHERE id = ?", id).Scan(&got, &attempts, &last)
		if err == nil && got == string(status) {
			if last != nil {
				lastError = *last
			}
			return attempts, lastError
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("message %s never reached %s", id, status)
	return 0, ""
}

// An outbox bridge of type plugin hands each message to the external plugin
// through the runtime, and the message is delivered.
func TestAttachOutbox_PluginBridgeDelivers(t *testing.T) {
	capture := scriptPlugin(t, "apptestqueue", 0, plugins.CapabilityQueuePublish)
	cfg := pluginBridgeConfig(map[string]interface{}{
		"provider":   "apptestqueue",
		"capability": "queue.publish",
		"pattern":    "orders.*",
		"timeout":    "5s",
		"topic":      "shop-events",
	})
	cfg.Plugins = PluginsConfig{Allowed: []PluginAllowance{{Provider: "apptestqueue", Capabilities: []string{plugins.CapabilityQueuePublish}}}}

	a, err := New(cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()
	if err := a.StartOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg, err := a.Outbox.Enqueue(context.Background(), outbox.Entry{Topic: "orders.created", Payload: map[string]any{"order_id": 42}})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, a, msg.ID, outbox.StatusDelivered)

	got := captured(t, capture)
	if len(got) != 1 {
		t.Fatalf("the plugin received %d envelope(s), want 1", len(got))
	}
	var payload plugins.QueuePublishPayload
	if err := json.Unmarshal(got[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if got[0].Capability != plugins.CapabilityQueuePublish || payload.Topic != "shop-events" || payload.Key != msg.ID ||
		string(payload.Body) != `{"order_id":42}` || got[0].Metadata["outbox_message_id"] != msg.ID || got[0].TimeoutMS != 5000 {
		t.Fatalf("envelope %+v, payload %+v (%s)", got[0], payload, payload.Body)
	}
}

// A refusal the plugin does not mark retriable sends the message to the
// dead letter on the attempt that got it.
func TestAttachOutbox_PluginBridgeRejectionIsTheDeadLetter(t *testing.T) {
	capture := scriptPlugin(t, "apptestreject", plugins.ExitCodeRejected, plugins.CapabilityWebhookDeliver)
	cfg := pluginBridgeConfig(map[string]interface{}{
		"provider":   "apptestreject",
		"capability": "webhook.deliver",
		"url":        "https://hooks.example.test/in",
		"secret":     "s",
	})
	a, err := New(cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()
	if err := a.StartOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg, err := a.Outbox.Enqueue(context.Background(), outbox.Entry{Topic: "orders.created", Payload: map[string]any{"order_id": 7}})
	if err != nil {
		t.Fatal(err)
	}
	attempts, lastError := waitForStatus(t, a, msg.ID, outbox.StatusFailed)
	if attempts != 1 || !strings.Contains(lastError, "does not mark it retriable") {
		t.Fatalf("failed after %d attempt(s): %q", attempts, lastError)
	}
	got := captured(t, capture)
	if len(got) != 1 {
		t.Fatalf("%d envelope(s)", len(got))
	}
	var payload plugins.WebhookDeliverPayload
	_ = json.Unmarshal(got[0].Payload, &payload)
	if payload.URL != "https://hooks.example.test/in" || payload.Headers[outbox.WebhookSignatureHeader] == "" || !strings.Contains(payload.Body, `"topic":"orders.created"`) {
		t.Fatalf("payload %+v", payload)
	}
}

// A bridge that cannot deliver stops the application at boot, saying why.
func TestAttachOutbox_PluginBridgeRefusals(t *testing.T) {
	scriptPlugin(t, "apptestrefused", 0, plugins.CapabilityQueuePublish)
	for _, tc := range []struct {
		name    string
		config  map[string]interface{}
		plugins PluginsConfig
		want    string
	}{
		{"not listed", map[string]interface{}{"provider": "apptestrefused", "capability": "queue.publish"},
			PluginsConfig{Allowed: []PluginAllowance{{Provider: "other", Capabilities: []string{"queue.publish"}}}}, "plugins.allowed has no entry"},
		{"listed for another capability", map[string]interface{}{"provider": "apptestrefused", "capability": "queue.publish"},
			PluginsConfig{Allowed: []PluginAllowance{{Provider: "apptestrefused", Capabilities: []string{"mail.send"}}}}, "not allowed to run queue.publish"},
		{"missing executable", map[string]interface{}{"provider": "apptestmissing", "capability": "queue.publish"}, PluginsConfig{}, "is not on PATH"},
		{"capability not advertised", map[string]interface{}{"provider": "apptestrefused", "capability": "webhook.deliver", "url": "https://x.test"}, PluginsConfig{}, "not webhook.deliver"},
		{"bad timeout", map[string]interface{}{"provider": "apptestrefused", "capability": "queue.publish", "timeout": "soon"}, PluginsConfig{}, "not a positive duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := pluginBridgeConfig(tc.config)
			cfg.Plugins = tc.plugins
			a, err := New(cfg, WithoutDefaults())
			if err == nil {
				_ = a.Shutdown(context.Background())
				t.Fatal("New accepted a bridge that cannot deliver")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// Without an allowlist the bridge runs, as a mail plugin does, and says
// once at boot that it will need listing (DEP-2026-014).
func TestAttachOutbox_PluginBridgeWarnsWithoutAnAllowlist(t *testing.T) {
	scriptPlugin(t, "apptestwarn", 0, plugins.CapabilityQueuePublish)
	cfg := pluginBridgeConfig(map[string]interface{}{"provider": "apptestwarn", "capability": "queue.publish"})
	var logs bytes.Buffer
	a := &App{Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	if _, err := pluginBridgeFromConfig(a, cfg, cfg.Outbox.Bridges[0]); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), "plugin bridge runs an external plugin without an allowlist"); n != 1 {
		t.Fatalf("%d WARN line(s), want 1:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "DEP-2026-014") {
		t.Fatalf("the WARN does not name the deprecation:\n%s", logs.String())
	}
}
