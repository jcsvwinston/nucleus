// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// fakeProvider records what the framework handed it, so the tests can
// assert on the division of labour itself — which is where the security
// properties of this seam live.
type fakeProvider struct {
	name        string
	beginCalls  int
	lastBegin   federated.BeginRequest
	completed   int
	lastRequest federated.CompleteRequest
	beginErr    error
	completeErr error
	user        *federated.User
	state       map[string]string
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Begin(_ context.Context, r federated.BeginRequest) (federated.Redirect, error) {
	f.beginCalls++
	f.lastBegin = r
	if f.beginErr != nil {
		return federated.Redirect{}, f.beginErr
	}
	return federated.Redirect{URL: "https://idp.example/authorize?x=1", State: f.state}, nil
}

func (f *fakeProvider) Complete(_ context.Context, r federated.CompleteRequest) (*federated.User, error) {
	f.completed++
	f.lastRequest = r
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	if f.user != nil {
		return f.user, nil
	}
	return &federated.User{Username: "ana"}, nil
}

func registerFake(t *testing.T, typ string, p *fakeProvider) {
	t.Helper()
	if err := federated.Register(typ, func(cfg backend.Config) (federated.Provider, error) {
		clone := *p
		clone.name = cfg.Name
		*p = clone
		return p, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { federated.Unregister(typ) })
}

func testSet(t *testing.T, p *fakeProvider, instances ...FederatedInstance) *FederatedSet {
	t.Helper()
	registerFake(t, "fakeidp", p)
	if len(instances) == 0 {
		instances = []FederatedInstance{{Name: "corp", Provider: "fakeidp"}}
	}
	set, err := NewFederatedSet(FederatedConfig{
		Instances:    instances,
		CallbackBase: "https://app.example.com",
	})
	if err != nil {
		t.Fatalf("NewFederatedSet: %v", err)
	}
	return set
}

// The whole reason this seam exists: the provider is never handed the
// anti-forgery state, so it cannot forget to check it. A callback with no
// state must be refused WITHOUT the provider being consulted.
func TestComplete_MissingStateIsRefusedWithoutCallingTheProvider(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p)

	if _, _, err := set.Begin(context.Background(), "corp"); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	_, err := set.Complete(context.Background(), "corp", "", callbackRequest(nil))
	if err == nil {
		t.Fatal("a callback with no anti-forgery state must be refused")
	}
	if p.completed != 0 {
		t.Fatalf("the provider must not be consulted for a callback that failed the state check, it was called %d times", p.completed)
	}
}

// A token this set never issued is the forged case, and it must be
// refused the same way — before the provider runs.
func TestComplete_ForgedStateIsRefusedWithoutCallingTheProvider(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p)

	if _, _, err := set.Begin(context.Background(), "corp"); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	_, err := set.Complete(context.Background(), "corp", "un-token-inventado", callbackRequest(nil))
	if err == nil {
		t.Fatal("a callback carrying a state this set did not issue must be refused")
	}
	if p.completed != 0 {
		t.Fatalf("the provider was consulted %d times for a forged state", p.completed)
	}
}

// A state token is single use. Replaying a callback must not sign the
// same flow in twice.
func TestComplete_StateTokenIsSingleUse(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p)

	_, token, err := set.Begin(context.Background(), "corp")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := set.Complete(context.Background(), "corp", token, callbackRequest(nil)); err != nil {
		t.Fatalf("the first callback must succeed: %v", err)
	}
	if _, err := set.Complete(context.Background(), "corp", token, callbackRequest(nil)); err == nil {
		t.Fatal("replaying the same anti-forgery state must be refused")
	}
	if p.completed != 1 {
		t.Fatalf("the provider must have been consulted exactly once, got %d", p.completed)
	}
}

// A token issued for one identity provider must not complete a flow at
// another: otherwise an attacker who can start a sign-in at a weak IdP
// could finish it at a strong one.
func TestComplete_StateIsBoundToTheInstanceThatIssuedIt(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p,
		FederatedInstance{Name: "corp", Provider: "fakeidp"},
		FederatedInstance{Name: "partners", Provider: "fakeidp"},
	)

	_, token, err := set.Begin(context.Background(), "corp")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := set.Complete(context.Background(), "partners", token, callbackRequest(nil)); err == nil {
		t.Fatal("a state issued for one instance must not complete a flow at another")
	}
}

// An abandoned sign-in must not stay usable. The flow is claimed even
// when it turns out to be expired, so a stale token cannot be probed
// repeatedly.
func TestComplete_ExpiredFlowIsRefusedAndConsumed(t *testing.T) {
	p := &fakeProvider{}
	registerFake(t, "fakeidp", p)
	set, err := NewFederatedSet(FederatedConfig{
		Instances:    []FederatedInstance{{Name: "corp", Provider: "fakeidp"}},
		CallbackBase: "https://app.example.com",
		PendingTTL:   time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewFederatedSet: %v", err)
	}

	_, token, err := set.Begin(context.Background(), "corp")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	if _, err := set.Complete(context.Background(), "corp", token, callbackRequest(nil)); err == nil {
		t.Fatal("a sign-in that sat unfinished past its TTL must be refused")
	}
	if p.completed != 0 {
		t.Fatalf("the provider must not be consulted for an expired flow, got %d calls", p.completed)
	}
	if n := set.PendingCount(); n != 0 {
		t.Fatalf("an expired flow must not stay in custody, %d left", n)
	}
}

// The framework owns the callback URL, not the provider: an operator
// registers with their identity provider the address this application
// will actually be listening on.
func TestBegin_HandsTheProviderTheFrameworksCallbackURL(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p)

	if _, _, err := set.Begin(context.Background(), "corp"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	want := "https://app.example.com/auth/corp/callback"
	if got := p.lastBegin.CallbackURL; got != want {
		t.Errorf("callback URL = %q, want %q", got, want)
	}
	if got := set.CallbackURL("corp"); got != want {
		t.Errorf("CallbackURL() = %q, want %q", got, want)
	}
	if p.lastBegin.Nonce == "" {
		t.Error("the provider must be handed a nonce it can bind into its own protocol")
	}
}

// The nonce reaching Complete must be the one Begin issued: a provider
// that checks the value its identity provider echoed back needs the same
// one, and the provider's own state must survive the round trip untouched.
func TestComplete_CarriesTheProvidersOwnStateAndNonceBack(t *testing.T) {
	p := &fakeProvider{state: map[string]string{"pkce_verifier": "abc123"}}
	set := testSet(t, p)

	_, token, err := set.Begin(context.Background(), "corp")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := set.Complete(context.Background(), "corp", token, callbackRequest(url.Values{"code": {"xyz"}})); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := p.lastRequest.Nonce; got != p.lastBegin.Nonce {
		t.Errorf("nonce at Complete = %q, at Begin = %q — they must match", got, p.lastBegin.Nonce)
	}
	if got := p.lastRequest.State["pkce_verifier"]; got != "abc123" {
		t.Errorf("the provider's own state must survive the round trip, got %q", got)
	}
	if got := p.lastRequest.Query.Get("code"); got != "xyz" {
		t.Errorf("the callback's query must reach the provider, got %q", got)
	}
}

// The anti-forgery token must never be the provider's problem — and it
// must never be reachable from what the provider is handed.
func TestComplete_ProviderNeverSeesTheAntiForgeryState(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p)

	_, token, err := set.Begin(context.Background(), "corp")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := set.Complete(context.Background(), "corp", token, callbackRequest(nil)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for key, value := range p.lastRequest.State {
		if strings.Contains(value, token) {
			t.Fatalf("the anti-forgery state leaked to the provider through State[%q]", key)
		}
	}
	if p.lastRequest.Nonce == token {
		t.Fatal("the nonce must not BE the anti-forgery state; a provider that echoes the nonce would publish it")
	}
}

// A provider that answers "no error, no user" would otherwise sign in a
// nil identity. That is a bug in the provider, so it reads as unavailable
// rather than as a decision about this person.
func TestComplete_NilUserWithoutErrorIsUnavailableNotAcceptance(t *testing.T) {
	set := testSet(t, &fakeProvider{})
	set.providers["corp"] = nilUserProvider{}

	_, token, err := set.Begin(context.Background(), "corp")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := set.Complete(context.Background(), "corp", token, callbackRequest(nil)); !errors.Is(err, federated.ErrProviderUnavailable) {
		t.Fatalf("want ErrProviderUnavailable, got %v", err)
	}
}

type nilUserProvider struct{}

func (nilUserProvider) Name() string { return "corp" }
func (nilUserProvider) Begin(context.Context, federated.BeginRequest) (federated.Redirect, error) {
	return federated.Redirect{URL: "https://idp.example/authorize"}, nil
}
func (nilUserProvider) Complete(context.Context, federated.CompleteRequest) (*federated.User, error) {
	return nil, nil
}

// An instance naming a provider nobody registered must fail at build
// time, naming what IS registered — that error is the only place an
// operator discovers the registry exists.
func TestNewFederatedSet_UnknownProviderNamesTheRegisteredOnes(t *testing.T) {
	p := &fakeProvider{}
	registerFake(t, "fakeidp", p)

	_, err := NewFederatedSet(FederatedConfig{
		Instances:    []FederatedInstance{{Name: "corp", Provider: "sarl"}},
		CallbackBase: "https://app.example.com",
	})
	if err == nil {
		t.Fatal("an unregistered provider type must fail")
	}
	if !strings.Contains(err.Error(), "fakeidp") {
		t.Errorf("the error must name what is registered so the typo is visible, got: %v", err)
	}
}

// Two identity providers of the same type is the ordinary case, and the
// reason instance and type are different names.
func TestNewFederatedSet_TwoInstancesOfTheSameProviderType(t *testing.T) {
	p := &fakeProvider{}
	set := testSet(t, p,
		FederatedInstance{Name: "corp", Provider: "fakeidp", DisplayName: "Corp SSO"},
		FederatedInstance{Name: "partners", Provider: "fakeidp"},
	)

	if got := set.Names(); len(got) != 2 || got[0] != "corp" || got[1] != "partners" {
		t.Fatalf("declaration order must be preserved, got %v", got)
	}
	insts := set.Instances()
	if insts[0].DisplayName != "Corp SSO" {
		t.Errorf("display name = %q", insts[0].DisplayName)
	}
	if insts[1].DisplayName != "partners" {
		t.Errorf("an absent display name must fall back to the instance name, got %q", insts[1].DisplayName)
	}
	if a, b := set.CallbackURL("corp"), set.CallbackURL("partners"); a == b {
		t.Fatalf("two instances must not share a callback URL: %s", a)
	}
}

// Duplicated instance names would share a callback URL, so they fail
// rather than letting the last declaration win in silence.
func TestNewFederatedSet_DuplicateInstanceNameIsAnError(t *testing.T) {
	p := &fakeProvider{}
	registerFake(t, "fakeidp", p)

	_, err := NewFederatedSet(FederatedConfig{
		Instances: []FederatedInstance{
			{Name: "corp", Provider: "fakeidp"},
			{Name: "corp", Provider: "fakeidp"},
		},
		CallbackBase: "https://app.example.com",
	})
	if err == nil {
		t.Fatal("two instances with the same name must fail")
	}
}

// Without the public base URL the callback would be a guess, and a wrong
// callback is a sign-in that fails only in production.
func TestNewFederatedSet_RequiresThePublicBaseURL(t *testing.T) {
	p := &fakeProvider{}
	registerFake(t, "fakeidp", p)

	_, err := NewFederatedSet(FederatedConfig{
		Instances: []FederatedInstance{{Name: "corp", Provider: "fakeidp"}},
	})
	if err == nil {
		t.Fatal("a federated set with no public base URL must fail")
	}
}

// The instance's own subtree must reach its factory, the same channel a
// credential backend is configured through.
func TestNewFederatedSet_HandsEachInstanceItsOwnSubtree(t *testing.T) {
	var seen []backend.Config
	if err := federated.Register("recording", func(cfg backend.Config) (federated.Provider, error) {
		seen = append(seen, cfg)
		return &fakeProvider{name: cfg.Name}, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { federated.Unregister("recording") })

	_, err := NewFederatedSet(FederatedConfig{
		Instances: []FederatedInstance{
			{Name: "corp", Provider: "recording"},
			{Name: "partners", Provider: "recording"},
		},
		ProviderConfig: map[string]map[string]any{
			"corp":     {"issuer": "https://login.corp.example/"},
			"partners": {"issuer": "https://idp.partners.example/"},
		},
		CallbackBase: "https://app.example.com",
	})
	if err != nil {
		t.Fatalf("NewFederatedSet: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("both factories must run, got %d", len(seen))
	}
	if seen[0].Name != "corp" || seen[0].ProviderConfig["issuer"] != "https://login.corp.example/" {
		t.Errorf("corp got %+v", seen[0])
	}
	if seen[1].Name != "partners" || seen[1].ProviderConfig["issuer"] != "https://idp.partners.example/" {
		t.Errorf("partners got %+v", seen[1])
	}
}

func callbackRequest(query url.Values) *http.Request {
	target := "/auth/corp/callback"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return httptest.NewRequest(http.MethodGet, target, nil)
}
