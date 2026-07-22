package outbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// hmacHex returns the hex-encoded HMAC-SHA256 of material under secret — the
// digest half of a "sha256=<hex>" signature, used by the SEC-3 tests to show
// what the bridge did and did not sign.
func hmacHex(secret string, material []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(material)
	return hex.EncodeToString(mac.Sum(nil))
}

// This file is the BODY contract of the webhook bridge: the exact bytes on
// the wire, per payload-encoding variant, pinned against checked-in fixtures
// in testdata/. The API contract freeze only sees exported symbols — a wire
// format can change without touching any symbol (that is exactly how the
// v1.4.0 base64 shape flipped to embedded JSON without any gate noticing) —
// so this test is the gate for the wire itself. Comparison is byte-for-byte:
// any drift fails naming the first diverging byte. Non-deterministic inputs
// (times, ids) are fixed values injected by the test, never regex-relaxed.

// contractTime is the fixed timestamp all fixture bodies carry.
var contractTime = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

// contractMessage returns the deterministic message the fixtures serialize.
func contractMessage(payload []byte) Message {
	return Message{
		ID:          "msg-fixture-1",
		Topic:       "orders.placed",
		Payload:     payload,
		Status:      StatusPending,
		Attempts:    1,
		AvailableAt: contractTime,
		CreatedAt:   contractTime,
	}
}

// captureWebhookFull records the last request's body AND headers.
func captureWebhookFull(t *testing.T) (*httptest.Server, func() ([]byte, http.Header)) {
	t.Helper()
	var (
		body    []byte
		headers http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := readAll(r)
		if err != nil {
			t.Errorf("read webhook body: %v", err)
		}
		body = b
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, func() ([]byte, http.Header) { return body, headers }
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

// assertBytesEqual compares got to want byte-for-byte and fails naming the
// first diverging offset with context around it.
func assertBytesEqual(t *testing.T, fixture string, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	window := func(b []byte) string {
		lo := i - 20
		if lo < 0 {
			lo = 0
		}
		hi := i + 20
		if hi > len(b) {
			hi = len(b)
		}
		at := "<end of body>"
		if i < len(b) {
			at = fmt.Sprintf("0x%02x %q", b[i], string(b[i]))
		}
		return fmt.Sprintf("byte %s, context %q", at, string(b[lo:hi]))
	}
	t.Fatalf("webhook body diverges from fixture %s at byte offset %d:\n  got  (%d bytes): %s\n  want (%d bytes): %s",
		fixture, i, len(got), window(got), len(want), window(want))
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestWebhookBodyContract(t *testing.T) {
	jsonPayload := []byte(`{"customer_email":"ada@example.test","order_id":42,"quantity":2}`)
	rawPayload := []byte{0xff, 0xfe, 0x00, 0x01}
	const secret = "contract-secret"

	cases := []struct {
		name         string
		fixture      string
		encoding     string // WebhookConfig.PayloadEncoding ("" = default)
		secret       string
		payload      []byte
		wantEncoding string // declared X-Outbox-Payload-Encoding
		wantSigned   bool
	}{
		{
			// The DEFAULT wire: byte-identical to the v1.4.0 release, with
			// the (additive) encoding header declaring it.
			name:         "default_base64_unsigned",
			fixture:      "webhook_body_base64.json",
			encoding:     "",
			payload:      jsonPayload,
			wantEncoding: PayloadEncodingBase64,
		},
		{
			name:         "default_base64_signed",
			fixture:      "webhook_body_base64.json",
			encoding:     PayloadEncodingBase64,
			secret:       secret,
			payload:      jsonPayload,
			wantEncoding: PayloadEncodingBase64,
			wantSigned:   true,
		},
		{
			name:         "json_optin_unsigned",
			fixture:      "webhook_body_json.json",
			encoding:     PayloadEncodingJSON,
			payload:      jsonPayload,
			wantEncoding: PayloadEncodingJSON,
		},
		{
			name:         "json_optin_signed",
			fixture:      "webhook_body_json.json",
			encoding:     PayloadEncodingJSON,
			secret:       secret,
			payload:      jsonPayload,
			wantEncoding: PayloadEncodingJSON,
			wantSigned:   true,
		},
		{
			// json opt-in with a payload that is not valid JSON: the body
			// keeps the base64 form and the header declares base64.
			name:         "json_optin_nonjson_fallback",
			fixture:      "webhook_body_json_nonjson_fallback.json",
			encoding:     PayloadEncodingJSON,
			payload:      rawPayload,
			wantEncoding: PayloadEncodingBase64,
		},
		{
			name:         "json_optin_no_payload_null",
			fixture:      "webhook_body_no_payload_null.json",
			encoding:     PayloadEncodingJSON,
			payload:      nil,
			wantEncoding: PayloadEncodingJSON,
		},
		{
			// Default mode with no payload: null too — exactly what v1.4.0
			// emitted for a nil []byte.
			name:         "default_base64_no_payload_null",
			fixture:      "webhook_body_no_payload_null.json",
			encoding:     "",
			payload:      nil,
			wantEncoding: PayloadEncodingBase64,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, received := captureWebhookFull(t)
			bridge, err := NewWebhookBridge(WebhookConfig{
				Name:            "contract",
				URL:             srv.URL,
				PayloadEncoding: tc.encoding,
				Secret:          tc.secret,
			})
			if err != nil {
				t.Fatalf("create webhook bridge: %v", err)
			}
			defer bridge.Close()

			if err := bridge.Send(context.Background(), contractMessage(tc.payload)); err != nil {
				t.Fatalf("Send: %v", err)
			}

			body, headers := received()
			want := readFixture(t, tc.fixture)

			// The body, byte for byte.
			assertBytesEqual(t, tc.fixture, body, want)

			// Headers asserted apart from the body. The encoding header is
			// ALWAYS present and declares the actual shape of this delivery.
			if got := headers.Get(WebhookPayloadEncodingHeader); got != tc.wantEncoding {
				t.Fatalf("%s = %q, want %q", WebhookPayloadEncodingHeader, got, tc.wantEncoding)
			}

			sig := headers.Get(WebhookSignatureHeader)
			if !tc.wantSigned {
				if sig != "" {
					t.Fatalf("unsigned bridge sent %s = %q", WebhookSignatureHeader, sig)
				}
				return
			}
			// The signature must be the HMAC-SHA256 of the FIXTURE bytes
			// (computed here independently of the bridge), in the module-
			// webhook format "sha256=<hex>".
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(want)
			if wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil)); sig != wantSig {
				t.Fatalf("%s = %q, want %q", WebhookSignatureHeader, sig, wantSig)
			}
		})
	}
}

// TestWebhookBridge_RejectsUnknownPayloadEncoding pins the config contract:
// payload_encoding accepts only "base64", "json" or empty.
func TestWebhookBridge_RejectsUnknownPayloadEncoding(t *testing.T) {
	_, err := NewWebhookBridge(WebhookConfig{Name: "bad", URL: "http://localhost", PayloadEncoding: "hex"})
	if err == nil {
		t.Fatal("expected error for payload_encoding \"hex\"")
	}
}

// TestWebhookPayloadEncodingHeader_NotInSignedMaterial pins the SEC-3
// decision (Option 2: freeze encoding per bridge, keep the header
// informational). The bridge signs the body ALONE — byte-for-byte the
// module-webhook scheme (nucleus.SignWebhookBody), the property that lets one
// verifier serve both surfaces and that
// TestOutboxBridgeSignature_MatchesModuleWebhookScheme (pkg/nucleus) pins.
// This test proves the X-Outbox-Payload-Encoding header is NOT folded into
// the signature: flipping it in transit leaves the body signature valid, so a
// consumer must not trust it — it decodes by config and rejects a mismatch
// with CheckPayloadEncoding.
func TestWebhookPayloadEncodingHeader_NotInSignedMaterial(t *testing.T) {
	const secret = "contract-secret"
	srv, received := captureWebhookFull(t)
	bridge, err := NewWebhookBridge(WebhookConfig{Name: "contract", URL: srv.URL, Secret: secret})
	if err != nil {
		t.Fatalf("create webhook bridge: %v", err)
	}
	defer bridge.Close()

	if err := bridge.Send(context.Background(), contractMessage([]byte(`{"order_id":42}`))); err != nil {
		t.Fatalf("Send: %v", err)
	}
	body, headers := received()

	sig := headers.Get(WebhookSignatureHeader)
	encHeader := headers.Get(WebhookPayloadEncodingHeader)
	if sig == "" || encHeader == "" {
		t.Fatalf("want both signature and encoding headers, got sig=%q enc=%q", sig, encHeader)
	}

	// The signature is the HMAC of the body alone; the encoding header is not
	// mixed in. (Had SEC-3 chosen Option 1, sig would equal the header+body
	// HMAC instead — this asserts it does NOT, so the module-webhook scheme is
	// intact.)
	if want := "sha256=" + hmacHex(secret, body); sig != want {
		t.Fatalf("signature %q is not the body-only HMAC %q", sig, want)
	}
	if forked := "sha256=" + hmacHex(secret, append([]byte(encHeader), body...)); sig == forked {
		t.Fatal("signature must NOT cover the encoding header (that would fork the module-webhook scheme)")
	}

	// Consequence: an attacker flipping the header leaves the body signature
	// valid. The header cannot be a security input; the consumer rejects the
	// mismatch with CheckPayloadEncoding instead.
	flipped := PayloadEncodingJSON
	if encHeader == PayloadEncodingJSON {
		flipped = PayloadEncodingBase64
	}
	if err := CheckPayloadEncoding(encHeader, flipped); !errors.Is(err, ErrPayloadEncodingMismatch) {
		t.Fatalf("CheckPayloadEncoding must reject a flipped header with ErrPayloadEncodingMismatch, got %v", err)
	}
}

// TestCheckPayloadEncoding covers the SEC-3 mismatch-rejection helper: matches
// (including normalization and the empty==base64 default) pass; a mismatch is
// the tampered/wrong-encoding case and is rejected with
// ErrPayloadEncodingMismatch.
func TestCheckPayloadEncoding(t *testing.T) {
	cases := []struct {
		expected, delivered string
		wantErr             bool
	}{
		{PayloadEncodingBase64, PayloadEncodingBase64, false},
		{PayloadEncodingJSON, PayloadEncodingJSON, false},
		{"", PayloadEncodingBase64, false},        // empty expected == base64 default
		{PayloadEncodingBase64, "", false},        // empty delivered == base64 default
		{"", "", false},                           // both default to base64
		{" JSON ", "json", false},                 // trimmed + case-folded
		{PayloadEncodingBase64, PayloadEncodingJSON, true}, // mismatch (e.g. header flipped in transit)
		{PayloadEncodingJSON, PayloadEncodingBase64, true},
	}
	for _, tc := range cases {
		err := CheckPayloadEncoding(tc.expected, tc.delivered)
		if tc.wantErr {
			if !errors.Is(err, ErrPayloadEncodingMismatch) {
				t.Errorf("CheckPayloadEncoding(%q,%q) = %v, want ErrPayloadEncodingMismatch", tc.expected, tc.delivered, err)
			}
		} else if err != nil {
			t.Errorf("CheckPayloadEncoding(%q,%q) = %v, want nil", tc.expected, tc.delivered, err)
		}
	}
}
