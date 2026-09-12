// Package oidc is an OpenID Connect provider for the federated sign-in
// seam: authorization code flow with PKCE, discovery, and an id_token
// verified against the provider's published keys.
//
// # Why this lives in the framework module and not in providers/
//
// The sibling modules (providers/ldap, drivers/*, exporters/*) exist for
// one measured reason: so an application that does not use a backend does
// not compile its third-party dependencies (ADR-030/031). This provider
// has NONE — discovery is net/http and encoding/json, PKCE is
// crypto/sha256, and the id_token is verified with the JWT library the
// framework already links. A Go package nobody imports costs nothing in
// anybody's binary, so extracting it would add a module, a tag, a manifest
// entry and a release train phase to solve a problem that does not exist
// here.
//
// # What it does and does not decide
//
// The framework owns the anti-forgery state (see pkg/auth/federated): this
// provider never sees it and cannot forget it. What it owns is the
// protocol — where to send the browser, how to exchange a code, and what a
// valid answer from the identity provider looks like.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// ProviderName is the name this provider registers under. An operator
// configures an INSTANCE of it: `auth_federated: [{name: corp, type: oidc}]`.
const ProviderName = "oidc"

func init() {
	// Registering in init is what makes `import _ ".../oidc"` enough, the
	// same shape every other pluggable backend uses.
	if err := federated.Register(ProviderName, New); err != nil {
		panic(fmt.Sprintf("oidc: register: %v", err))
	}
}

// Config is the `auth.<instance>.*` subtree an operator fills in.
type Config struct {
	// Issuer is the provider's base URL, e.g.
	// "https://accounts.example.com". Discovery hangs off it.
	Issuer string `koanf:"issuer"`
	// ClientID and ClientSecret identify this application.
	ClientID     string `koanf:"client_id"`
	ClientSecret string `koanf:"client_secret"`
	// Scopes defaults to "openid email profile". "openid" is added when
	// missing: without it the provider returns an OAuth token and no
	// identity, which fails later and confusingly.
	Scopes []string `koanf:"scopes"`
	// RoleClaim is the claim carrying group or role membership, e.g.
	// "groups" or "roles". Empty means the identity arrives with no
	// roles rather than with a guess.
	RoleClaim string `koanf:"role_claim"`
	// UsernameClaim defaults to "preferred_username", then "email".
	UsernameClaim string `koanf:"username_claim"`
	// SkipIssuerVerification exists for a provider whose discovery
	// document disagrees with its own issuer URL. It is a footgun with a
	// name, so an audit can find it.
	SkipIssuerVerification bool `koanf:"skip_issuer_verification"`
	// Timeout for discovery and token exchange. Default 10s.
	Timeout time.Duration `koanf:"timeout"`
}

// Provider implements federated.Provider.
type Provider struct {
	name   string
	cfg    Config
	client *http.Client

	mu       sync.RWMutex
	metadata *metadata
	keys     *keySet
}

// metadata is the part of the discovery document this provider uses.
type metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// New builds a provider from the operator's configuration. It is the
// federated.Factory this package registers.
func New(cfg backend.Config) (federated.Provider, error) {
	var parsed Config
	if err := cfg.Bind(&parsed); err != nil {
		return nil, fmt.Errorf("oidc: read configuration: %w", err)
	}
	return newProvider(cfg.Name, parsed, nil)
}

// newProvider applies the defaults and builds the provider. New and the
// tests both go through it, so a test cannot accidentally exercise a
// provider assembled differently from the one an operator gets — which is
// how "scopes are empty" turned up as a test-only condition the first time
// this was written.
func newProvider(name string, cfg Config, client *http.Client) (*Provider, error) {
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, errors.New("oidc: issuer is required (the provider's base URL)")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("oidc: client_id is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	cfg.Scopes = withOpenID(cfg.Scopes)
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{name: name, cfg: cfg, client: client}, nil
}

// Name implements federated.Provider.
func (p *Provider) Name() string { return p.name }

// Begin implements federated.Provider: it returns where to send the browser
// and the per-flow state the framework keeps for it.
//
// PKCE is not optional here. The code verifier makes a stolen authorization
// code useless to anyone who did not start the flow, and there is no
// deployment where leaving it out is the right trade — so it is generated
// whether or not the provider advertises support.
func (p *Provider) Begin(ctx context.Context, req federated.BeginRequest) (federated.Redirect, error) {
	meta, err := p.discover(ctx)
	if err != nil {
		return federated.Redirect{}, err
	}

	verifier, err := randomURLSafe(32)
	if err != nil {
		return federated.Redirect{}, err
	}
	challenge := pkceChallenge(verifier)

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", p.cfg.ClientID)
	params.Set("redirect_uri", req.CallbackURL)
	params.Set("scope", strings.Join(p.cfg.Scopes, " "))
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	if req.Nonce != "" {
		params.Set("nonce", req.Nonce)
	}

	return federated.Redirect{
		URL: meta.AuthorizationEndpoint + "?" + params.Encode(),
		State: map[string]string{
			"verifier":     verifier,
			"nonce":        req.Nonce,
			"callback_url": req.CallbackURL,
		},
	}, nil
}

// Complete implements federated.Provider: it exchanges the code and
// verifies the identity token.
func (p *Provider) Complete(ctx context.Context, req federated.CompleteRequest) (*backend.User, error) {
	if errMsg := strings.TrimSpace(req.Query.Get("error")); errMsg != "" {
		// The provider said no. Its description is operator-facing
		// detail; the caller gets the sentinel it can branch on.
		return nil, fmt.Errorf("%w: %s: %s", federated.ErrIdentityRejected,
			errMsg, req.Query.Get("error_description"))
	}
	code := strings.TrimSpace(req.Query.Get("code"))
	if code == "" {
		return nil, fmt.Errorf("%w: the callback carried no authorization code", federated.ErrIdentityRejected)
	}

	meta, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", req.State["callback_url"])
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", req.State["verifier"])
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}

	tokenResponse, err := p.exchange(ctx, meta.TokenEndpoint, form)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokenResponse.IDToken) == "" {
		// An OAuth2 provider answers with an access token and no
		// identity. Saying so beats a nil dereference three frames down.
		return nil, fmt.Errorf("%w: the provider returned no id_token (is \"openid\" in the scopes?)",
			federated.ErrIdentityRejected)
	}

	claims, err := p.verifyIDToken(ctx, meta, tokenResponse.IDToken, req.State["nonce"])
	if err != nil {
		return nil, err
	}
	return p.identityFrom(claims), nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (p *Provider) exchange(ctx context.Context, endpoint string, form url.Values) (tokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("oidc: build token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("oidc: token exchange: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("%w: the token endpoint answered %d",
			federated.ErrIdentityRejected, response.StatusCode)
	}
	var parsed tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return tokenResponse{}, fmt.Errorf("oidc: decode token response: %w", err)
	}
	return parsed, nil
}

// verifyIDToken checks the signature against the provider's published keys
// and every claim that decides whether this token is for this application,
// from this issuer, right now — and, when the flow carried one, that the
// nonce comes back. A token that is merely well-formed proves nothing.
func (p *Provider) verifyIDToken(ctx context.Context, meta *metadata, raw, nonce string) (jwt.MapClaims, error) {
	keys, err := p.jwks(ctx, meta.JWKSURI)
	if err != nil {
		return nil, err
	}

	claims := jwt.MapClaims{}
	parserOpts := []jwt.ParserOption{
		jwt.WithAudience(p.cfg.ClientID),
		jwt.WithExpirationRequired(),
		// Only asymmetric algorithms: an id_token signed HS256 with the
		// client secret as the key is how "alg confusion" starts.
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "PS256"}),
	}
	if !p.cfg.SkipIssuerVerification {
		parserOpts = append(parserOpts, jwt.WithIssuer(meta.Issuer))
	}

	token, err := jwt.ParseWithClaims(raw, claims, keys.keyfunc, parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: id_token: %v", federated.ErrIdentityRejected, err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("%w: the id_token did not validate", federated.ErrIdentityRejected)
	}
	if nonce != "" {
		got, _ := claims["nonce"].(string)
		if got != nonce {
			return nil, fmt.Errorf("%w: the id_token's nonce does not match this sign-in",
				federated.ErrIdentityRejected)
		}
	}
	return claims, nil
}

// identityFrom maps claims onto the framework's identity. Roles land in the
// LIST the identity now carries, which is the half of this that pkg/auth
// gained for it: a provider answering with three groups used to have one
// field to put them in.
func (p *Provider) identityFrom(claims jwt.MapClaims) *backend.User {
	user := &backend.User{}
	user.ID, _ = claims["sub"].(string)
	user.Email, _ = claims["email"].(string)

	usernameClaim := strings.TrimSpace(p.cfg.UsernameClaim)
	if usernameClaim != "" {
		user.Username, _ = claims[usernameClaim].(string)
	}
	if user.Username == "" {
		if preferred, ok := claims["preferred_username"].(string); ok {
			user.Username = preferred
		} else {
			user.Username = user.Email
		}
	}

	if roleClaim := strings.TrimSpace(p.cfg.RoleClaim); roleClaim != "" {
		user.Roles = stringsFromClaim(claims[roleClaim])
		if len(user.Roles) > 0 {
			user.Role = user.Roles[0]
		}
	}
	return user
}

// stringsFromClaim accepts the two shapes providers use for a list claim: a
// JSON array, and a space-separated string.
func stringsFromClaim(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return typed
	case string:
		return strings.Fields(typed)
	default:
		return nil
	}
}

func withOpenID(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{"openid", "email", "profile"}
	}
	for _, s := range scopes {
		if strings.EqualFold(strings.TrimSpace(s), "openid") {
			return scopes
		}
	}
	return append([]string{"openid"}, scopes...)
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: generate: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
