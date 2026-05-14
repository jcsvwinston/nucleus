package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// key32 is a deterministic 32-byte AES-256 key for tests. It is plain
// ASCII and NOT uniformly random — fine for exercising the code paths in
// a unit test, never acceptable as a production key.
const key32 = "0123456789abcdef0123456789abcdef"

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// ---------- EncryptionKey validation (ADR-006) ----------

func TestNewCSRFMiddleware_NoKeyRequiredWhenXSRFCookieDisabled(t *testing.T) {
	// The common case: EnableXSRFCookie is false, EncryptionKey is unused
	// and must not be required.
	mw, err := NewCSRFMiddleware(CSRFOptions{})
	if err != nil {
		t.Fatalf("NewCSRFMiddleware with XSRF cookie disabled should not require a key: %v", err)
	}
	if mw == nil {
		t.Fatal("expected a non-nil middleware")
	}
}

func TestNewCSRFMiddleware_ValidKeyAccepted(t *testing.T) {
	mw, err := NewCSRFMiddleware(CSRFOptions{
		EnableXSRFCookie: true,
		EncryptionKey:    key32,
	})
	if err != nil {
		t.Fatalf("NewCSRFMiddleware with a 32-byte key should succeed: %v", err)
	}
	if mw == nil {
		t.Fatal("expected a non-nil middleware")
	}
}

func TestNewCSRFMiddleware_RejectsMissingKey(t *testing.T) {
	_, err := NewCSRFMiddleware(CSRFOptions{EnableXSRFCookie: true})
	if err == nil {
		t.Fatal("NewCSRFMiddleware must reject EnableXSRFCookie with no EncryptionKey")
	}
	if !errors.Is(err, ErrCSRFEncryptionKey) {
		t.Fatalf("expected ErrCSRFEncryptionKey, got %v", err)
	}
}

func TestNewCSRFMiddleware_RejectsShortKey(t *testing.T) {
	_, err := NewCSRFMiddleware(CSRFOptions{
		EnableXSRFCookie: true,
		EncryptionKey:    "too-short",
	})
	if err == nil || !errors.Is(err, ErrCSRFEncryptionKey) {
		t.Fatalf("expected ErrCSRFEncryptionKey for a short key, got %v", err)
	}
}

func TestNewCSRFMiddleware_RejectsLongKey(t *testing.T) {
	_, err := NewCSRFMiddleware(CSRFOptions{
		EnableXSRFCookie: true,
		EncryptionKey:    key32 + key32, // 64 bytes
	})
	if err == nil || !errors.Is(err, ErrCSRFEncryptionKey) {
		t.Fatalf("expected ErrCSRFEncryptionKey for a long key, got %v", err)
	}
}

// ---------- CSRFMiddleware panic-on-misconfiguration (ADR-006) ----------

func TestCSRFMiddleware_PanicsOnMissingKey(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("CSRFMiddleware must panic when EnableXSRFCookie is set without a key")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "EncryptionKey") {
			t.Fatalf("panic message should mention EncryptionKey, got %v", r)
		}
	}()
	_ = CSRFMiddleware(CSRFOptions{EnableXSRFCookie: true})
}

func TestCSRFMiddleware_DoesNotPanicForValidConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CSRFMiddleware panicked on a valid config: %v", r)
		}
	}()
	_ = CSRFMiddleware(CSRFOptions{EnableXSRFCookie: true, EncryptionKey: key32})
	_ = CSRFMiddleware(CSRFOptions{}) // XSRF cookie disabled — also fine
}

// ---------- defaults() no longer derives a weak key ----------

func TestCSRFOptions_DefaultsDoesNotDeriveEncryptionKey(t *testing.T) {
	var o CSRFOptions
	o.defaults()
	if o.EncryptionKey != "" {
		t.Fatalf("defaults() must not populate EncryptionKey (weak-key derivation removed); got %q", o.EncryptionKey)
	}
	// The other defaults still apply.
	if o.CookieName != "_csrf" || o.HeaderName != "X-CSRF-Token" {
		t.Fatalf("defaults() should still set the non-secret defaults: %+v", o)
	}
}

// ---------- constant-time comparison behaviour ----------

// TestCSRFMiddleware_RejectsSameLengthWrongToken exercises the
// ConstantTimeCompare path: a token that is the correct length but wrong
// content must still be rejected (ConstantTimeCompare returns 0), and the
// correct token must still be accepted (returns 1).
func TestCSRFMiddleware_ConstantTimeCompareAcceptsAndRejects(t *testing.T) {
	mw := CSRFMiddleware(CSRFOptions{})
	handler := mw(okHandler())

	// First a GET to obtain a real token cookie.
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var realToken string
	for _, c := range getRec.Result().Cookies() {
		if c.Name == "_csrf" {
			realToken = c.Value
		}
	}
	if realToken == "" {
		t.Fatal("expected a _csrf cookie from the GET")
	}

	// Correct token → accepted.
	{
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.AddCookie(&http.Cookie{Name: "_csrf", Value: realToken})
		req.Header.Set("X-CSRF-Token", realToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("correct token should be accepted, got %d", rec.Code)
		}
	}

	// Same-length wrong token → rejected. (Hex token, flip the first char.)
	{
		wrong := flipFirstHex(realToken)
		if len(wrong) != len(realToken) {
			t.Fatal("test bug: wrong token must be the same length")
		}
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.AddCookie(&http.Cookie{Name: "_csrf", Value: realToken})
		req.Header.Set("X-CSRF-Token", wrong)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatal("same-length wrong token must be rejected")
		}
	}

	// Different-length token → rejected (ConstantTimeCompare returns 0 for
	// length mismatch).
	{
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.AddCookie(&http.Cookie{Name: "_csrf", Value: realToken})
		req.Header.Set("X-CSRF-Token", realToken+"extra")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatal("different-length token must be rejected")
		}
	}
}

func flipFirstHex(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

// ---------- XSRF cookie round-trip with a real key ----------

func TestCSRFMiddleware_XSRFCookieRoundTrip(t *testing.T) {
	mw := CSRFMiddleware(CSRFOptions{EnableXSRFCookie: true, EncryptionKey: key32})
	handler := mw(okHandler())

	// GET issues both the _csrf cookie and the encrypted XSRF-TOKEN cookie.
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/", nil))

	var csrfToken, xsrfEncrypted string
	for _, c := range getRec.Result().Cookies() {
		switch c.Name {
		case "_csrf":
			csrfToken = c.Value
		case "XSRF-TOKEN":
			xsrfEncrypted = c.Value
		}
	}
	if csrfToken == "" || xsrfEncrypted == "" {
		t.Fatalf("expected both _csrf and XSRF-TOKEN cookies, got _csrf=%q XSRF-TOKEN=%q", csrfToken, xsrfEncrypted)
	}
	if xsrfEncrypted == csrfToken {
		t.Fatal("XSRF-TOKEN cookie should be encrypted, not the raw token")
	}

	// POST with the encrypted XSRF-TOKEN header must decrypt and validate.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: "_csrf", Value: csrfToken})
	req.Header.Set("X-XSRF-TOKEN", xsrfEncrypted)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypted XSRF-TOKEN round-trip should be accepted, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestCSRFMiddleware_TamperedXSRFHeaderRejected confirms that a
// corrupted X-XSRF-TOKEN header is rejected — not accepted, and not a
// panic. A decrypt failure must leave `submitted` empty so the request
// falls through to rejection by the constant-time compare.
func TestCSRFMiddleware_TamperedXSRFHeaderRejected(t *testing.T) {
	mw := CSRFMiddleware(CSRFOptions{EnableXSRFCookie: true, EncryptionKey: key32})
	handler := mw(okHandler())

	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var csrfToken, xsrfEncrypted string
	for _, c := range getRec.Result().Cookies() {
		switch c.Name {
		case "_csrf":
			csrfToken = c.Value
		case "XSRF-TOKEN":
			xsrfEncrypted = c.Value
		}
	}
	if csrfToken == "" || xsrfEncrypted == "" {
		t.Fatal("expected _csrf and XSRF-TOKEN cookies from the GET")
	}

	cases := map[string]string{
		"bit-flipped ciphertext": flipFirstHex(xsrfEncrypted),
		"garbage value":          "not-even-base64-!!!",
		"empty after prefix":     "",
		"truncated ciphertext":   xsrfEncrypted[:len(xsrfEncrypted)/2],
	}
	for name, headerVal := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("a tampered X-XSRF-TOKEN must not panic, got: %v", r)
				}
			}()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.AddCookie(&http.Cookie{Name: "_csrf", Value: csrfToken})
			req.Header.Set("X-XSRF-TOKEN", headerVal)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Fatalf("tampered X-XSRF-TOKEN %q must be rejected, got 200", name)
			}
		})
	}
}

// TestCSRFMiddleware_XSRFHeaderIgnoredWhenCookieDisabled confirms that
// when EnableXSRFCookie is false, the X-XSRF-TOKEN header is not read at
// all — a client cannot smuggle a raw value through it.
func TestCSRFMiddleware_XSRFHeaderIgnoredWhenCookieDisabled(t *testing.T) {
	mw := CSRFMiddleware(CSRFOptions{}) // EnableXSRFCookie defaults false
	handler := mw(okHandler())

	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/", nil))
	var csrfToken string
	for _, c := range getRec.Result().Cookies() {
		if c.Name == "_csrf" {
			csrfToken = c.Value
		}
	}

	// Send the real token in the X-XSRF-TOKEN header (the wrong header for
	// this config). It must be ignored, so the POST is rejected.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: "_csrf", Value: csrfToken})
	req.Header.Set("X-XSRF-TOKEN", csrfToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("X-XSRF-TOKEN must be ignored when EnableXSRFCookie is false")
	}

	// The same token in the correct header is accepted — proves the
	// rejection above was about the header choice, not the token.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.AddCookie(&http.Cookie{Name: "_csrf", Value: csrfToken})
	req2.Header.Set("X-CSRF-Token", csrfToken)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("correct X-CSRF-Token header should be accepted, got %d", rec2.Code)
	}
}

// ---------- encrypt/decrypt defensive behaviour ----------

func TestEncryptDecryptToken_RoundTrip(t *testing.T) {
	enc, err := encryptToken("hello-token", key32)
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}
	dec, err := decryptToken(enc, key32)
	if err != nil {
		t.Fatalf("decryptToken: %v", err)
	}
	if dec != "hello-token" {
		t.Fatalf("round-trip mismatch: got %q", dec)
	}
}

func TestEncryptToken_ShortKeyReturnsErrorNotPanic(t *testing.T) {
	// Previously encryptToken did key[:32], which panics on a short key.
	// It must now return an error from aes.NewCipher instead.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("encryptToken must not panic on a short key, got panic: %v", r)
		}
	}()
	if _, err := encryptToken("x", "short"); err == nil {
		t.Fatal("encryptToken should return an error for a non-32-byte key")
	}
}

func TestDecryptToken_ShortCiphertextReturnsError(t *testing.T) {
	// A ciphertext shorter than the GCM nonce previously decrypted to ""
	// with a nil error (latent bug). It must now return a real error.
	_, err := decryptToken("AAAA", key32) // valid base64, far too short
	if err == nil {
		t.Fatal("decryptToken must return an error for a too-short ciphertext")
	}
}
