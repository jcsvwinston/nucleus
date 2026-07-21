package nucleus

// The outbox webhook bridge signs delivery bodies with the SAME scheme
// module webhooks verify: HMAC-SHA256 of the raw body, "sha256=<hex>", in
// the X-Nucleus-Signature header. pkg/outbox cannot import pkg/nucleus
// (cycle via pkg/app), so it carries its own private copy of the signer —
// this test is the cross-package pin that the two implementations can never
// drift apart: the bridge's emitted header must be accepted by the module
// verifier and must equal SignWebhookBody's output for the same body.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

func TestOutboxBridgeSignature_MatchesModuleWebhookScheme(t *testing.T) {
	const secret = "shared-webhook-secret"

	var (
		body   []byte
		header string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		body = b
		header = r.Header.Get(WebhookSignatureHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	bridge, err := outbox.NewWebhookBridge(outbox.WebhookConfig{
		Name:   "signed",
		URL:    srv.URL,
		Secret: secret,
	})
	if err != nil {
		t.Fatalf("create bridge: %v", err)
	}
	defer bridge.Close()

	if err := bridge.Send(context.Background(), outbox.Message{
		ID:      "msg-1",
		Topic:   "orders.placed",
		Payload: []byte(`{"order_id":42}`),
		Status:  outbox.StatusPending,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Same header constant on both sides of the contract.
	if outbox.WebhookSignatureHeader != WebhookSignatureHeader {
		t.Fatalf("outbox.WebhookSignatureHeader = %q, nucleus.WebhookSignatureHeader = %q",
			outbox.WebhookSignatureHeader, WebhookSignatureHeader)
	}

	// The module-webhook helper reproduces the bridge's signature exactly...
	if want := SignWebhookBody(secret, body); header != want {
		t.Fatalf("bridge signature %q != SignWebhookBody %q", header, want)
	}

	// ...and the module-webhook verifier (the code behind WebhookSpec.Secret)
	// accepts it in constant time.
	if !verifyWebhookSignature(secret, body, header) {
		t.Fatalf("module webhook verifier rejected the bridge signature %q", header)
	}
	if verifyWebhookSignature("other-secret", body, header) {
		t.Fatal("module webhook verifier accepted the signature under the wrong secret")
	}
}
