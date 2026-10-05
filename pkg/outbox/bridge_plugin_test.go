// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// fakeHost is a plugins.Host that runs no process: it advertises caps and
// answers every request with answer, recording what it was sent.
type fakeHost struct {
	caps   []string
	answer func(plugins.RequestEnvelope) (plugins.ResponseEnvelope, error)

	mu   sync.Mutex
	sent []plugins.RequestEnvelope
}

func (h *fakeHost) CollectInventory(string, []string, time.Duration) []plugins.Descriptor { return nil }

func (h *fakeHost) ProbeCapabilities(context.Context, string, time.Duration) ([]string, error) {
	return h.caps, nil
}

func (h *fakeHost) ExecuteRequest(_ context.Context, _ string, req plugins.RequestEnvelope, _ time.Duration) (plugins.ResponseEnvelope, error) {
	h.mu.Lock()
	h.sent = append(h.sent, req)
	h.mu.Unlock()
	if h.answer != nil {
		return h.answer(req)
	}
	return plugins.ResponseEnvelope{Version: "v1", RequestID: req.RequestID, OK: true, Output: json.RawMessage(`{"accepted":true}`)}, nil
}

func (h *fakeHost) requests() []plugins.RequestEnvelope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]plugins.RequestEnvelope(nil), h.sent...)
}

// fakeBinary is an existing file standing for the plugin executable, so the
// bridge's Healthy has something to find.
func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), plugins.GenericBinaryPrefix+"fake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func testMessage() Message {
	created := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	return Message{
		ID:          "msg-1",
		Topic:       "orders.created",
		Payload:     []byte(`{"order_id":42}`),
		Status:      StatusProcessing,
		Attempts:    2,
		AvailableAt: created,
		CreatedAt:   created,
	}
}

func TestNewPluginBridge_RefusesWhatCannotDeliver(t *testing.T) {
	bin := fakeBinary(t)
	host := &fakeHost{caps: []string{plugins.CapabilityQueuePublish}}
	cases := []struct {
		name string
		cfg  PluginConfig
		want string
	}{
		{"no name", PluginConfig{Provider: "fake", Capability: plugins.CapabilityQueuePublish}, "name is required"},
		{"no provider", PluginConfig{Name: "b", Capability: plugins.CapabilityQueuePublish}, "provider is required"},
		{"a path for a provider", PluginConfig{Name: "b", Provider: "/usr/bin/x", Capability: plugins.CapabilityQueuePublish}, "not a provider name"},
		{"no capability", PluginConfig{Name: "b", Provider: "fake"}, "capability is required"},
		{"a capability the outbox does not deliver", PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityMailSend}, "not one the outbox delivers through"},
		{"webhook without url", PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityWebhookDeliver, Binary: bin, Host: host}, "url is required"},
		{"bad payload encoding", PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityWebhookDeliver, URL: "https://x.test", PayloadEncoding: "xml", Binary: bin, Host: host}, "payload_encoding"},
		{"refused by the allowlist", PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: bin, Host: host,
			Policy: plugins.Policy{Allowed: []plugins.Allowance{{Provider: "fake", Capabilities: []string{plugins.CapabilityMailSend}}}}}, "plugins.allowed"},
		{"switched off", PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: bin, Host: host,
			Policy: plugins.Policy{DenyExternal: true}}, "plugins.allow_external is false"},
		{"not on PATH", PluginConfig{Name: "b", Provider: "nucleus-outbox-test-missing", Capability: plugins.CapabilityQueuePublish, Host: host}, "is not on PATH"},
		{"capability not advertised", PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityWebhookDeliver, URL: "https://x.test", Binary: bin, Host: host}, "advertises [queue.publish], not webhook.deliver"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPluginBridge(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

// The policy is asked before anything runs: a refused provider is never
// asked for its capabilities.
func TestNewPluginBridge_PolicyRefusesBeforeExecuting(t *testing.T) {
	probed := false
	host := &probeRecorder{probed: &probed}
	_, err := NewPluginBridge(PluginConfig{Name: "b", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: fakeBinary(t), Host: host,
		Policy: plugins.Policy{DenyExternal: true}})
	var refused *plugins.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("want a RefusedError, got %v", err)
	}
	if probed {
		t.Fatal("the refused plugin was executed to read its capabilities")
	}
}

type probeRecorder struct {
	fakeHost
	probed *bool
}

func (p *probeRecorder) ProbeCapabilities(context.Context, string, time.Duration) ([]string, error) {
	*p.probed = true
	return []string{plugins.CapabilityQueuePublish}, nil
}

func TestPluginBridge_QueuePublishEnvelope(t *testing.T) {
	host := &fakeHost{caps: []string{plugins.CapabilityQueuePublish}}
	b, err := NewPluginBridge(PluginConfig{
		Name: "events", Provider: "fake", Capability: plugins.CapabilityQueuePublish,
		Binary: fakeBinary(t), Host: host, Timeout: 3 * time.Second,
		Headers: map[string]string{"source": "shop"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sent := host.requests()
	if len(sent) != 1 {
		t.Fatalf("%d request(s), want 1", len(sent))
	}
	req := sent[0]
	if req.Capability != plugins.CapabilityQueuePublish || req.Provider != "fake" || req.TimeoutMS != 3000 {
		t.Errorf("envelope: %+v", req)
	}
	for k, want := range map[string]string{"outbox_message_id": "msg-1", "outbox_topic": "orders.created", "outbox_attempt": "2", "outbox_bridge": "events"} {
		if req.Metadata[k] != want {
			t.Errorf("metadata %s = %q, want %q", k, req.Metadata[k], want)
		}
	}
	var payload plugins.QueuePublishPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Topic != "orders.created" || payload.Key != "msg-1" || string(payload.Body) != `{"order_id":42}` || payload.Headers["source"] != "shop" {
		t.Errorf("payload: %+v body=%s", payload, payload.Body)
	}

	// A configured topic replaces the outbox topic on the queue.
	b.topic = "shop-events"
	_ = b.Send(context.Background(), testMessage())
	sent = host.requests()
	_ = json.Unmarshal(sent[1].Payload, &payload)
	if payload.Topic != "shop-events" {
		t.Errorf("topic = %q, want the configured one", payload.Topic)
	}
}

// A webhook.deliver plugin is handed exactly the request the webhook bridge
// would have sent for the same message: body, payload encoding and
// signature, byte for byte.
func TestPluginBridge_WebhookDeliverCarriesTheWebhookRequest(t *testing.T) {
	type captured struct {
		body    []byte
		headers http.Header
	}
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- captured{body, r.Header.Clone()}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	for _, encoding := range []string{"", PayloadEncodingJSON} {
		webhook, err := NewWebhookBridge(WebhookConfig{Name: "w", URL: srv.URL, Secret: "s3cret", PayloadEncoding: encoding,
			Headers: map[string]string{"authorization": "Bearer t"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := webhook.Send(context.Background(), testMessage()); err != nil {
			t.Fatal(err)
		}
		direct := <-got

		host := &fakeHost{caps: []string{plugins.CapabilityWebhookDeliver}}
		b, err := NewPluginBridge(PluginConfig{
			Name: "hooks", Provider: "fake", Capability: plugins.CapabilityWebhookDeliver, Binary: fakeBinary(t), Host: host,
			URL: srv.URL, Secret: "s3cret", PayloadEncoding: encoding, Headers: map[string]string{"authorization": "Bearer t"},
			Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Send(context.Background(), testMessage()); err != nil {
			t.Fatal(err)
		}
		var payload plugins.WebhookDeliverPayload
		if err := json.Unmarshal(host.requests()[0].Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.URL != srv.URL || payload.Method != http.MethodPost || payload.TimeoutMS != 4000 {
			t.Errorf("%q: payload %+v", encoding, payload)
		}
		if payload.Body != string(direct.body) {
			t.Errorf("%q: the plugin body differs from the webhook bridge's:\n%s\n%s", encoding, payload.Body, direct.body)
		}
		for _, h := range []string{"Content-Type", WebhookPayloadEncodingHeader, WebhookSignatureHeader, "Authorization"} {
			if payload.Headers[h] != direct.headers.Get(h) {
				t.Errorf("%q: header %s = %q, the webhook bridge sends %q", encoding, h, payload.Headers[h], direct.headers.Get(h))
			}
		}
	}
}

func TestPluginBridge_FailuresKeepTheOutboxSemantics(t *testing.T) {
	fail := func(exit int, retriable bool) func(plugins.RequestEnvelope) (plugins.ResponseEnvelope, error) {
		return func(plugins.RequestEnvelope) (plugins.ResponseEnvelope, error) {
			return plugins.ResponseEnvelope{}, &plugins.ExecutionError{ExitCode: exit, Retriable: retriable, Code: "X", Message: "no"}
		}
	}
	cases := []struct {
		name      string
		answer    func(plugins.RequestEnvelope) (plugins.ResponseEnvelope, error)
		permanent bool
	}{
		{"validation (10) is the dead letter", fail(plugins.ExitCodeValidation, false), true},
		{"rejection (30) is the dead letter", fail(plugins.ExitCodeRejected, false), true},
		{"a rejection the plugin marks retriable is retried", fail(plugins.ExitCodeRejected, true), false},
		{"transient (20) is retried", fail(plugins.ExitCodeTransient, true), false},
		{"timeout (40) is retried", fail(plugins.ExitCodeTimeout, true), false},
		{"internal (50) is retried", fail(plugins.ExitCodeInternal, true), false},
		{"a crash is retried", fail(2, false), false},
		{"a binary that cannot start is retried", fail(-1, false), false},
		{"accepted:false is retried", func(req plugins.RequestEnvelope) (plugins.ResponseEnvelope, error) {
			return plugins.ResponseEnvelope{OK: true, Output: json.RawMessage(`{"accepted":false}`)}, nil
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := &fakeHost{caps: []string{plugins.CapabilityQueuePublish}, answer: tc.answer}
			b, err := NewPluginBridge(PluginConfig{Name: "q", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: fakeBinary(t), Host: host})
			if err != nil {
				t.Fatal(err)
			}
			err = b.Send(context.Background(), testMessage())
			if err == nil {
				t.Fatal("Send succeeded")
			}
			if IsPermanent(err) != tc.permanent {
				t.Fatalf("IsPermanent = %t, want %t (%v)", IsPermanent(err), tc.permanent, err)
			}
		})
	}

	t.Run("a refusal during shutdown is retried", func(t *testing.T) {
		host := &fakeHost{caps: []string{plugins.CapabilityQueuePublish}, answer: fail(plugins.ExitCodeValidation, false)}
		b, _ := NewPluginBridge(PluginConfig{Name: "q", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: fakeBinary(t), Host: host})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := b.Send(ctx, testMessage()); err == nil || IsPermanent(err) {
			t.Fatalf("a cancelled delivery must stay retriable: %v", err)
		}
	})

	t.Run("a payload that is not JSON is the dead letter", func(t *testing.T) {
		host := &fakeHost{caps: []string{plugins.CapabilityQueuePublish}}
		b, _ := NewPluginBridge(PluginConfig{Name: "q", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: fakeBinary(t), Host: host})
		msg := testMessage()
		msg.Payload = []byte("not json")
		if err := b.Send(context.Background(), msg); !IsPermanent(err) {
			t.Fatalf("want a permanent failure, got %v", err)
		}
		if len(host.requests()) != 0 {
			t.Fatal("the plugin was run for a message it cannot carry")
		}
	})

	t.Run("a webhook status outside 2xx is retried", func(t *testing.T) {
		host := &fakeHost{caps: []string{plugins.CapabilityWebhookDeliver}, answer: func(plugins.RequestEnvelope) (plugins.ResponseEnvelope, error) {
			return plugins.ResponseEnvelope{OK: true, Output: json.RawMessage(`{"accepted":true,"status_code":503}`)}, nil
		}}
		b, _ := NewPluginBridge(PluginConfig{Name: "w", Provider: "fake", Capability: plugins.CapabilityWebhookDeliver, URL: "https://x.test", Binary: fakeBinary(t), Host: host})
		err := b.Send(context.Background(), testMessage())
		if err == nil || IsPermanent(err) || !strings.Contains(err.Error(), "answered 503") {
			t.Fatalf("want a retriable failure naming the status, got %v", err)
		}
	})
}

func TestPluginBridge_Healthy(t *testing.T) {
	bin := fakeBinary(t)
	b, err := NewPluginBridge(PluginConfig{Name: "q", Provider: "fake", Capability: plugins.CapabilityQueuePublish, Binary: bin,
		Host: &fakeHost{caps: []string{plugins.CapabilityQueuePublish}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
	_ = os.Remove(bin)
	if err := b.Healthy(context.Background()); err == nil {
		t.Fatal("Healthy with the executable gone")
	}
}

// The dispatcher honours a permanent failure: the message is failed on the
// attempt that returned it, with attempts to spare.
func TestDispatcherFailsAPermanentFailureAtOnce(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(context.Background(), Entry{Topic: "orders.created", Payload: map[string]any{"id": 1}}); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewDispatcher(store, func(context.Context, Message) error {
		return Permanent(errors.New("the receiver refused it"))
	}, DispatcherConfig{LeaseOwner: "test", BatchSize: 1, MaxAttempts: 5, BaseDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || result.Retried != 0 {
		t.Fatalf("result %+v: want the message failed on its first attempt", result)
	}
	var lastError string
	var attempts int
	if err := db.QueryRow("SELECT last_error, attempts FROM nucleus_outbox").Scan(&lastError, &attempts); err != nil {
		t.Fatal(err)
	}
	if lastError != "the receiver refused it" || attempts != 1 {
		t.Fatalf("last_error %q after %d attempt(s)", lastError, attempts)
	}
	// The dead letter is the ordinary one: RequeueFailed brings it back.
	if n, err := store.RequeueFailed(context.Background()); err != nil || n != 1 {
		t.Fatalf("RequeueFailed: %d, %v", n, err)
	}
}

// With several bridges, the message fails at once only when every failure
// was permanent.
func TestDispatcherFanOutPermanence(t *testing.T) {
	for _, tc := range []struct {
		name          string
		second        error
		failed, retry int
	}{
		{"one permanent, one retriable: retried", errors.New("503"), 0, 1},
		{"both permanent: failed", Permanent(errors.New("410")), 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openOutboxTestDB(t)
			store, _ := NewStore(db, Config{Flavor: FlavorSQLite})
			if _, err := store.Enqueue(context.Background(), Entry{Topic: "orders.created", Payload: map[string]any{"id": 1}}); err != nil {
				t.Fatal(err)
			}
			registry, router := NewBridgeRegistry(), NewRouter()
			_ = registry.Register(&mockBridge{name: "a", sendErr: Permanent(errors.New("400"))})
			_ = registry.Register(&mockBridge{name: "b", sendErr: tc.second})
			router.AddRoute("*", "a", "b")
			dispatcher, err := NewDispatcher(store, nil, DispatcherConfig{LeaseOwner: "test", BatchSize: 1, MaxAttempts: 5,
				BaseDelay: time.Millisecond, Registry: registry, Router: router})
			if err != nil {
				t.Fatal(err)
			}
			result, err := dispatcher.RunOnce(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Failed != tc.failed || result.Retried != tc.retry {
				t.Fatalf("result %+v", result)
			}
			var lastError string
			_ = db.QueryRow("SELECT last_error FROM nucleus_outbox").Scan(&lastError)
			if !strings.HasPrefix(lastError, "dispatch errors: [") {
				t.Fatalf("last_error %q: the fan-out text changed", lastError)
			}
		})
	}
}
