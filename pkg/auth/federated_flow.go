// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// FederatedInstance is one identity provider an operator declared.
//
// Name and Provider are different things on purpose. Name is the instance
// — what appears in the URL and what the operator writes their settings
// under — and Provider is the registered type that implements it. Two
// declarations with provider: oidc and different names are two identity
// providers, which is the ordinary case (a corporate tenant and a partner
// one) and the reason the registry is keyed by type.
type FederatedInstance struct {
	// Name identifies this instance: the `auth.<name>.*` subtree, the URL
	// segment, the value a sign-in link asks for.
	Name string `koanf:"name"`

	// Provider is the registered type — "oidc", "saml".
	Provider string `koanf:"provider"`

	// DisplayName is what a sign-in button says. Empty falls back to Name,
	// which is right for "corp" and wrong for nothing.
	DisplayName string `koanf:"display_name"`
}

// FederatedConfig builds the set of configured identity providers.
type FederatedConfig struct {
	// Instances are the declarations, in the order an operator wrote them
	// — which is the order sign-in buttons should appear in.
	Instances []FederatedInstance

	// ProviderConfig maps an INSTANCE name to its `auth.<name>.*` subtree,
	// the same channel a credential backend is configured through.
	ProviderConfig map[string]map[string]any

	// CallbackBase is the absolute base URL this application is reached
	// at, without a trailing slash — "https://app.example.com". The
	// callback URL a provider registers with its identity provider is
	// built from it, so it has to be the address the BROWSER uses, not
	// the one the process binds.
	CallbackBase string

	// PendingTTL bounds how long a started sign-in may sit unfinished.
	// Zero means DefaultFederatedPendingTTL.
	PendingTTL time.Duration
}

// DefaultFederatedPendingTTL is how long a started sign-in stays valid.
// Long enough for somebody to type a password and answer a second factor,
// short enough that an abandoned flow is not a lasting foothold.
const DefaultFederatedPendingTTL = 15 * time.Minute

// FederatedSet is the configured identity providers and the custody of the
// flows in progress.
//
// The custody is the point. A provider never sees the anti-forgery state:
// this type issues it, holds it, and refuses a callback that does not
// carry it back — before the provider is asked anything. A redirect flow
// without that check works perfectly well until somebody attacks it, which
// is exactly the kind of omission that does not announce itself, so the
// framework does not offer the choice.
type FederatedSet struct {
	order     []string
	instances map[string]FederatedInstance
	providers map[string]federated.Provider
	base      string
	ttl       time.Duration

	mu      sync.Mutex
	pending map[string]*pendingFlow
}

type pendingFlow struct {
	instance  string
	nonce     string
	state     map[string]string
	expiresAt time.Time
}

// NewFederatedSet builds the configured providers, failing on the first
// declaration that cannot be honoured.
func NewFederatedSet(cfg FederatedConfig) (*FederatedSet, error) {
	if len(cfg.Instances) == 0 {
		return nil, fmt.Errorf("auth: a federated set needs at least one instance")
	}
	base := strings.TrimRight(cfg.CallbackBase, "/")
	if base == "" {
		return nil, fmt.Errorf("auth: federated sign-in needs the public base URL (the address the browser uses), so the callback a provider registers with its identity provider is the one this application will be listening on")
	}
	ttl := cfg.PendingTTL
	if ttl <= 0 {
		ttl = DefaultFederatedPendingTTL
	}

	set := &FederatedSet{
		instances: make(map[string]FederatedInstance, len(cfg.Instances)),
		providers: make(map[string]federated.Provider, len(cfg.Instances)),
		base:      base,
		ttl:       ttl,
		pending:   map[string]*pendingFlow{},
	}

	for i, inst := range cfg.Instances {
		name := strings.ToLower(strings.TrimSpace(inst.Name))
		if name == "" {
			return nil, fmt.Errorf("auth: federated instance #%d has no name; the name is the URL segment and the configuration subtree, so it cannot be inferred", i+1)
		}
		if _, dup := set.instances[name]; dup {
			return nil, fmt.Errorf("auth: federated instance %q is declared twice; two identity providers cannot share a name because they would share a callback URL", name)
		}
		typ := strings.ToLower(strings.TrimSpace(inst.Provider))
		if typ == "" {
			return nil, fmt.Errorf("auth: federated instance %q does not say which provider implements it (registered: %s)", name, registeredFederatedNames())
		}
		factory, ok := federated.Lookup(typ)
		if !ok {
			return nil, fmt.Errorf("auth: federated instance %q wants provider %q, which is not registered (registered: %s). A provider registers itself when its package is imported for side effects.",
				name, typ, registeredFederatedNames())
		}

		inst.Name, inst.Provider = name, typ
		provider, err := factory(backend.Config{Name: name, ProviderConfig: cfg.ProviderConfig[name]})
		if err != nil {
			return nil, fmt.Errorf("auth: federated instance %q (%s): %w", name, typ, err)
		}
		if provider == nil {
			return nil, fmt.Errorf("auth: federated instance %q (%s): the factory returned no provider and no error", name, typ)
		}

		set.order = append(set.order, name)
		set.instances[name] = inst
		set.providers[name] = provider
	}
	return set, nil
}

func registeredFederatedNames() string {
	names := federated.Registered()
	if len(names) == 0 {
		return "none"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Names returns the configured instance names, in declaration order.
func (s *FederatedSet) Names() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Instances returns the declarations, in order, for a sign-in page that
// needs to render a button per identity provider.
func (s *FederatedSet) Instances() []FederatedInstance {
	out := make([]FederatedInstance, 0, len(s.order))
	for _, name := range s.order {
		inst := s.instances[name]
		if inst.DisplayName == "" {
			inst.DisplayName = inst.Name
		}
		out = append(out, inst)
	}
	return out
}

// CallbackURL is the absolute URL an identity provider must send the
// browser back to for one instance. An operator registers this value with
// their identity provider, and `nucleus doctor` prints it, so it is
// derived in exactly one place.
func (s *FederatedSet) CallbackURL(instance string) string {
	return s.base + FederatedCallbackPath(instance)
}

// FederatedStartPath and FederatedCallbackPath are the routes the
// framework wires for an instance. They are functions rather than
// constants because the instance name is in them.
func FederatedStartPath(instance string) string {
	return "/auth/" + url.PathEscape(instance) + "/start"
}

func FederatedCallbackPath(instance string) string {
	return "/auth/" + url.PathEscape(instance) + "/callback"
}

// Begin starts a sign-in and returns where to send the browser, plus the
// opaque state token the caller must give back on the callback.
//
// The token is what a caller stores in a short-lived cookie. It is not
// the provider's state and the provider never sees it.
func (s *FederatedSet) Begin(ctx context.Context, instance string) (redirectURL, stateToken string, err error) {
	name := strings.ToLower(strings.TrimSpace(instance))
	provider, ok := s.providers[name]
	if !ok {
		return "", "", fmt.Errorf("auth: no federated instance named %q (configured: %s)", instance, strings.Join(s.order, ", "))
	}

	nonce, err := randomToken()
	if err != nil {
		return "", "", fmt.Errorf("auth: federated %q: generating the nonce: %w", name, err)
	}
	token, err := randomToken()
	if err != nil {
		return "", "", fmt.Errorf("auth: federated %q: generating the anti-forgery state: %w", name, err)
	}

	red, err := provider.Begin(ctx, federated.BeginRequest{
		CallbackURL: s.CallbackURL(name),
		Nonce:       nonce,
	})
	if err != nil {
		return "", "", fmt.Errorf("auth: federated %q: starting the sign-in: %w", name, err)
	}
	if red.URL == "" {
		return "", "", fmt.Errorf("auth: federated %q: the provider returned no redirect URL", name)
	}

	s.mu.Lock()
	s.evictExpiredLocked()
	s.pending[token] = &pendingFlow{
		instance:  name,
		nonce:     nonce,
		state:     red.State,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()

	return red.URL, token, nil
}

// Complete verifies the callback and returns the authenticated identity.
//
// stateToken is what Begin returned. A callback that arrives without it,
// with one this set did not issue, or with one already spent, is refused
// HERE — the provider is not called, so a provider cannot forget to check.
func (s *FederatedSet) Complete(ctx context.Context, instance, stateToken string, r *http.Request) (*federated.User, error) {
	name := strings.ToLower(strings.TrimSpace(instance))
	provider, ok := s.providers[name]
	if !ok {
		return nil, fmt.Errorf("auth: no federated instance named %q (configured: %s)", instance, strings.Join(s.order, ", "))
	}

	flow, err := s.claim(name, stateToken)
	if err != nil {
		return nil, err
	}

	var form url.Values
	if r != nil {
		if err := r.ParseForm(); err == nil {
			form = r.PostForm
		}
	}
	var query url.Values
	if r != nil && r.URL != nil {
		query = r.URL.Query()
	}

	user, err := provider.Complete(ctx, federated.CompleteRequest{
		Query: query,
		Form:  form,
		State: flow.state,
		Nonce: flow.nonce,
	})
	if err != nil {
		return nil, err
	}
	if user == nil {
		// A provider that answers "no error, no user" would otherwise sign
		// in a nil identity. Treat it as unavailable rather than as a
		// rejection: it is a bug in the provider, not a decision about
		// this person.
		return nil, fmt.Errorf("%w: federated %q returned no user and no error", federated.ErrProviderUnavailable, name)
	}
	return user, nil
}

// claim consumes a pending flow. A state token is single use: replaying a
// callback must not sign anybody in twice.
//
// The defence is the lookup, not the empty check below: an unissued token
// is not in the map whether it is empty or invented. The empty case is
// separated only to say something more useful than "unknown state" when
// the caller forgot to pass the cookie through — verified by mutation,
// which is how it was found to be a message rather than a guard.
func (s *FederatedSet) claim(instance, token string) (*pendingFlow, error) {
	if token == "" {
		return nil, fmt.Errorf("auth: federated %q: the callback carried no anti-forgery state", instance)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()

	flow, ok := s.pending[token]
	if !ok {
		return nil, fmt.Errorf("auth: federated %q: the callback's anti-forgery state is unknown, already used, or expired", instance)
	}
	// Compared in constant time even though the lookup above is not: the
	// map hit proves the token exists, this proves the caller sent the one
	// belonging to the instance it claims.
	if subtle.ConstantTimeCompare([]byte(flow.instance), []byte(instance)) != 1 {
		return nil, fmt.Errorf("auth: federated %q: this anti-forgery state was issued for a different identity provider", instance)
	}
	delete(s.pending, token)
	if time.Now().After(flow.expiresAt) {
		return nil, fmt.Errorf("auth: federated %q: the sign-in took longer than %s and has to be started again", instance, s.ttl)
	}
	return flow, nil
}

func (s *FederatedSet) evictExpiredLocked() {
	now := time.Now()
	for token, flow := range s.pending {
		if now.After(flow.expiresAt) {
			delete(s.pending, token)
		}
	}
}

// PendingCount reports how many sign-ins are in flight. It exists for
// tests and for a metric; it is not part of the flow.
func (s *FederatedSet) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()
	return len(s.pending)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
