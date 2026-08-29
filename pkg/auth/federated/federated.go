// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package federated is the contract a browser-redirect identity provider
// implements — OIDC, SAML, anything where the user leaves for an identity
// provider and comes back — and nothing else.
//
// It is a second contract rather than a wider first one because the shapes
// genuinely differ. [backend.Backend] answers "do these credentials belong
// to a real user": one call, no browser, no state. A federated flow has no
// credentials to hand over at all. The application sends the browser away
// with a request it must remember, the provider answers to a different URL
// later, and only then is there an identity. Squeezing that into
// Authenticate(ctx, username, password) would have meant a password
// parameter nobody fills in and a stateful handshake hidden behind a
// stateless signature.
//
// # What the framework keeps
//
// The provider does NOT own the anti-forgery state. The framework
// generates it, stores it, and refuses a callback that does not carry it
// back — before the provider is called at all. That division is the reason
// this package exists rather than a bare interface: the state parameter is
// the part of a redirect flow that is easy to leave out and impossible to
// notice missing, because a flow with no CSRF protection works perfectly
// well until somebody attacks it. An author who forgets it here cannot
// forget it: they are never handed the choice.
//
// The provider is given a Nonce it may bind into its own protocol (an
// OIDC id_token nonce, a SAML RelayState) and must not confuse with the
// state: the state is the framework's, checked before Complete runs.
//
// # What the provider owns
//
// Where to send the browser, and what a valid answer from its own identity
// provider looks like. Everything protocol-specific — signature validation,
// token exchange, claim mapping — belongs to the provider, because that is
// what differs between OIDC and SAML and what a third party knows better
// than this framework does.
package federated

import (
	"context"
	"errors"
	"net/url"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// Errors a provider returns. They mirror [backend.ErrInvalidCredentials]
// and [backend.ErrBackendUnavailable] deliberately: the reasons the
// distinction matters do not change because the protocol did.
var (
	// ErrIdentityRejected is a CERTAIN no: the identity provider answered,
	// and the answer was that this person does not get in.
	ErrIdentityRejected = errors.New("auth: the identity provider rejected the sign-in")

	// ErrProviderUnavailable means the provider could not reach its
	// identity provider, or the answer was unusable. It is not a
	// rejection: an operator with a local account must still be able to
	// sign in on the morning the identity provider is down.
	ErrProviderUnavailable = errors.New("auth: identity provider unavailable")
)

// User is the identity a completed flow yields. It is [backend.User] —
// the same type, not a parallel one — so an application that already knows
// how to receive an authenticated user does not learn a second shape.
type User = backend.User

// Provider is one configured identity provider.
//
// An implementation is built by a [Factory] and used concurrently: Begin
// and Complete may run for many sign-ins at once, so an implementation
// holds configuration, not per-flow state. The per-flow state is what
// Begin returns and what the framework carries.
type Provider interface {
	// Name is the INSTANCE name the operator configured this provider
	// under — "corp", not "oidc". It appears in its URLs and its logs.
	Name() string

	// Begin starts a sign-in and says where to send the browser.
	Begin(ctx context.Context, req BeginRequest) (Redirect, error)

	// Complete validates what came back and returns the identity.
	//
	// The framework has already verified that the callback carries the
	// state it issued; a provider does not re-check it and must not
	// accept a callback that skips it, because it never sees one.
	Complete(ctx context.Context, req CompleteRequest) (*User, error)
}

// BeginRequest is what the framework hands a provider to start a flow.
type BeginRequest struct {
	// CallbackURL is the absolute URL the identity provider must send the
	// browser back to. The framework owns it — it is the route it wired
	// and the one it will be listening on — so a provider registers this
	// value with its identity provider rather than inventing one.
	CallbackURL string

	// Nonce is a single-use value the provider may bind into its own
	// protocol so the answer can be tied to this request. It is NOT the
	// anti-forgery state: that one never reaches the provider.
	Nonce string
}

// Redirect is where to send the browser to start a sign-in.
type Redirect struct {
	// URL is the absolute URL to redirect to.
	URL string

	// State is whatever the provider needs back when the flow completes —
	// a PKCE verifier, a SAML request ID. The framework stores it with
	// the pending flow and returns it in CompleteRequest, and never
	// interprets it. It is not sent to the browser.
	State map[string]string
}

// CompleteRequest is the callback, parsed and vouched for.
type CompleteRequest struct {
	// Query and Form are the values the identity provider sent back. A
	// provider reads its own protocol's parameters from them; it is given
	// values rather than the request so that reaching for a header, a
	// cookie or the session is not possible by accident.
	Query url.Values
	Form  url.Values

	// State is exactly what Begin returned in Redirect.State.
	State map[string]string

	// Nonce is the one Begin was given, for a provider that must check it
	// against what its identity provider echoed back.
	Nonce string
}

// Factory builds a configured provider. cfg carries the instance name and
// the `auth.<instance>.*` subtree, the same channel a credential backend
// receives — an operator configures a federated provider the way they
// configure any other, and a provider reads it with cfg.Bind.
type Factory func(cfg backend.Config) (Provider, error)
