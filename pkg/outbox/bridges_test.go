package outbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBridgeRegistry(t *testing.T) {
	registry := NewBridgeRegistry()

	// Test registering a bridge
	mockBridge := &mockBridge{name: "test-bridge"}
	err := registry.Register(mockBridge)
	if err != nil {
		t.Fatalf("register bridge: %v", err)
	}

	// Test getting a bridge
	bridge, ok := registry.Get("test-bridge")
	if !ok {
		t.Fatal("bridge not found")
	}
	if bridge.Name() != "test-bridge" {
		t.Fatalf("unexpected bridge name: %s", bridge.Name())
	}

	// Test duplicate registration
	err = registry.Register(mockBridge)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}

	// Test listing bridges
	bridges := registry.List()
	if len(bridges) != 1 {
		t.Fatalf("expected 1 bridge, got %d", len(bridges))
	}
}

func TestRouter(t *testing.T) {
	router := NewRouter()

	// Test adding routes
	router.AddRoute("billing.*", "webhook-billing")
	router.AddRoute("orders.created", "kafka-orders")

	// Test matching patterns
	matches := router.Match("billing.invoice.created")
	if len(matches) != 1 || matches[0] != "webhook-billing" {
		t.Fatalf("expected webhook-billing for billing.invoice.created, got %v", matches)
	}

	matches = router.Match("orders.created")
	if len(matches) != 1 || matches[0] != "kafka-orders" {
		t.Fatalf("expected kafka-orders for orders.created, got %v", matches)
	}

	matches = router.Match("orders.updated")
	if len(matches) != 0 {
		t.Fatalf("expected no matches for orders.updated, got %v", matches)
	}

	// Test wildcard
	router.AddRoute("*", "default-bridge")
	matches = router.Match("any.topic")
	if len(matches) != 1 || matches[0] != "default-bridge" {
		t.Fatalf("expected default-bridge for wildcard, got %v", matches)
	}
}

func TestWebhookBridge(t *testing.T) {
	// This test would require a test HTTP server
	// For now, we'll just test the configuration
	cfg := WebhookConfig{
		Name: "test-webhook",
		URL:  "http://localhost:8080/webhook",
		Headers: map[string]string{
			"Authorization": "Bearer test-token",
		},
	}

	bridge, err := NewWebhookBridge(cfg)
	if err != nil {
		t.Fatalf("create webhook bridge: %v", err)
	}

	if bridge.Name() != "test-webhook" {
		t.Fatalf("unexpected bridge name: %s", bridge.Name())
	}

	if err := bridge.Close(); err != nil {
		t.Fatalf("close bridge: %v", err)
	}
}

// captureWebhook starts a test HTTP server that records the last request
// body it receives and returns 204.
func captureWebhook(t *testing.T) (*httptest.Server, func() []byte) {
	t.Helper()
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read webhook body: %v", err)
		}
		received = body
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []byte { return received }
}

// TestWebhookBridgeSend_PayloadIsEmbeddedJSON pins the opt-in embedded shape
// (issue #228): with PayloadEncoding "json", the payload stored by Enqueue —
// already a JSON document — travels as nested JSON the consumer reads
// directly, not as the base64 string Go emits for a plain []byte, which
// forced a second decode.
func TestWebhookBridgeSend_PayloadIsEmbeddedJSON(t *testing.T) {
	srv, received := captureWebhook(t)

	bridge, err := NewWebhookBridge(WebhookConfig{Name: "test", URL: srv.URL, PayloadEncoding: PayloadEncodingJSON})
	if err != nil {
		t.Fatalf("create webhook bridge: %v", err)
	}
	defer bridge.Close()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	msg := Message{
		ID:          "msg-1",
		Topic:       "orders.created",
		Payload:     []byte(`{"order_id":42,"total":"19.90"}`),
		Status:      StatusPending,
		Attempts:    1,
		AvailableAt: now,
		CreatedAt:   now,
	}
	if err := bridge.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var wire struct {
		ID      string          `json:"id"`
		Topic   string          `json:"topic"`
		Payload json.RawMessage `json:"payload"`
		Status  string          `json:"status"`
	}
	if err := json.Unmarshal(received(), &wire); err != nil {
		t.Fatalf("unmarshal webhook body %q: %v", received(), err)
	}
	if wire.ID != "msg-1" || wire.Topic != "orders.created" || wire.Status != "pending" {
		t.Fatalf("unexpected envelope: %+v", wire)
	}

	// The payload must be the nested JSON object itself, not a quoted
	// base64 string.
	trimmed := strings.TrimSpace(string(wire.Payload))
	if !strings.HasPrefix(trimmed, "{") {
		t.Fatalf("payload on the wire = %s, want a nested JSON object", trimmed)
	}
	var inner struct {
		OrderID int    `json:"order_id"`
		Total   string `json:"total"`
	}
	if err := json.Unmarshal(wire.Payload, &inner); err != nil {
		t.Fatalf("decode nested payload %s: %v", wire.Payload, err)
	}
	if inner.OrderID != 42 || inner.Total != "19.90" {
		t.Fatalf("nested payload = %+v", inner)
	}
}

// TestWebhookBridgeSend_NonJSONPayloadKeepsBase64 pins the documented json-
// mode fallback: a hand-built Message whose payload is not valid JSON keeps
// the base64-string form instead of producing an invalid webhook body, and
// the encoding header declares base64 for that delivery.
func TestWebhookBridgeSend_NonJSONPayloadKeepsBase64(t *testing.T) {
	srv, received := captureWebhook(t)

	bridge, err := NewWebhookBridge(WebhookConfig{Name: "test", URL: srv.URL, PayloadEncoding: PayloadEncodingJSON})
	if err != nil {
		t.Fatalf("create webhook bridge: %v", err)
	}
	defer bridge.Close()

	raw := []byte{0xff, 0xfe, 0x00, 0x01}
	msg := Message{ID: "msg-2", Topic: "raw.bytes", Payload: raw, Status: StatusPending}
	if err := bridge.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var wire struct {
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(received(), &wire); err != nil {
		t.Fatalf("unmarshal webhook body %q: %v", received(), err)
	}
	decoded, err := base64.StdEncoding.DecodeString(wire.Payload)
	if err != nil {
		t.Fatalf("payload is not the documented base64 fallback: %v", err)
	}
	if string(decoded) != string(raw) {
		t.Fatalf("base64 fallback roundtrip = %v, want %v", decoded, raw)
	}
}

// TestWebhookBridgeSend_EmptyPayloadIsNull pins the documented json-mode
// empty-payload form: null, not "" and not the base64 of zero bytes.
func TestWebhookBridgeSend_EmptyPayloadIsNull(t *testing.T) {
	srv, received := captureWebhook(t)

	bridge, err := NewWebhookBridge(WebhookConfig{Name: "test", URL: srv.URL, PayloadEncoding: PayloadEncodingJSON})
	if err != nil {
		t.Fatalf("create webhook bridge: %v", err)
	}
	defer bridge.Close()

	msg := Message{ID: "msg-3", Topic: "empty.payload", Status: StatusPending}
	if err := bridge.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var wire map[string]json.RawMessage
	if err := json.Unmarshal(received(), &wire); err != nil {
		t.Fatalf("unmarshal webhook body %q: %v", received(), err)
	}
	if got := strings.TrimSpace(string(wire["payload"])); got != "null" {
		t.Fatalf("empty payload on the wire = %s, want null", got)
	}
}

func TestWebhookBridgeValidation(t *testing.T) {
	// Test missing name
	_, err := NewWebhookBridge(WebhookConfig{URL: "http://localhost"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	// Test missing URL
	_, err = NewWebhookBridge(WebhookConfig{Name: "test"})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestKafkaBridge(t *testing.T) {
	cfg := KafkaConfig{
		Name:    "test-kafka",
		Brokers: []string{"localhost:9092"},
		Topic:   "events",
	}

	_, err := NewKafkaBridge(cfg)
	if err == nil {
		t.Fatal("expected disabled kafka bridge error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled kafka bridge error, got %v", err)
	}
}

func TestKafkaBridgeValidation(t *testing.T) {
	// Test missing name
	_, err := NewKafkaBridge(KafkaConfig{Brokers: []string{"localhost:9092"}, Topic: "events"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}

	// Test missing brokers
	_, err = NewKafkaBridge(KafkaConfig{Name: "test", Topic: "events"})
	if err == nil {
		t.Fatal("expected error for missing brokers")
	}

	// Test missing topic
	_, err = NewKafkaBridge(KafkaConfig{Name: "test", Brokers: []string{"localhost:9092"}})
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
}

// mockBridge is a test implementation of Bridge
type mockBridge struct {
	name    string
	sendErr error
	healthy bool
	closed  bool
}

func (m *mockBridge) Name() string {
	return m.name
}

func (m *mockBridge) Send(ctx context.Context, msg Message) error {
	return m.sendErr
}

func (m *mockBridge) Healthy(ctx context.Context) error {
	if !m.healthy {
		return &testError{msg: "unhealthy"}
	}
	return nil
}

func (m *mockBridge) Close() error {
	m.closed = true
	return nil
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
