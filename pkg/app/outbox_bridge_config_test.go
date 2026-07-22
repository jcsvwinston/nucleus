package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// TestAttachOutbox_WebhookBridgeConfigKeys pins the config→bridge wiring of
// the webhook bridge contract keys: `outbox.bridges.<n>.config.secret` must
// produce a signed delivery (X-Nucleus-Signature over the exact body) and
// `outbox.bridges.<n>.config.payload_encoding` must select the wire shape
// declared in X-Outbox-Payload-Encoding. A typo in the getConfigString key
// names would fail here, not in production.
func TestAttachOutbox_WebhookBridgeConfigKeys(t *testing.T) {
	const secret = "wiring-secret"

	type capture struct {
		body    []byte
		headers http.Header
	}
	got := make(chan capture, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read delivery body: %v", err)
		}
		select {
		case got <- capture{body: body, headers: r.Header.Clone()}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := testAppConfig()
	cfg.Outbox = OutboxConfig{
		Enabled:       true,
		TableName:     "nucleus_outbox",
		LeaseDuration: 30 * time.Second,
		MaxRetries:    3,
		RetryBackoff:  time.Second,
		Bridges: []BridgeConfig{{
			Name: "wired",
			Type: "webhook",
			Config: map[string]interface{}{
				"url":              srv.URL,
				"pattern":          "*",
				"secret":           secret,
				"payload_encoding": "json",
			},
		}},
	}

	a, err := New(cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := a.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	if _, err := a.Outbox.Enqueue(context.Background(), outbox.Entry{
		Topic:   "wiring.test",
		Payload: map[string]any{"order_id": 42},
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	var delivery capture
	select {
	case delivery = <-got:
	case <-time.After(15 * time.Second):
		t.Fatal("outbox dispatcher never delivered to the webhook bridge")
	}

	// payload_encoding: json reached the bridge — declared on the wire.
	if enc := delivery.headers.Get(outbox.WebhookPayloadEncodingHeader); enc != outbox.PayloadEncodingJSON {
		t.Fatalf("%s = %q, want %q", outbox.WebhookPayloadEncodingHeader, enc, outbox.PayloadEncodingJSON)
	}

	// secret reached the bridge — the delivery is signed over the exact body.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(delivery.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig := delivery.headers.Get(outbox.WebhookSignatureHeader); sig != want {
		t.Fatalf("%s = %q, want %q", outbox.WebhookSignatureHeader, sig, want)
	}
}
