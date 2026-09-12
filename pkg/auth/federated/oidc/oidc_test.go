package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// fakeIdP is an identity provider: discovery, a token endpoint and a real
// JWKS with a real signing key. A provider tested against a mock that
// returns a canned string proves the code compiles; this one proves it
// verifies.
type fakeIdP struct {
	server    *httptest.Server
	key       *rsa.PrivateKey
	kid       string
	issuer    string
	clientID  string
	claims    jwt.MapClaims
	nonce     string
	lastForm  url.Values
	tokenCode string
}

func newFakeIdP(t *testing.T, clientID string) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	idp := &fakeIdP{key: key, kid: "test-key-1", clientID: clientID, tokenCode: "the-code"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.issuer,
			"authorization_endpoint": idp.issuer + "/authorize",
			"token_endpoint":         idp.issuer + "/token",
			"jwks_uri":               idp.issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{idp.jwk()}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.lastForm = r.PostForm
		if r.PostForm.Get("code") != idp.tokenCode {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at",
			"token_type":   "Bearer",
			"id_token":     idp.signIDToken(t, idp.claims),
		})
	})

	idp.server = httptest.NewServer(mux)
	idp.issuer = idp.server.URL
	t.Cleanup(idp.server.Close)
	return idp
}

func (i *fakeIdP) jwk() map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": i.kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes()),
	}
}

func (i *fakeIdP) signIDToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	if claims == nil {
		claims = jwt.MapClaims{}
	}
	full := jwt.MapClaims{
		"iss": i.issuer,
		"aud": i.clientID,
		"sub": "subject-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	// A real provider echoes the nonce it was given back into the token.
	if i.nonce != "" {
		full["nonce"] = i.nonce
	}
	for k, v := range claims {
		full[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, full)
	token.Header["kid"] = i.kid
	signed, err := token.SignedString(i.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func testProvider(t *testing.T, idp *fakeIdP, cfg Config) *Provider {
	t.Helper()
	cfg.Issuer = idp.issuer
	if cfg.ClientID == "" {
		cfg.ClientID = idp.clientID
	}
	// Through the same constructor production uses, so the defaults under
	// test are the defaults an operator gets.
	p, err := newProvider("corp", cfg, idp.server.Client())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	return p
}

func beginAndComplete(t *testing.T, p *Provider, idp *fakeIdP, nonce string) (*backend.User, error) {
	t.Helper()
	idp.nonce = nonce
	redirect, err := p.Begin(context.Background(), federated.BeginRequest{
		CallbackURL: "https://app.example.test/auth/corp/callback",
		Nonce:       nonce,
	})
	if err != nil {
		return nil, err
	}
	return p.Complete(context.Background(), federated.CompleteRequest{
		Query: url.Values{"code": []string{idp.tokenCode}},
		State: redirect.State,
		Nonce: nonce,
	})
}

func TestOIDC_SignInEndToEnd(t *testing.T) {
	idp := newFakeIdP(t, "client-1")
	idp.claims = jwt.MapClaims{
		"email":              "ana@example.test",
		"preferred_username": "ana",
		"groups":             []any{"editors", "billing"},
	}
	p := testProvider(t, idp, Config{RoleClaim: "groups", Scopes: []string{"openid", "email"}})

	user, err := beginAndComplete(t, p, idp, "nonce-1")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if user.ID != "subject-1" || user.Email != "ana@example.test" || user.Username != "ana" {
		t.Fatalf("identity is %+v", user)
	}
	// The roles a provider returns land in the LIST the identity gained
	// for exactly this.
	if len(user.Roles) != 2 || user.Role != "editors" {
		t.Fatalf("roles are %v (primary %q)", user.Roles, user.Role)
	}
}

// PKCE is generated whether or not the provider advertises it, and the
// verifier that reaches the token endpoint must match the challenge that
// started the flow.
func TestOIDC_PKCEIsAlwaysUsedAndMatches(t *testing.T) {
	idp := newFakeIdP(t, "client-1")
	p := testProvider(t, idp, Config{})

	idp.nonce = "n"
	redirect, err := p.Begin(context.Background(), federated.BeginRequest{
		CallbackURL: "https://app.example.test/cb", Nonce: "n",
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	parsed, err := url.Parse(redirect.URL)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	challenge := parsed.Query().Get("code_challenge")
	if challenge == "" || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("no S256 challenge in %s", redirect.URL)
	}
	if parsed.Query().Get("scope") == "" || !strings.Contains(parsed.Query().Get("scope"), "openid") {
		t.Errorf("scopes are %q, openid must be there", parsed.Query().Get("scope"))
	}

	if _, err := p.Complete(context.Background(), federated.CompleteRequest{
		Query: url.Values{"code": []string{idp.tokenCode}},
		State: redirect.State,
		Nonce: "n",
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	verifier := idp.lastForm.Get("code_verifier")
	if verifier == "" {
		t.Fatal("no code_verifier reached the token endpoint")
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Fatal("the verifier does not match the challenge")
	}
}

// Everything an id_token has to be rejected for. Each case is one property
// a "well-formed token" does not prove.
func TestOIDC_RejectsBadIdentityTokens(t *testing.T) {
	cases := map[string]struct {
		claims jwt.MapClaims
		nonce  string
		mangle func(idp *fakeIdP)
	}{
		"another audience": {claims: jwt.MapClaims{"aud": "somebody-else"}},
		"another issuer":   {claims: jwt.MapClaims{"iss": "https://evil.example"}},
		"already expired":  {claims: jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}},
		"mismatched nonce": {claims: jwt.MapClaims{"nonce": "not-the-one"}, nonce: "expected-nonce"},
		"unknown signer":   {mangle: func(idp *fakeIdP) { idp.kid = "a-kid-nobody-published" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			idp := newFakeIdP(t, "client-1")
			idp.claims = tc.claims
			p := testProvider(t, idp, Config{})
			if tc.mangle != nil {
				// Fetch the key set first, so the mangled kid is
				// genuinely unknown rather than merely unfetched.
				if _, err := p.jwks(context.Background(), idp.issuer+"/jwks"); err != nil {
					t.Fatalf("prime jwks: %v", err)
				}
				tc.mangle(idp)
			}

			if _, err := beginAndComplete(t, p, idp, tc.nonce); err == nil {
				t.Fatal("the token was accepted")
			} else if !errors.Is(err, federated.ErrIdentityRejected) && !strings.Contains(err.Error(), "kid") {
				t.Fatalf("rejected for the wrong reason: %v", err)
			}
		})
	}
}

// An id_token signed with the CLIENT SECRET as an HMAC key is the "alg
// confusion" attack. Only asymmetric algorithms are accepted.
func TestOIDC_RefusesASymmetricallySignedToken(t *testing.T) {
	idp := newFakeIdP(t, "client-1")
	p := testProvider(t, idp, Config{ClientSecret: "the-client-secret"})
	if _, err := p.jwks(context.Background(), idp.issuer+"/jwks"); err != nil {
		t.Fatalf("prime jwks: %v", err)
	}

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": idp.issuer, "aud": "client-1", "sub": "attacker",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	forged.Header["kid"] = idp.kid
	signed, err := forged.SignedString([]byte("the-client-secret"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	meta, err := p.discover(context.Background())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if _, err := p.verifyIDToken(context.Background(), meta, signed, ""); err == nil {
		t.Fatal("an HS256 token signed with the client secret was accepted")
	}
}

// A provider that answers with an access token and no identity is an OAuth2
// provider. Saying so beats failing three frames later.
func TestOIDC_NoIDTokenIsNamed(t *testing.T) {
	idp := newFakeIdP(t, "client-1")
	idp.server.Config.Handler.(*http.ServeMux).HandleFunc("/token-empty", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer"})
	})
	p := testProvider(t, idp, Config{})
	if _, err := p.exchangeAndVerify(t, idp, "/token-empty"); err == nil || !strings.Contains(err.Error(), "openid") {
		t.Fatalf("the error does not name the cause: %v", err)
	}
}

// exchangeAndVerify drives Complete against an alternative token endpoint.
func (p *Provider) exchangeAndVerify(t *testing.T, idp *fakeIdP, path string) (*backend.User, error) {
	t.Helper()
	meta, err := p.discover(context.Background())
	if err != nil {
		return nil, err
	}
	meta.TokenEndpoint = idp.issuer + path
	p.mu.Lock()
	p.metadata = meta
	p.mu.Unlock()
	return p.Complete(context.Background(), federated.CompleteRequest{
		Query: url.Values{"code": []string{idp.tokenCode}},
		State: map[string]string{"callback_url": "https://app.example.test/cb", "verifier": "v"},
	})
}

// A discovery document whose issuer disagrees with where it was fetched
// from is a misconfiguration or a redirect somebody should look at.
func TestOIDC_DiscoveryIssuerMismatchIsRefused(t *testing.T) {
	idp := newFakeIdP(t, "client-1")
	p := testProvider(t, idp, Config{})
	p.cfg.Issuer = idp.issuer // correct
	idp.issuer = "https://somewhere.else"

	if _, err := p.discover(context.Background()); err == nil {
		t.Fatal("a mismatched issuer was accepted")
	} else if !strings.Contains(err.Error(), "skip_issuer_verification") {
		t.Fatalf("the error does not name the escape hatch: %v", err)
	}
}

// The provider that came back with an error parameter said no.
func TestOIDC_ProviderErrorIsRejection(t *testing.T) {
	idp := newFakeIdP(t, "client-1")
	p := testProvider(t, idp, Config{})
	_, err := p.Complete(context.Background(), federated.CompleteRequest{
		Query: url.Values{
			"error":             []string{"access_denied"},
			"error_description": []string{"the user said no"},
		},
		State: map[string]string{},
	})
	if !errors.Is(err, federated.ErrIdentityRejected) {
		t.Fatalf("expected ErrIdentityRejected, got %v", err)
	}
}

func TestOIDC_ConfigurationIsChecked(t *testing.T) {
	if _, err := New(backend.Config{Name: "corp"}); err == nil {
		t.Error("a provider with no issuer was built")
	}
}

func TestOIDC_RegistersItself(t *testing.T) {
	found := false
	for _, name := range federated.Registered() {
		if name == ProviderName {
			found = true
		}
	}
	if !found {
		t.Fatalf("the provider is not registered: %v", federated.Registered())
	}
}

func TestStringsFromClaim(t *testing.T) {
	cases := map[string]any{
		"array":  []any{"a", "b"},
		"space":  "a b",
		"native": []string{"a", "b"},
	}
	for name, value := range cases {
		if got := stringsFromClaim(value); len(got) != 2 {
			t.Errorf("%s: got %v", name, got)
		}
	}
	if got := stringsFromClaim(42); got != nil {
		t.Errorf("a number produced %v", got)
	}
}
