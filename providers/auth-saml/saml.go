// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package saml is a SAML 2.0 service provider for the federated sign-in
// seam (pkg/auth/federated): a person signs in at an identity provider —
// Okta, Microsoft Entra ID, Google Workspace, Keycloak, Shibboleth, ADFS —
// and comes back to the application with a signed assertion.
//
// Import it for its side effect, declare an instance and mount the
// framework's sign-in routes; `nucleus add saml` writes all three:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/auth-saml"
//
//	nucleus.New().FromConfigFile("nucleus.yml").Mount(nucleus.FederatedSignIn()).Start()
//
//	# nucleus.yml
//	public_base_url: https://app.example.com
//	auth_federated:
//	  - name: corp
//	    provider: saml
//	auth:
//	  corp:
//	    sp_entity_id: https://app.example.com/auth/corp/metadata
//	    idp_metadata_url: https://idp.example.com/metadata
//
// # Why it is a module of its own
//
// The OIDC provider lives in the framework because it needs nothing the
// framework does not already link. SAML needs an XML signature
// implementation, and this package uses a maintained one rather than its
// own: github.com/crewjam/saml for the protocol, which verifies signatures
// with github.com/russellhaering/goxmldsig. An application that does not
// speak SAML does not link either (ADR-030/031).
//
// # What it accepts
//
// Only what answers a sign-in this application started, signed by the
// identity provider its metadata names:
//
//   - The flow is service-provider initiated: the AuthnRequest goes out with
//     the HTTP-Redirect binding, and the Response must come back by
//     HTTP-POST. A Response that answers no request this application made
//     (identity-provider-initiated sign-in) is refused, and so is the
//     artifact binding.
//   - The assertion itself must carry a valid signature by a certificate in
//     the identity provider's metadata. A signed Response around an
//     unsigned assertion is refused; so is a Response with more than one
//     assertion, an encrypted assertion, or two elements sharing an ID.
//   - Its audience must be this application's entity ID, its recipient the
//     callback URL, its InResponseTo the request the start route issued,
//     and its validity window must include now, with the clock skew the
//     library allows (three minutes).
//   - An assertion is accepted once: the framework spends the sign-in's
//     state on the first callback, and this package remembers the
//     assertion's ID until it expires.
//
// The anti-forgery state is the framework's, as for every federated
// provider: a callback that does not carry the cookie the start route set
// is refused before this package is consulted.
package saml

import (
	"context"
	"crypto"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	crewjam "github.com/crewjam/saml"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// ProviderName is the name this provider registers under. An operator
// configures an INSTANCE of it: `auth_federated: [{name: corp, provider: saml}]`.
const ProviderName = "saml"

func init() {
	if err := federated.Register(ProviderName, New); err != nil {
		panic(fmt.Sprintf("saml: register: %v", err))
	}
}

// Config is the `auth.<instance>.*` subtree an operator fills in. A key
// that is not one of these stops the boot.
type Config struct {
	// SPEntityID is this application's entity ID: the name the identity
	// provider knows it by, and the audience every assertion must carry.
	// Required. The convention is the URL of the metadata this provider
	// serves, <public_base_url>/auth/<instance>/metadata.
	SPEntityID string `koanf:"sp_entity_id"`

	// IdPMetadataURL is where the identity provider publishes its metadata.
	// It is fetched on the first sign-in, not at boot — an application
	// still starts the morning the identity provider does not answer — and
	// again every MetadataRefresh. It must be https; plain http is accepted
	// for a loopback address only.
	IdPMetadataURL string `koanf:"idp_metadata_url"`

	// IdPMetadataFile is the same document read from a file, at boot.
	// Exactly one of IdPMetadataURL and IdPMetadataFile is set.
	IdPMetadataFile string `koanf:"idp_metadata_file"`

	// IdPEntityID selects the identity provider in a metadata document that
	// describes several (an EntitiesDescriptor). Optional for a document
	// with one; when set, the document's entity ID must match it.
	IdPEntityID string `koanf:"idp_entity_id"`

	// SPCertificateFile and SPKeyFile are this application's key pair, PEM
	// encoded. Optional, together. With them the AuthnRequest is signed
	// (RSA-SHA256 or ECDSA-SHA256) and the certificate is published in the
	// service-provider metadata for the identity provider to verify it.
	SPCertificateFile string `koanf:"sp_certificate_file"`
	SPKeyFile         string `koanf:"sp_key_file"`

	// NameIDFormat is the NameID format the AuthnRequest asks for:
	// persistent, email, transient, unspecified, or a full
	// urn:oasis:names:tc:SAML URI. Empty asks for none, which lets the
	// identity provider choose. The NameID becomes the identity's ID, so a
	// format that is stable for a person (persistent) is the one to ask for
	// when the application keeps anything keyed by it.
	NameIDFormat string `koanf:"name_id_format"`

	// EmailAttribute, UsernameAttribute and RoleAttribute name the
	// assertion attributes (by Name or FriendlyName) mapped onto the
	// identity. Empty email and username look for the names identity
	// providers commonly use (mail, email, uid, and their OID and claim
	// URIs); an empty role attribute maps no roles rather than guessing.
	EmailAttribute    string `koanf:"email_attribute"`
	UsernameAttribute string `koanf:"username_attribute"`
	RoleAttribute     string `koanf:"role_attribute"`

	// MetadataRefresh is how long fetched metadata is used before it is
	// fetched again. Default 24h, minimum 1m. A refresh that fails keeps
	// the last metadata that loaded and tries again a minute later.
	MetadataRefresh time.Duration `koanf:"metadata_refresh"`

	// Timeout bounds a metadata fetch. Default 10s.
	Timeout time.Duration `koanf:"timeout"`
}

// Provider implements federated.Provider.
type Provider struct {
	name         string
	cfg          Config
	nameIDFormat crewjam.NameIDFormat

	// key and cert are the optional SP key pair; sigMethod is set when
	// they are, and then every AuthnRequest is signed.
	key       crypto.Signer
	cert      *x509.Certificate
	sigMethod string

	metadata *metadataSource
	replay   *replayCache
}

// New builds a provider from the operator's configuration. It is the
// federated.Factory this package registers.
func New(cfg backend.Config) (federated.Provider, error) {
	var parsed Config
	if err := cfg.Bind(&parsed); err != nil {
		return nil, fmt.Errorf("saml: read configuration: %w", err)
	}
	return newProvider(cfg.Name, parsed, nil)
}

// newProvider validates the configuration and builds the provider. New and
// the tests both go through it, so a test exercises the provider an
// operator gets.
func newProvider(name string, cfg Config, client *http.Client) (*Provider, error) {
	cfg.SPEntityID = strings.TrimSpace(cfg.SPEntityID)
	if cfg.SPEntityID == "" {
		return nil, errors.New("saml: sp_entity_id is required (this application's entity ID, conventionally <public_base_url>/auth/<instance>/metadata)")
	}
	if len(cfg.SPEntityID) > 1024 || strings.ContainsAny(cfg.SPEntityID, " \t\r\n") {
		return nil, fmt.Errorf("saml: sp_entity_id %q is not a URI", cfg.SPEntityID)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	switch {
	case cfg.MetadataRefresh == 0:
		cfg.MetadataRefresh = 24 * time.Hour
	case cfg.MetadataRefresh < time.Minute:
		return nil, fmt.Errorf("saml: metadata_refresh %s is shorter than the minimum of 1m", cfg.MetadataRefresh)
	}

	format, err := parseNameIDFormat(cfg.NameIDFormat)
	if err != nil {
		return nil, err
	}

	p := &Provider{name: name, cfg: cfg, nameIDFormat: format, replay: newReplayCache()}
	if p.key, p.cert, p.sigMethod, err = loadKeyPair(cfg.SPCertificateFile, cfg.SPKeyFile); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	if p.metadata, err = newMetadataSource(cfg, client); err != nil {
		return nil, err
	}
	return p, nil
}

// Name implements federated.Provider.
func (p *Provider) Name() string { return p.name }

// CallbackIsCrossSiteFormPost reports that the identity provider sends the
// browser back with a form POST from its own site (the HTTP-POST binding).
// A SameSite=Lax cookie does not ride such a request, so FederatedSignIn
// sets the sign-in's state cookie SameSite=None (with Secure) for this
// instance when the application is served over https. The method uses no
// type of the framework so that this module builds against the release it
// pins.
func (p *Provider) CallbackIsCrossSiteFormPost() bool { return true }

// relayStatePattern is what the RelayState may look like: the library
// appends it to the redirect URL without escaping it, so only characters
// that need none are accepted. The framework's nonce is base64url.
var relayStatePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

// Begin implements federated.Provider: an AuthnRequest, sent with the
// HTTP-Redirect binding, asking for the Response by HTTP-POST at the
// callback. The framework's nonce is the RelayState, and the request's ID is
// kept in the flow's state so the Response can be bound to it.
func (p *Provider) Begin(ctx context.Context, req federated.BeginRequest) (federated.Redirect, error) {
	if !relayStatePattern.MatchString(req.Nonce) {
		return federated.Redirect{}, errors.New("saml: the sign-in carries no usable nonce for the RelayState")
	}
	acs, err := parseCallbackURL(req.CallbackURL)
	if err != nil {
		return federated.Redirect{}, err
	}
	idp, err := p.metadata.get(ctx)
	if err != nil {
		return federated.Redirect{}, err
	}
	sp := p.serviceProvider(idp, acs)
	sso := sp.GetSSOBindingLocation(crewjam.HTTPRedirectBinding)
	if sso == "" {
		return federated.Redirect{}, fmt.Errorf("%w: saml: the identity provider's metadata has no SingleSignOnService with the HTTP-Redirect binding",
			federated.ErrProviderUnavailable)
	}
	authn, err := sp.MakeAuthenticationRequest(sso, crewjam.HTTPRedirectBinding, crewjam.HTTPPostBinding)
	if err != nil {
		return federated.Redirect{}, fmt.Errorf("saml: build the AuthnRequest: %w", err)
	}
	target, err := authn.Redirect(req.Nonce, sp)
	if err != nil {
		return federated.Redirect{}, fmt.Errorf("saml: encode the AuthnRequest: %w", err)
	}
	return federated.Redirect{
		URL: target.String(),
		State: map[string]string{
			stateRequestID:  authn.ID,
			stateACS:        acs.String(),
			stateRelayState: req.Nonce,
		},
	}, nil
}

// The keys of the per-flow state Begin hands the framework.
const (
	stateRequestID  = "request_id"
	stateACS        = "acs"
	stateRelayState = "relay_state"
)

// maxSAMLResponse bounds the base64 SAMLResponse a callback may carry. A
// signed assertion with a few attributes is a few kilobytes; this leaves
// room for large group lists and refuses anything that is only big.
const maxSAMLResponse = 512 << 10

// Complete implements federated.Provider: the Response is verified and the
// assertion mapped onto the identity.
func (p *Provider) Complete(ctx context.Context, req federated.CompleteRequest) (*backend.User, error) {
	requestID, acsRaw, relay := req.State[stateRequestID], req.State[stateACS], req.State[stateRelayState]
	if requestID == "" || acsRaw == "" || relay == "" {
		return nil, fmt.Errorf("%w: saml: the sign-in's state carries no request to bind the Response to", federated.ErrProviderUnavailable)
	}
	encoded := req.Form.Get("SAMLResponse")
	switch {
	case encoded == "" && req.Query.Get("SAMLResponse") != "":
		return nil, rejected("the Response arrived in the query string; the Web SSO profile answers by HTTP-POST")
	case encoded == "" && (req.Form.Get("SAMLart") != "" || req.Query.Get("SAMLart") != ""):
		return nil, rejected("the artifact binding is not supported")
	case encoded == "":
		return nil, rejected("the callback carried no SAMLResponse")
	case len(encoded) > maxSAMLResponse:
		return nil, rejected(fmt.Sprintf("the SAMLResponse is %d bytes, over the %d this provider reads", len(encoded), maxSAMLResponse))
	}
	// The RelayState is the framework's nonce for this sign-in, echoed by
	// the identity provider (SAML bindings §3.4.3, §3.5.3: it MUST return
	// it unchanged).
	if subtle.ConstantTimeCompare([]byte(req.Form.Get("RelayState")), []byte(relay)) != 1 {
		return nil, rejected("the RelayState does not match this sign-in")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, rejected("the SAMLResponse is not base64")
	}
	acs, err := url.Parse(acsRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: saml: the sign-in's callback URL does not parse", federated.ErrProviderUnavailable)
	}
	idp, err := p.metadata.get(ctx)
	if err != nil {
		return nil, err
	}
	assertion, err := p.verify(idp, acs, raw, requestID)
	if err != nil {
		return nil, err
	}
	return p.identityFrom(assertion)
}

// serviceProvider is the library's service provider for one identity
// provider and one callback URL. It is built per call: it holds no state of
// its own, and the metadata it points at may have been refreshed.
func (p *Provider) serviceProvider(idp *crewjam.EntityDescriptor, acs *url.URL) *crewjam.ServiceProvider {
	return &crewjam.ServiceProvider{
		EntityID:          p.cfg.SPEntityID,
		Key:               p.key,
		Certificate:       p.cert,
		AcsURL:            *acs,
		IDPMetadata:       idp,
		AuthnNameIDFormat: p.nameIDFormat,
		SignatureMethod:   p.sigMethod,
		// Off, and not a setting: a Response that answers no request this
		// application made has nothing to be bound to, and the framework
		// refuses a callback without the state its start route set.
		AllowIDPInitiated:           false,
		ValidateAudienceRestriction: p.checkAudience,
	}
}

// rejected is a certain no about this sign-in: the identity provider's
// answer is not acceptable.
func rejected(why string) error {
	return fmt.Errorf("%w: saml: %s", federated.ErrIdentityRejected, why)
}

// parseCallbackURL checks the callback URL the framework hands Begin.
func parseCallbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("saml: the callback URL %q is not an absolute http(s) URL (is public_base_url set?)", raw)
	}
	return u, nil
}

// parseNameIDFormat resolves the configured NameID format.
func parseNameIDFormat(raw string) (crewjam.NameIDFormat, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "unspecified":
		return crewjam.UnspecifiedNameIDFormat, nil
	case "persistent":
		return crewjam.PersistentNameIDFormat, nil
	case "transient":
		return crewjam.TransientNameIDFormat, nil
	case "email", "emailaddress":
		return crewjam.EmailAddressNameIDFormat, nil
	}
	if strings.HasPrefix(raw, "urn:oasis:names:tc:SAML:") && !strings.ContainsAny(raw, " \t\r\n") {
		return crewjam.NameIDFormat(raw), nil
	}
	return "", fmt.Errorf("saml: name_id_format %q is not persistent, email, transient, unspecified or a urn:oasis:names:tc:SAML URI", raw)
}
