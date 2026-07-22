package outbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebhookSignatureHeader carries the HMAC-SHA256 signature of the webhook
// body when the bridge is configured with a Secret. It is the SAME header,
// with the SAME "sha256=<hex>" value format, that module webhooks
// (pkg/nucleus, WebhookSpec.Secret) verify on inbound requests — one
// signing scheme across the framework, so a consumer can verify outbox
// deliveries with the same code (and the same helper,
// nucleus.SignWebhookBody) it already uses for module webhooks.
const WebhookSignatureHeader = "X-Nucleus-Signature"

// WebhookPayloadEncodingHeader declares, on every webhook delivery, how the
// "payload" field of that message's body is encoded. Its value is one of
// PayloadEncodingBase64 or PayloadEncodingJSON. The header is always
// present, so a consumer never has to guess the payload shape.
//
// The header is INFORMATIONAL and deliberately NOT part of the signed
// material (SEC-3). The bridge signs the body alone — byte-for-byte the
// module-webhook scheme (nucleus.SignWebhookBody), which is exactly why one
// verifier serves both surfaces; binding this header into the signature would
// fork that scheme and break every consumer that verifies outbox deliveries
// with the module-webhook verifier. Because the header is unsigned, a consumer
// must never let it drive a decoding or security decision: decode by the
// encoding you were configured to expect, and use CheckPayloadEncoding to
// reject — as defense in depth — any delivery whose declared encoding
// disagrees with that expectation. Flipping the header in transit does not
// forge anything (it leaves the body signature valid and, at worst, makes a
// consumer that trusts it misparse a legitimate body into a 400), which is
// why closing this hole is a hardening, not a fix for an exploitable bug.
const WebhookPayloadEncodingHeader = "X-Outbox-Payload-Encoding"

// ErrPayloadEncodingMismatch reports that a webhook delivery declared, in its
// WebhookPayloadEncodingHeader, a payload encoding different from the one the
// consumer was configured to expect. Returned (wrapped) by
// CheckPayloadEncoding.
var ErrPayloadEncodingMismatch = errors.New("outbox: payload encoding mismatch")

// CheckPayloadEncoding is the consumer-side defense-in-depth check for the
// WebhookPayloadEncodingHeader (SEC-3). Since the header is not part of the
// signed material, a consumer decodes by the encoding it was configured to
// expect and MAY call CheckPayloadEncoding to reject a delivery whose declared
// encoding disagrees with that expectation:
//
//	enc := r.Header.Get(outbox.WebhookPayloadEncodingHeader)
//	if err := outbox.CheckPayloadEncoding(cfgEncoding, enc); err != nil {
//	    http.Error(w, "bad request", http.StatusBadRequest)
//	    return
//	}
//
// Both arguments are normalized the way NewWebhookBridge normalizes
// PayloadEncoding: surrounding whitespace trimmed, case folded, and an empty
// value taken as PayloadEncodingBase64 (the default wire shape). A match
// returns nil; a mismatch returns an error wrapping ErrPayloadEncodingMismatch
// and naming both encodings.
//
// One legitimate divergence exists: a bridge in PayloadEncodingJSON mode
// downgrades a payload that is not valid JSON to the base64 shape for that
// delivery (see Send), declaring "base64" accordingly. That fallback only
// affects hand-built messages — a producer that enqueues through the store
// always emits valid JSON — so a store-backed consumer never sees the
// divergence and can reject the mismatch; a consumer that also accepts
// hand-built payloads should tolerate a base64 delivery under a json
// expectation.
func CheckPayloadEncoding(expected, delivered string) error {
	norm := func(v string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return PayloadEncodingBase64
		}
		return v
	}
	e, d := norm(expected), norm(delivered)
	if e != d {
		return fmt.Errorf("%w: expected %q, delivery declared %q", ErrPayloadEncodingMismatch, e, d)
	}
	return nil
}

// Values of WebhookPayloadEncodingHeader and of
// WebhookConfig.PayloadEncoding.
//
// Under either declared encoding, a message with no payload puts JSON null
// in the "payload" field.
const (
	// PayloadEncodingBase64 declares the classic wire shape: the "payload"
	// field is a JSON string holding the base64 encoding of the raw payload
	// bytes (Go's default encoding/json representation of []byte).
	PayloadEncodingBase64 = "base64"

	// PayloadEncodingJSON declares the embedded shape: the "payload" field
	// is the payload's JSON document itself, embedded verbatim.
	PayloadEncodingJSON = "json"
)

// WebhookBridge delivers outbox messages via HTTP webhooks.
//
// This bridge sends outbox messages as HTTP POST requests to a configured URL.
// The message payload is serialized as JSON and includes the message ID, topic,
// payload, status, and metadata. Custom headers can be configured for authentication
// and other purposes.
//
// Every delivery carries the WebhookPayloadEncodingHeader declaring the
// payload shape, and — when a Secret is configured — the
// WebhookSignatureHeader with an HMAC-SHA256 signature of the body.
//
// Example usage:
//
//	bridge, err := outbox.NewWebhookBridge(outbox.WebhookConfig{
//	    Name:   "notifications",
//	    URL:    "https://api.example.com/webhooks",
//	    Secret: os.Getenv("NOTIFICATIONS_WEBHOOK_SECRET"),
//	    Headers: map[string]string{
//	        "Authorization": "Bearer token",
//	    },
//	})
type WebhookBridge struct {
	name     string
	url      string
	headers  map[string]string
	secret   string
	encoding string
	client   *http.Client
}

// WebhookConfig configures a webhook bridge.
//
// The URL field is required and must be a valid HTTP/HTTPS endpoint.
// Headers can be used for authentication (e.g., Bearer tokens) or custom metadata.
// Timeout defaults to 30 seconds if not specified.
//
// Secret, when non-empty, makes the bridge sign every delivery body with
// HMAC-SHA256 and send the result as WebhookSignatureHeader ("sha256=<hex>")
// — the exact scheme module webhooks verify, so consumers can share one
// verifier. An empty Secret sends unsigned deliveries: the consumer must
// authenticate the caller by other means.
//
// PayloadEncoding selects the wire shape of the "payload" field:
// PayloadEncodingBase64 (the default when empty — the classic shape every
// tagged release up to v1.4.0 emits) or PayloadEncodingJSON (opt-in: the
// payload JSON document is embedded verbatim, saving the consumer a base64
// round-trip). Any other value is rejected by NewWebhookBridge. Whatever the
// mode, each delivery declares its actual payload shape in
// WebhookPayloadEncodingHeader.
type WebhookConfig struct {
	Name            string
	URL             string
	Headers         map[string]string
	Timeout         time.Duration
	Secret          string
	PayloadEncoding string
}

// NewWebhookBridge creates a new webhook bridge.
//
// Returns an error if the name or URL is empty, or if PayloadEncoding is
// neither empty (meaning PayloadEncodingBase64), PayloadEncodingBase64 nor
// PayloadEncodingJSON. The HTTP client is configured with the specified
// timeout (default 30 seconds).
func NewWebhookBridge(cfg WebhookConfig) (*WebhookBridge, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("webhook: name is required")
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("webhook: url is required")
	}

	encoding := strings.ToLower(strings.TrimSpace(cfg.PayloadEncoding))
	switch encoding {
	case "":
		encoding = PayloadEncodingBase64
	case PayloadEncodingBase64, PayloadEncodingJSON:
	default:
		return nil, fmt.Errorf("webhook: payload_encoding %q is not supported (use %q or %q)",
			cfg.PayloadEncoding, PayloadEncodingBase64, PayloadEncodingJSON)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &WebhookBridge{
		name:     cfg.Name,
		url:      cfg.URL,
		headers:  cfg.Headers,
		secret:   cfg.Secret,
		encoding: encoding,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Name returns the bridge name.
func (b *WebhookBridge) Name() string {
	return b.name
}

// Send delivers a message via HTTP POST.
//
// The message is serialized as JSON with the following structure:
//
//	{
//	  "id": "message-id",
//	  "topic": "event.topic",
//	  "payload": "eyJvcmRlcl9pZCI6NDJ9",
//	  "status": "pending",
//	  "attempts": 1,
//	  "available_at": "2024-01-01T00:00:00Z",
//	  "created_at": "2024-01-01T00:00:00Z"
//	}
//
// The shape of the "payload" field is governed by
// WebhookConfig.PayloadEncoding and declared per delivery in the
// WebhookPayloadEncodingHeader, which is always present:
//
//   - PayloadEncodingBase64 (the default): the field is a JSON string with
//     the base64 encoding of the raw payload bytes — byte-for-byte the wire
//     shape of every tagged release up to v1.4.0, so existing consumers keep
//     working without changes. The header is "base64".
//   - PayloadEncodingJSON (opt-in): the field embeds the payload verbatim as
//     JSON — Store.Enqueue encodes Entry.Payload with encoding/json, so
//     Message.Payload is a JSON document by construction and consumers read
//     it directly. The header is "json". A payload that is not valid JSON —
//     possible only for a Message built by hand rather than read from the
//     store — falls back to the base64-string form for that delivery, and the
//     header declares "base64" accordingly.
//
// Under either mode a message with no payload puts JSON null in the field.
//
// When WebhookConfig.Secret is set, the request also carries
// WebhookSignatureHeader with the HMAC-SHA256 signature of the exact body
// bytes ("sha256=<hex>", identical to module webhooks).
//
// Returns an error if the HTTP request fails or returns a non-2xx status code.
// The response body is included in error messages for debugging.
func (b *WebhookBridge) Send(ctx context.Context, msg Message) error {
	wirePayload, encoding := webhookPayload(msg.Payload, b.encoding)
	payload := map[string]interface{}{
		"id":           msg.ID,
		"topic":        msg.Topic,
		"payload":      wirePayload,
		"status":       msg.Status,
		"attempts":     msg.Attempts,
		"available_at": msg.AvailableAt,
		"created_at":   msg.CreatedAt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	return b.post(ctx, body, encoding)
}

// webhookPayload returns the wire representation of an outbox payload plus
// the encoding it declares in WebhookPayloadEncodingHeader.
//
// In the default base64 mode the payload bytes are handed to encoding/json
// verbatim — a []byte marshals as a base64 JSON string and a nil payload as
// null — which keeps the wire byte-identical to the classic shape.
//
// In json mode the payload is embedded verbatim as json.RawMessage
// (Message.Payload is a JSON document by construction: Store.Enqueue encodes
// Entry.Payload with encoding/json). The fallbacks are for messages built by
// hand: an empty payload becomes null, and bytes that are not valid JSON
// keep the base64-string form — dropping or corrupting them would be worse —
// with the declared encoding downgraded to base64 for that delivery.
func webhookPayload(p []byte, mode string) (any, string) {
	if mode == PayloadEncodingJSON {
		if len(p) == 0 {
			return nil, PayloadEncodingJSON
		}
		if json.Valid(p) {
			return json.RawMessage(p), PayloadEncodingJSON
		}
		return p, PayloadEncodingBase64 // []byte marshals as a base64 JSON string
	}
	// Base64 (default): the raw bytes, exactly as encoding/json represents
	// []byte (nil marshals as null — no payload).
	return p, PayloadEncodingBase64
}

// signWebhookBody returns the WebhookSignatureHeader value ("sha256=<hex>")
// for body under secret. It mirrors nucleus.SignWebhookBody exactly — same
// algorithm, same output format — so module-webhook verifiers accept bridge
// deliveries; pkg/outbox cannot import pkg/nucleus (import cycle via
// pkg/app), and the cross-package equivalence is pinned by a test in
// pkg/nucleus.
func signWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (b *WebhookBridge) post(ctx context.Context, body []byte, encoding string) error {

	req, err := http.NewRequestWithContext(ctx, "POST", b.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range b.headers {
		req.Header.Set(key, value)
	}
	// Contract headers are set after the custom ones so a configured header
	// can never clobber what the receiver relies on to decode and verify.
	req.Header.Set(WebhookPayloadEncodingHeader, encoding)
	if b.secret != "" {
		req.Header.Set(WebhookSignatureHeader, signWebhookBody(b.secret, body))
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Healthy checks if the webhook endpoint is reachable.
//
// This method performs a GET request to the configured URL and checks the response.
// Status codes 2xx-4xx are considered healthy (the endpoint is responding).
// Status code 5xx indicates a server error and is considered unhealthy.
//
// Note: Some webhook endpoints may not support GET requests. In such cases,
// consider disabling health checks or implementing a custom health check endpoint.
func (b *WebhookBridge) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", b.url, nil)
	if err != nil {
		return fmt.Errorf("webhook: create health check request: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: health check failed: %w", err)
	}
	defer resp.Body.Close()

	// Accept any 2xx-4xx status as healthy (5xx indicates server error)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("webhook: endpoint unhealthy (status %d)", resp.StatusCode)
	}

	return nil
}

// Close closes the HTTP client.
//
// This method closes idle connections to release resources. It is safe to call
// multiple times.
func (b *WebhookBridge) Close() error {
	b.client.CloseIdleConnections()
	return nil
}
