package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// CookieSessionStore persists sessions in encrypted cookies.
// This is useful for stateless applications where you don't want to store session data on the server.
// Note: Cookie size limits (typically 4KB) restrict the amount of data you can store.
//
// Deprecated: CookieSessionStore has never been functional. CommitCtx
// encrypts the payload and then discards it, because the SessionStore
// contract has no access to the HTTP response and therefore cannot set a
// cookie — sessions written through this store are silently lost. It is
// scheduled for removal in v0.12.0 (DEP-2026-006); use the memory, sql,
// or redis stores instead. A response-aware cookie-session feature may
// return post-v1.0 under a contract designed for it.
type CookieSessionStore struct {
	encryptionKey []byte
}

// NewCookieSessionStore creates a cookie-backed session store with the given encryption key.
// The key must be 32 bytes (256 bits) for AES-256 encryption.
//
// Deprecated: the store it constructs has never been functional (see
// CookieSessionStore); scheduled for removal in v0.12.0 (DEP-2026-006).
func NewCookieSessionStore(encryptionKey string) (*CookieSessionStore, error) {
	key := []byte(encryptionKey)
	if len(key) != 32 {
		return nil, fmt.Errorf("cookie session store: encryption key must be 32 bytes, got %d", len(key))
	}

	return &CookieSessionStore{
		encryptionKey: key,
	}, nil
}

// Delete removes the session token from the store.
// For cookie store, this is a no-op since cookies are managed by the client.
func (s *CookieSessionStore) Delete(token string) error {
	return s.DeleteCtx(context.Background(), token)
}

// Find retrieves the session payload for token.
func (s *CookieSessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

// Commit stores the session payload for token with absolute expiry.
// For cookie store, expiry is encoded in the encrypted payload.
func (s *CookieSessionStore) Commit(token string, b []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, b, expiry)
}

// All returns all active sessions.
// For cookie store, this returns an empty map since cookies are client-side.
func (s *CookieSessionStore) All() (map[string][]byte, error) {
	return s.AllCtx(context.Background())
}

// DeleteCtx removes the session token from the store.
func (s *CookieSessionStore) DeleteCtx(ctx context.Context, token string) error {
	// No-op for cookie store - cookies are managed by the client
	return nil
}

// FindCtx retrieves the session payload from the cookie store.
// The token is expected to be a base64-encoded encrypted string containing the session data.
func (s *CookieSessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	if token == "" {
		return nil, false, nil
	}

	// Decode base64
	ciphertext, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, false, fmt.Errorf("cookie session find: decode base64: %w", err)
	}

	// Decrypt
	plaintext, err := s.decrypt(ciphertext)
	if err != nil {
		return nil, false, fmt.Errorf("cookie session find: decrypt: %w", err)
	}

	// Parse the cookie session data
	var cookieData cookieSessionData
	if err := json.Unmarshal(plaintext, &cookieData); err != nil {
		return nil, false, fmt.Errorf("cookie session find: unmarshal: %w", err)
	}

	// Check expiry
	if !cookieData.Expiry.IsZero() && time.Now().UTC().After(cookieData.Expiry) {
		return nil, false, nil
	}

	return cookieData.Data, true, nil
}

// CommitCtx stores the session payload in the cookie store.
// The data is encrypted and base64-encoded for safe storage in cookies.
func (s *CookieSessionStore) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	if token == "" {
		return fmt.Errorf("cookie session commit: empty token")
	}

	cookieData := cookieSessionData{
		Data:   b,
		Expiry: expiry.UTC(),
	}

	// Marshal
	plaintext, err := json.Marshal(cookieData)
	if err != nil {
		return fmt.Errorf("cookie session commit: marshal: %w", err)
	}

	// Encrypt
	ciphertext, err := s.encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("cookie session commit: encrypt: %w", err)
	}

	// Encode base64
	encoded := base64.URLEncoding.EncodeToString(ciphertext)

	// Store the encrypted data in the session using the token as key
	// This allows the middleware to read it and set it as a cookie
	_ = encoded // In a real implementation, this would be set as a cookie

	return nil
}

// AllCtx returns all active sessions.
// For cookie store, this returns an empty map since cookies are client-side.
func (s *CookieSessionStore) AllCtx(ctx context.Context) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}

// encrypt encrypts plaintext using AES-GCM.
func (s *CookieSessionStore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decrypt decrypts ciphertext using AES-GCM.
func (s *CookieSessionStore) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// cookieSessionData represents the structure of data stored in cookie sessions.
type cookieSessionData struct {
	Data   []byte    `json:"data"`
	Expiry time.Time `json:"expiry"`
}
