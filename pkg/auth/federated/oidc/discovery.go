package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// discover fetches and caches the provider's metadata.
//
// The document is cached for the life of the process because the endpoints
// in it are effectively static; the KEYS are not, and they are refreshed on
// a miss (see jwks). Caching them the same way is how a provider's key
// rotation turns into an outage that lasts until a restart.
func (p *Provider) discover(ctx context.Context) (*metadata, error) {
	p.mu.RLock()
	cached := p.metadata
	p.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	endpoint := strings.TrimRight(strings.TrimSpace(p.cfg.Issuer), "/") + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: build discovery request: %w", err)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery at %s answered %d", endpoint, response.StatusCode)
	}

	var parsed metadata
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("oidc: decode discovery document: %w", err)
	}
	if parsed.AuthorizationEndpoint == "" || parsed.TokenEndpoint == "" || parsed.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: discovery document from %s is missing an endpoint this flow needs", endpoint)
	}
	// The issuer in the document is what an id_token must claim. A
	// document that disagrees with the URL it was fetched from is either
	// a misconfiguration or a redirect somebody should look at.
	if !p.cfg.SkipIssuerVerification && parsed.Issuer != strings.TrimRight(strings.TrimSpace(p.cfg.Issuer), "/") {
		return nil, fmt.Errorf("oidc: the discovery document claims issuer %q but was fetched from %q (set skip_issuer_verification only if you know why)",
			parsed.Issuer, p.cfg.Issuer)
	}

	p.mu.Lock()
	p.metadata = &parsed
	p.mu.Unlock()
	return &parsed, nil
}

// keySet holds the provider's published verification keys.
type keySet struct {
	mu        sync.RWMutex
	byKID     map[string]any
	fetchedAt time.Time
	jwksURI   string
	// client is the provider's own, so a refresh triggered from the
	// keyfunc keeps its timeout instead of falling back to a default
	// client with none.
	client *http.Client
}

// jwksMinRefresh is how often an unknown kid may trigger a refetch. Without
// it, a token carrying a random kid is a request amplifier pointed at the
// identity provider.
const jwksMinRefresh = time.Minute

// jwks returns the key set, fetching it the first time.
func (p *Provider) jwks(ctx context.Context, uri string) (*keySet, error) {
	p.mu.RLock()
	cached := p.keys
	p.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	keys := &keySet{byKID: map[string]any{}, client: p.client}
	if err := keys.refresh(ctx, p.client, uri); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.keys = keys
	p.mu.Unlock()
	return keys, nil
}

// keyfunc resolves the key a token was signed with, refetching once when
// the kid is unknown — which is what a provider's key rotation looks like
// from here.
func (k *keySet) keyfunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)

	k.mu.RLock()
	key, found := k.byKID[kid]
	k.mu.RUnlock()
	if found {
		return key, nil
	}

	// A single unknown kid is normal after a rotation; a stream of them
	// is somebody using this as an amplifier.
	k.mu.RLock()
	stale := time.Since(k.fetchedAt) > jwksMinRefresh
	k.mu.RUnlock()
	if !stale {
		return nil, fmt.Errorf("oidc: no published key with kid %q", kid)
	}
	k.mu.RLock()
	client, uri := k.client, k.jwksURI
	k.mu.RUnlock()
	if client == nil {
		client = http.DefaultClient
	}
	if err := k.refresh(context.Background(), client, uri); err != nil {
		return nil, err
	}

	k.mu.RLock()
	defer k.mu.RUnlock()
	if key, found := k.byKID[kid]; found {
		return key, nil
	}
	return nil, fmt.Errorf("oidc: no published key with kid %q", kid)
}

func (k *keySet) refresh(ctx context.Context, client *http.Client, uri string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("oidc: build jwks request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("oidc: fetch jwks: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks at %s answered %d", uri, response.StatusCode)
	}

	var document struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return fmt.Errorf("oidc: decode jwks: %w", err)
	}

	parsed := map[string]any{}
	for _, jwk := range document.Keys {
		key, err := jwk.publicKey()
		if err != nil {
			// One unusable key must not blind the set to the others:
			// providers publish key types this framework does not verify
			// with, and that is not an error until a token needs one.
			continue
		}
		parsed[jwk.KID] = key
	}
	if len(parsed) == 0 {
		return fmt.Errorf("oidc: the key set at %s has no key this framework can verify with", uri)
	}

	k.mu.Lock()
	k.byKID = parsed
	k.fetchedAt = time.Now()
	k.jwksURI = uri
	if k.client == nil {
		k.client = client
	}
	k.mu.Unlock()
	return nil
}

// jsonWebKey is the subset of RFC 7517 this provider verifies with.
type jsonWebKey struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (j jsonWebKey) publicKey() (any, error) {
	switch j.KTY {
	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(j.N)
		if err != nil {
			return nil, err
		}
		e, err := base64.RawURLEncoding.DecodeString(j.E)
		if err != nil {
			return nil, err
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		if exponent == 0 {
			return nil, fmt.Errorf("oidc: RSA key %q has a zero exponent", j.KID)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, nil

	case "EC":
		var curve elliptic.Curve
		switch j.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("oidc: unsupported curve %q", j.Crv)
		}
		x, err := base64.RawURLEncoding.DecodeString(j.X)
		if err != nil {
			return nil, err
		}
		y, err := base64.RawURLEncoding.DecodeString(j.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		}, nil

	default:
		return nil, fmt.Errorf("oidc: unsupported key type %q", j.KTY)
	}
}
