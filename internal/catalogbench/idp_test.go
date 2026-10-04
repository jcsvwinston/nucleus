// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// standInIdP is an OpenID Connect provider small enough to read: discovery,
// a published RSA key, and a token endpoint that exchanges the one code it
// was told to expect — checking the PKCE verifier against the challenge the
// application sent — for an id_token carrying the nonce the application
// bound into the flow. What the bench measures is the application's side of
// the flow, end to end, without a cloud identity provider.
type standInIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu        sync.Mutex
	code      string
	nonce     string
	challenge string
	clientID  string
	exchanged bool
}

const standInKID = "catalogbench"

func newStandInIdP(tb testing.TB) *standInIdP {
	tb.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		tb.Fatal(err)
	}
	p := &standInIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 p.srv.URL,
			"authorization_endpoint": p.srv.URL + "/authorize",
			"token_endpoint":         p.srv.URL + "/token",
			"jwks_uri":               p.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := p.key.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": standInKID, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.mu.Lock()
		defer p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != p.code || p.code == "" ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": p.srv.URL, "aud": p.clientID, "sub": "bench-subject",
			"preferred_username": "bench-user", "email": "bench@example.test",
			"nonce": p.nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
		})
		token.Header["kid"] = standInKID
		signed, err := token.SignedString(p.key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.code, p.exchanged = "", true // single use
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "bench", "token_type": "Bearer", "id_token": signed, "expires_in": 300})
	})
	p.srv = httptest.NewServer(mux)
	tb.Cleanup(p.srv.Close)
	return p
}

// expect arms the token endpoint for one sign-in: the code the browser will
// bring back, and what the application sent to the authorize endpoint.
func (p *standInIdP) expect(code, nonce, challenge, clientID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code, p.nonce, p.challenge, p.clientID, p.exchanged = code, nonce, challenge, clientID, false
}

func (p *standInIdP) wasExchanged() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exchanged
}

// idp is the bench's stand-in identity provider, started on first use.
func (e *env) idp() *standInIdP {
	e.idpOnce.Do(func() { e.idpSrv = newStandInIdP(e.tb) })
	return e.idpSrv
}
