package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

type jwtCtxKey struct{}

// Claims holds the JWT payload with user identity information.
type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// SigningAlgorithm enumerates the JWT signing algorithms this package
// supports. New algorithms (e.g. ES256) can be added without changing
// the public API — extend the switch in signingMethod / verifyMaterial.
type SigningAlgorithm string

const (
	HS256 SigningAlgorithm = "HS256"
	RS256 SigningAlgorithm = "RS256"
	// ES256 is ECDSA with the NIST P-256 curve and SHA-256. Only P-256
	// is supported — P-384 (ES384) and P-521 (ES512) are deliberately
	// out of scope until there is a concrete need; see ADR-005.
	ES256 SigningAlgorithm = "ES256"
)

// SigningKey is one entry in a JWTManager's keyset. Exactly one of the
// material fields must be set, matching the Algorithm.
type SigningKey struct {
	KID          string
	Algorithm    SigningAlgorithm
	HMACSecret   []byte            // HS256
	RSAPrivate   *rsa.PrivateKey   // RS256
	ECDSAPrivate *ecdsa.PrivateKey // ES256 (P-256 only)
}

func (k *SigningKey) validate() error {
	if k == nil {
		return errors.New("nil SigningKey")
	}
	if strings.TrimSpace(k.KID) == "" {
		return errors.New("SigningKey.KID is required")
	}
	switch k.Algorithm {
	case HS256:
		if len(k.HMACSecret) == 0 {
			return fmt.Errorf("SigningKey %q: HS256 requires HMACSecret", k.KID)
		}
	case RS256:
		if k.RSAPrivate == nil {
			return fmt.Errorf("SigningKey %q: RS256 requires RSAPrivate", k.KID)
		}
	case ES256:
		if k.ECDSAPrivate == nil {
			return fmt.Errorf("SigningKey %q: ES256 requires ECDSAPrivate", k.KID)
		}
		if k.ECDSAPrivate.Curve != elliptic.P256() {
			return fmt.Errorf("SigningKey %q: ES256 requires the P-256 curve, got %s", k.KID, k.ECDSAPrivate.Curve.Params().Name)
		}
	default:
		return fmt.Errorf("SigningKey %q: unsupported algorithm %q", k.KID, k.Algorithm)
	}
	return nil
}

func (k *SigningKey) signingMethod() jwt.SigningMethod {
	switch k.Algorithm {
	case HS256:
		return jwt.SigningMethodHS256
	case RS256:
		return jwt.SigningMethodRS256
	case ES256:
		return jwt.SigningMethodES256
	}
	return nil
}

func (k *SigningKey) signMaterial() any {
	switch k.Algorithm {
	case HS256:
		return k.HMACSecret
	case RS256:
		return k.RSAPrivate
	case ES256:
		return k.ECDSAPrivate
	}
	return nil
}

func (k *SigningKey) verifyMaterial() any {
	switch k.Algorithm {
	case HS256:
		return k.HMACSecret
	case RS256:
		return &k.RSAPrivate.PublicKey
	case ES256:
		return &k.ECDSAPrivate.PublicKey
	}
	return nil
}

// JWTManager handles JWT token generation and validation. It supports
// two modes that coexist:
//
//   - Legacy single-secret HS256: built via NewJWTManager. Tokens have
//     no "kid" header; Validate falls back to the single secret when
//     the incoming token lacks a kid.
//   - Multi-key with rotation: built via NewJWTManagerFromKeys or by
//     calling RotateKey on an existing manager. Tokens carry a "kid"
//     header; Validate looks up the key by kid. Adding a new key keeps
//     existing tokens valid until they expire — the operator path for
//     zero-downtime rotation.
//
// JWKSHandler exposes the asymmetric portion of the keyset over an
// RFC 7517 / RFC 7518 JSON shape; HMAC keys are intentionally omitted
// so the published endpoint cannot leak shared secrets.
type JWTManager struct {
	mu sync.RWMutex

	legacySecret []byte

	keys       map[string]*SigningKey
	currentKID string

	expiry time.Duration
	issuer string
}

// NewJWTManager creates a single-secret HS256 manager. Backwards
// compatible: tokens carry no "kid" header and Validate uses the single
// secret. Use NewJWTManagerFromKeys (or RotateKey on the returned
// manager) to opt into rotation.
func NewJWTManager(secret string, expiry time.Duration, issuer ...string) *JWTManager {
	iss := "nucleus"
	if len(issuer) > 0 && issuer[0] != "" {
		iss = issuer[0]
	}
	return &JWTManager{
		legacySecret: []byte(secret),
		keys:         make(map[string]*SigningKey),
		expiry:       expiry,
		issuer:       iss,
	}
}

// NewJWTManagerFromKeys creates a manager that signs with the key
// identified by currentKID and accepts tokens signed by any key in
// keys. At least one key is required; currentKID must match one of
// them.
func NewJWTManagerFromKeys(keys []SigningKey, currentKID string, expiry time.Duration, issuer ...string) (*JWTManager, error) {
	if len(keys) == 0 {
		return nil, errors.New("auth.NewJWTManagerFromKeys: at least one key is required")
	}
	iss := "nucleus"
	if len(issuer) > 0 && issuer[0] != "" {
		iss = issuer[0]
	}

	m := &JWTManager{
		keys:   make(map[string]*SigningKey, len(keys)),
		expiry: expiry,
		issuer: iss,
	}
	for i := range keys {
		key := keys[i]
		if err := key.validate(); err != nil {
			return nil, fmt.Errorf("auth.NewJWTManagerFromKeys: %w", err)
		}
		if _, exists := m.keys[key.KID]; exists {
			return nil, fmt.Errorf("auth.NewJWTManagerFromKeys: duplicate kid %q", key.KID)
		}
		m.keys[key.KID] = &key
	}
	if _, ok := m.keys[currentKID]; !ok {
		return nil, fmt.Errorf("auth.NewJWTManagerFromKeys: currentKID %q not found in keys", currentKID)
	}
	m.currentKID = currentKID
	return m, nil
}

// RotateKey adds a key to the verification set. When makeCurrent is
// true, future Generate calls use this key as the signing key. Existing
// tokens (signed with the previous current key) remain valid as long as
// that key stays in the set. Operators are responsible for removing
// keys with RemoveKey after the access-token lifetime expires.
func (m *JWTManager) RotateKey(key SigningKey, makeCurrent bool) error {
	if err := key.validate(); err != nil {
		return fmt.Errorf("auth.JWTManager.RotateKey: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.keys[key.KID]; exists {
		return fmt.Errorf("auth.JWTManager.RotateKey: kid %q already present", key.KID)
	}
	m.keys[key.KID] = &key
	if makeCurrent {
		m.currentKID = key.KID
	}
	return nil
}

// RemoveKey drops a key from the verification set. Tokens signed with
// that kid will be rejected on the next Validate call. The current
// signing key cannot be removed; promote a different key with RotateKey
// first.
func (m *JWTManager) RemoveKey(kid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kid == m.currentKID {
		return fmt.Errorf("auth.JWTManager.RemoveKey: cannot remove the current signing key %q", kid)
	}
	if _, ok := m.keys[kid]; !ok {
		return fmt.Errorf("auth.JWTManager.RemoveKey: kid %q not found", kid)
	}
	delete(m.keys, kid)
	return nil
}

// CurrentKID returns the kid that Generate will stamp into new tokens,
// or empty when the manager is in legacy single-secret mode.
func (m *JWTManager) CurrentKID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentKID
}

// Generate creates a signed JWT token for the given user claims. In
// multi-key mode the current key's algorithm is used and its kid is
// stamped into the token header. In legacy mode HS256 is used and the
// header carries no kid.
func (m *JWTManager) Generate(userID, username, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiry)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	m.mu.RLock()
	current := m.currentKID
	var signingKey *SigningKey
	if current != "" {
		signingKey = m.keys[current]
	}
	legacy := m.legacySecret
	m.mu.RUnlock()

	if signingKey != nil {
		token := jwt.NewWithClaims(signingKey.signingMethod(), claims)
		token.Header["kid"] = signingKey.KID
		signed, err := token.SignedString(signingKey.signMaterial())
		if err != nil {
			return "", fmt.Errorf("auth.JWTManager.Generate: %w", err)
		}
		return signed, nil
	}

	if len(legacy) == 0 {
		return "", errors.New("auth.JWTManager.Generate: no signing key configured")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(legacy)
	if err != nil {
		return "", fmt.Errorf("auth.JWTManager.Generate: %w", err)
	}
	return signed, nil
}

// Validate parses and validates a JWT token string. Returns the claims
// if valid.
//
// Resolution order:
//
//  1. If the token header carries a "kid" matching a key in the
//     verification set, that key is used.
//  2. Otherwise, if the manager has a legacy single secret, it is used
//     (HS256 only).
//  3. Otherwise, the token is rejected — a multi-key manager will not
//     accept a token without a kid.
func (m *JWTManager) Validate(tokenString string) (*Claims, error) {
	keyfunc := func(t *jwt.Token) (any, error) {
		m.mu.RLock()
		defer m.mu.RUnlock()

		kid, _ := t.Header["kid"].(string)
		if kid != "" {
			key, ok := m.keys[kid]
			if !ok {
				return nil, fmt.Errorf("unknown kid %q", kid)
			}
			if t.Method != key.signingMethod() {
				return nil, fmt.Errorf("token alg %q does not match kid %q (expected %q)", t.Method.Alg(), kid, key.signingMethod().Alg())
			}
			return key.verifyMaterial(), nil
		}

		// No kid: legacy path. Multi-key managers reject.
		if len(m.legacySecret) == 0 {
			return nil, errors.New("token has no kid and no legacy secret is configured")
		}
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("legacy validation expects HS256, got %q", t.Method.Alg())
		}
		return m.legacySecret, nil
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, keyfunc)
	if err != nil {
		return nil, fmt.Errorf("auth.JWTManager.Validate: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("auth.JWTManager.Validate: invalid token claims")
	}
	return claims, nil
}

// Middleware returns an HTTP middleware that extracts and validates the JWT token
// from the Authorization header (Bearer scheme). On success, the claims are stored
// in the request context and can be retrieved via ClaimsFromContext.
func (m *JWTManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				gferrors.WriteError(w, r, gferrors.Unauthorized("missing authorization header"), nil)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				gferrors.WriteError(w, r, gferrors.Unauthorized("invalid authorization format, expected: Bearer <token>"), nil)
				return
			}

			claims, err := m.Validate(parts[1])
			if err != nil {
				gferrors.WriteError(w, r, gferrors.Unauthorized("invalid or expired token"), nil)
				return
			}

			next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
		})
	}
}

// ClaimsFromContext extracts JWT claims from the request context.
// Returns nil, false if no claims are present.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(jwtCtxKey{}).(*Claims)
	return claims, ok
}

// ContextWithClaims returns a copy of ctx carrying the given claims —
// exactly what JWTManager.Middleware stores after validating a bearer
// token, including the observability user-id propagation for log
// attribution. It is the bridge for applications that authenticate by
// other means — typically a server-side session — and still need the
// authorization layer (Enforcer.Middleware, Enforcer.RequireRole) to see
// the request's subject: load the session, build a *Claims carrying the
// subject and role, and wrap the request context in a middleware that
// runs before those checks. A nil claims returns ctx unchanged.
//
// Two caveats. Claims values are trusted as-is — build them only from
// server-side state (a session), never from request-supplied input. And
// an empty claims.UserID still overwrites the observability user-id slot
// with ""; pass a non-empty UserID when injecting a real subject.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	if claims == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, jwtCtxKey{}, claims)
	return observe.CtxWithUserID(ctx, claims.UserID)
}

// OptionalJWTMiddleware is like Middleware but does not reject requests without
// a token. If a valid token is present, claims are added to the context.
// If no token or an invalid token is present, the request proceeds without claims.
func (m *JWTManager) OptionalJWTMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
					if claims, err := m.Validate(parts[1]); err == nil {
						r = r.WithContext(ContextWithClaims(r.Context(), claims))
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// JWKSHandler returns an HTTP handler that serves the manager's public
// key set in JWK Set format (RFC 7517 / RFC 7518). Only asymmetric keys
// are published — HMAC keys are intentionally excluded so the endpoint
// cannot leak shared secrets.
//
// Mount the handler at the canonical path (`/.well-known/jwks.json`)
// or wherever your deployment expects relying parties to discover it.
func (m *JWTManager) JWKSHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		set := m.JWKS()
		w.Header().Set("Content-Type", "application/jwk-set+json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(set)
	}
}

// JWKS returns the public key set in the canonical JWK Set shape. Use
// JWKSHandler for a ready-to-mount HTTP handler; JWKS itself is exposed
// for callers that need the raw structure (e.g. embedding in OIDC
// discovery responses).
func (m *JWTManager) JWKS() JWKSet {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]JWK, 0, len(m.keys))
	for _, key := range m.keys {
		jwk, ok := toJWK(key)
		if !ok {
			continue
		}
		keys = append(keys, jwk)
	}
	return JWKSet{Keys: keys}
}

// JWKSet is the wire shape of an RFC 7517 JSON Web Key Set.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// JWK is the wire shape of an RFC 7517 JSON Web Key. The Use field is
// always "sig" for keys produced by this package; HMAC keys are not
// emitted, so the kty field is "RSA" (RS256) or "EC" (ES256).
//
// RSA keys populate N and E; EC keys populate Crv, X and Y. The omitempty
// tags keep each emitted key minimal — an EC key carries no n/e and an
// RSA key carries no crv/x/y.
type JWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

func toJWK(key *SigningKey) (JWK, bool) {
	switch key.Algorithm {
	case RS256:
		pub := &key.RSAPrivate.PublicKey
		return JWK{
			Kid: key.KID,
			Kty: "RSA",
			Alg: "RS256",
			Use: "sig",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}, true
	case ES256:
		pub := &key.ECDSAPrivate.PublicKey
		// RFC 7518 §6.2.1.2/3: the x and y coordinates are the
		// fixed-length big-endian representation for the curve, NOT the
		// minimal-length output of big.Int.Bytes(). For P-256 that is
		// 32 bytes each, left-padded with zeros.
		size := (pub.Curve.Params().BitSize + 7) / 8
		return JWK{
			Kid: key.KID,
			Kty: "EC",
			Alg: "ES256",
			Use: "sig",
			Crv: "P-256",
			X:   base64.RawURLEncoding.EncodeToString(leftPad(pub.X.Bytes(), size)),
			Y:   base64.RawURLEncoding.EncodeToString(leftPad(pub.Y.Bytes(), size)),
		}, true
	default:
		// HS* keys are NOT published — exposing the bytes would leak
		// the shared secret.
		return JWK{}, false
	}
}

// leftPad returns b left-padded with zero bytes to exactly size bytes.
// If b is already >= size it is returned unchanged (callers pass
// coordinates that never exceed the curve size, so the truncation case
// does not arise in practice).
func leftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}
