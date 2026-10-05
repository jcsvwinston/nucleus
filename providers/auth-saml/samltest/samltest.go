// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package samltest is a SAML 2.0 identity provider for tests, in the spirit
// of net/http/httptest: it generates its own key and certificate, publishes
// metadata, answers an AuthnRequest sent with the HTTP-Redirect binding with
// a signed Response, and can be told to answer wrongly — an unsigned
// assertion, another audience, an expired window — so that an application
// can check that it refuses what it must.
//
// It signs with github.com/russellhaering/goxmldsig, the library the
// service provider verifies with, and builds the documents with the types
// of github.com/crewjam/saml. It is not an identity provider for anything
// but tests: it signs in whoever asks, as the User it is given.
//
//	idp, _ := samltest.New()
//	idp.Start()
//	defer idp.Close()
//	// auth.corp.idp_metadata_url: idp.MetadataURL()
package samltest

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

// User is who the identity provider signs in.
type User struct {
	// NameID is the subject. NameIDFormat defaults to persistent.
	NameID       string
	NameIDFormat string
	// Attributes are sent by Name, each with its values.
	Attributes map[string][]string
}

// IdP is the stand-in identity provider.
type IdP struct {
	// EntityID is the identity provider's entity ID. Start sets it to the
	// metadata URL when it is empty.
	EntityID string
	// User is who every sign-in is answered for.
	User User

	key  *rsa.PrivateKey
	cert *x509.Certificate

	mu  sync.Mutex
	srv *httptest.Server
}

// New generates a 2048-bit RSA key and a self-signed certificate valid
// from an hour ago for a day, and a default user.
func New() (*IdP, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "samltest identity provider"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &IdP{
		key:  key,
		cert: cert,
		User: User{
			NameID: "samltest-user-1",
			Attributes: map[string][]string{
				"uid":  {"samltest-user"},
				"mail": {"samltest-user@example.test"},
			},
		},
	}, nil
}

// Start serves the identity provider on a loopback address: its metadata at
// /metadata and its single sign-on endpoint at /sso.
func (p *IdP) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		return
	}
	p.srv = httptest.NewServer(p.Handler())
	if p.EntityID == "" {
		p.EntityID = p.srv.URL + "/metadata"
	}
}

// Close stops the server Start started.
func (p *IdP) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		p.srv.Close()
	}
}

// URL is the base URL of the started server.
func (p *IdP) URL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv == nil {
		return ""
	}
	return p.srv.URL
}

// MetadataURL is where the metadata is served.
func (p *IdP) MetadataURL() string { return p.URL() + "/metadata" }

// SSOURL is the single sign-on endpoint the metadata names.
func (p *IdP) SSOURL() string { return p.URL() + "/sso" }

// Certificate is the signing certificate the metadata publishes.
func (p *IdP) Certificate() *x509.Certificate { return p.cert }

// Metadata is the identity provider's metadata: its entity ID, its signing
// certificate and an HTTP-Redirect single sign-on endpoint.
func (p *IdP) Metadata() ([]byte, error) {
	md := crewjam.EntityDescriptor{
		EntityID: p.EntityID,
		IDPSSODescriptors: []crewjam.IDPSSODescriptor{{
			SSODescriptor: crewjam.SSODescriptor{
				RoleDescriptor: crewjam.RoleDescriptor{
					ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
					KeyDescriptors: []crewjam.KeyDescriptor{{
						Use: "signing",
						KeyInfo: crewjam.KeyInfo{X509Data: crewjam.X509Data{X509Certificates: []crewjam.X509Certificate{
							{Data: base64.StdEncoding.EncodeToString(p.cert.Raw)},
						}}},
					}},
				},
				NameIDFormats: []crewjam.NameIDFormat{crewjam.PersistentNameIDFormat},
			},
			SingleSignOnServices: []crewjam.Endpoint{{Binding: crewjam.HTTPRedirectBinding, Location: p.SSOURL()}},
		}},
	}
	body, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// Handler serves /metadata and /sso. /sso answers an AuthnRequest with the
// page a browser posts to the service provider: an HTML form, submitted on
// load, carrying SAMLResponse and RelayState.
func (p *IdP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metadata", func(w http.ResponseWriter, _ *http.Request) {
		body, err := p.Metadata()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/sso", func(w http.ResponseWriter, r *http.Request) {
		req, err := ParseRedirect(r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		form, err := p.Respond(req, Options{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = postPage.Execute(w, form)
	})
	return mux
}

var postPage = template.Must(template.New("post").Parse(`<!doctype html>
<html><body onload="document.forms[0].submit()">
<form method="post" action="{{.ACS}}">
<input type="hidden" name="SAMLResponse" value="{{.SAMLResponse}}">
<input type="hidden" name="RelayState" value="{{.RelayState}}">
<noscript><button type="submit">Continue</button></noscript>
</form></body></html>
`))

// Request is what the identity provider read from an AuthnRequest.
type Request struct {
	ID         string // the AuthnRequest's ID, which InResponseTo answers
	Issuer     string // the service provider's entity ID
	ACS        string // AssertionConsumerServiceURL
	RelayState string
	// Signed reports whether the redirect carried SigAlg and Signature.
	// The signature itself is not verified here.
	Signed bool
	// SignedQuery is the part of the query a redirect-binding signature
	// covers (SAML bindings §3.4.4.1), and Signature its decoded value.
	SignedQuery string
	SigAlg      string
	Signature   []byte
}

// ParseRedirect reads an AuthnRequest sent with the HTTP-Redirect binding:
// the URL the service provider's start route redirects the browser to.
func ParseRedirect(location string) (Request, error) {
	u, err := url.Parse(location)
	if err != nil {
		return Request{}, err
	}
	q := u.Query()
	encoded := q.Get("SAMLRequest")
	if encoded == "" {
		return Request{}, errors.New("samltest: the URL carries no SAMLRequest")
	}
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Request{}, fmt.Errorf("samltest: SAMLRequest is not base64: %w", err)
	}
	inflated, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(compressed)), 1<<20))
	if err != nil {
		return Request{}, fmt.Errorf("samltest: SAMLRequest does not inflate: %w", err)
	}
	var authn crewjam.AuthnRequest
	if err := xml.Unmarshal(inflated, &authn); err != nil {
		return Request{}, fmt.Errorf("samltest: SAMLRequest is not an AuthnRequest: %w", err)
	}
	req := Request{ID: authn.ID, ACS: authn.AssertionConsumerServiceURL, RelayState: q.Get("RelayState")}
	if authn.Issuer != nil {
		req.Issuer = authn.Issuer.Value
	}
	if sigAlg, sig := q.Get("SigAlg"), q.Get("Signature"); sigAlg != "" && sig != "" {
		req.Signed, req.SigAlg = true, sigAlg
		req.Signature, _ = base64.StdEncoding.DecodeString(sig)
		// The signed octets are the raw query as sent, without Signature.
		for _, part := range bytes.Split([]byte(u.RawQuery), []byte("&")) {
			if bytes.HasPrefix(part, []byte("Signature=")) {
				continue
			}
			if req.SignedQuery != "" {
				req.SignedQuery += "&"
			}
			req.SignedQuery += string(part)
		}
	}
	if req.ID == "" || req.ACS == "" || req.Issuer == "" {
		return Request{}, errors.New("samltest: the AuthnRequest has no ID, AssertionConsumerServiceURL or Issuer")
	}
	return req, nil
}

// Options make a Response wrong in one way. The zero value is a Response a
// correct service provider accepts.
type Options struct {
	// Now is the identity provider's clock; zero is time.Now().
	Now time.Time
	// Lifetime is how long the assertion is valid from Now; zero is five
	// minutes.
	Lifetime time.Duration
	// Audience replaces the audience (the AuthnRequest's Issuer);
	// OmitAudience sends no AudienceRestriction at all.
	Audience     string
	OmitAudience bool
	// Recipient replaces the confirmation's recipient and Destination the
	// Response's (both the AuthnRequest's AssertionConsumerServiceURL);
	// InResponseTo replaces the request ID the Response and the
	// confirmation answer.
	Recipient    string
	Destination  string
	InResponseTo string
	// Unsolicited sends what an identity-provider-initiated sign-in
	// sends: no InResponseTo at all.
	Unsolicited bool
	// OmitNameID, OmitSubjectConfirmation and OmitConditions leave out an
	// element the Web SSO profile requires, signed all the same.
	OmitNameID              bool
	OmitSubjectConfirmation bool
	OmitConditions          bool
	// UnsignedAssertion sends the assertion without a signature;
	// SignResponse signs the Response element too.
	UnsignedAssertion bool
	SignResponse      bool
}

// Form is what the browser posts to the service provider.
type Form struct {
	ACS          string
	SAMLResponse string
	RelayState   string
}

// Values is the form body.
func (f Form) Values() url.Values {
	return url.Values{"SAMLResponse": {f.SAMLResponse}, "RelayState": {f.RelayState}}
}

// XML decodes the Response, for a test that tampers with it.
func (f Form) XML() ([]byte, error) { return base64.StdEncoding.DecodeString(f.SAMLResponse) }

// WithXML is the form carrying another Response document.
func (f Form) WithXML(doc []byte) Form {
	f.SAMLResponse = base64.StdEncoding.EncodeToString(doc)
	return f
}

// Respond answers an AuthnRequest with a Response for the identity
// provider's User: one assertion, signed unless Options says otherwise.
func (p *IdP) Respond(req Request, opts Options) (Form, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	lifetime := opts.Lifetime
	if lifetime == 0 {
		lifetime = 5 * time.Minute
	}
	audience := req.Issuer
	if opts.Audience != "" {
		audience = opts.Audience
	}
	recipient, destination := req.ACS, req.ACS
	if opts.Recipient != "" {
		recipient = opts.Recipient
	}
	if opts.Destination != "" {
		destination = opts.Destination
	}
	inResponseTo := req.ID
	if opts.InResponseTo != "" {
		inResponseTo = opts.InResponseTo
	}
	if opts.Unsolicited {
		inResponseTo = ""
	}
	format := p.User.NameIDFormat
	if format == "" {
		format = string(crewjam.PersistentNameIDFormat)
	}

	assertion := &crewjam.Assertion{
		ID:           newID(),
		IssueInstant: now,
		Version:      "2.0",
		Issuer:       crewjam.Issuer{Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity", Value: p.EntityID},
		Subject: &crewjam.Subject{
			NameID: &crewjam.NameID{Format: format, Value: p.User.NameID},
			SubjectConfirmations: []crewjam.SubjectConfirmation{{
				Method: "urn:oasis:names:tc:SAML:2.0:cm:bearer",
				SubjectConfirmationData: &crewjam.SubjectConfirmationData{
					InResponseTo: inResponseTo,
					NotOnOrAfter: now.Add(lifetime),
					Recipient:    recipient,
				},
			}},
		},
		Conditions: &crewjam.Conditions{NotBefore: now.Add(-5 * time.Second), NotOnOrAfter: now.Add(lifetime)},
		AuthnStatements: []crewjam.AuthnStatement{{
			AuthnInstant: now,
			SessionIndex: newID(),
			AuthnContext: crewjam.AuthnContext{AuthnContextClassRef: &crewjam.AuthnContextClassRef{
				Value: "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport",
			}},
		}},
	}
	if !opts.OmitAudience {
		assertion.Conditions.AudienceRestrictions = []crewjam.AudienceRestriction{{Audience: crewjam.Audience{Value: audience}}}
	}
	if opts.OmitNameID {
		assertion.Subject.NameID = nil
	}
	if opts.OmitSubjectConfirmation {
		assertion.Subject.SubjectConfirmations = nil
	}
	if opts.OmitConditions {
		assertion.Conditions = nil
	}
	if len(p.User.Attributes) > 0 {
		var attrs []crewjam.Attribute
		for name, values := range p.User.Attributes {
			attr := crewjam.Attribute{Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic"}
			for _, v := range values {
				attr.Values = append(attr.Values, crewjam.AttributeValue{Type: "xs:string", Value: v})
			}
			attrs = append(attrs, attr)
		}
		assertion.AttributeStatements = []crewjam.AttributeStatement{{Attributes: attrs}}
	}

	signer, err := p.signingContext()
	if err != nil {
		return Form{}, err
	}
	assertionEl := assertion.Element()
	if !opts.UnsignedAssertion {
		signed, err := signer.SignEnveloped(assertionEl)
		if err != nil {
			return Form{}, fmt.Errorf("samltest: sign the assertion: %w", err)
		}
		assertion.Signature = signed.ChildElements()[len(signed.ChildElements())-1]
		assertionEl = assertion.Element()
	}

	response := &crewjam.Response{
		ID:           newID(),
		InResponseTo: inResponseTo,
		Version:      "2.0",
		IssueInstant: now,
		Destination:  destination,
		Issuer:       &crewjam.Issuer{Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity", Value: p.EntityID},
		Status:       crewjam.Status{StatusCode: crewjam.StatusCode{Value: crewjam.StatusSuccess}},
	}
	responseEl := response.Element()
	responseEl.AddChild(assertionEl)
	if opts.SignResponse {
		signed, err := signer.SignEnveloped(responseEl)
		if err != nil {
			return Form{}, fmt.Errorf("samltest: sign the response: %w", err)
		}
		response.Signature = signed.ChildElements()[len(signed.ChildElements())-1]
		responseEl = response.Element()
		responseEl.AddChild(assertionEl)
	}

	doc := etree.NewDocument()
	doc.SetRoot(responseEl)
	raw, err := doc.WriteToBytes()
	if err != nil {
		return Form{}, err
	}
	return Form{ACS: req.ACS, SAMLResponse: base64.StdEncoding.EncodeToString(raw), RelayState: req.RelayState}, nil
}

// SignElement signs an element the way the identity provider signs an
// assertion — for a test that builds a document of its own.
func (p *IdP) SignElement(el *etree.Element) (*etree.Element, error) {
	signer, err := p.signingContext()
	if err != nil {
		return nil, err
	}
	return signer.SignEnveloped(el)
}

func (p *IdP) signingContext() (*dsig.SigningContext, error) {
	store := dsig.TLSCertKeyStore(tls.Certificate{Certificate: [][]byte{p.cert.Raw}, PrivateKey: p.key, Leaf: p.cert})
	ctx := dsig.NewDefaultSigningContext(store)
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		return nil, err
	}
	return ctx, nil
}

func newID() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return "id-" + hex.EncodeToString(b)
}
