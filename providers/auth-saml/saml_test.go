// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package saml_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"

	saml "github.com/jcsvwinston/nucleus/providers/auth-saml"
	"github.com/jcsvwinston/nucleus/providers/auth-saml/samltest"
)

// These tests drive the provider the way the framework does: through
// auth.FederatedSet, which issues and spends the anti-forgery state, against
// a stand-in identity provider that signs with its own key. What reaches
// Complete is an *http.Request carrying the form a browser would post.
//
// They use only what the release this module pins exports, so that the
// module's standalone lane (GOWORK=off) compiles them. The sign-in through
// the framework's own routes, on the api starter, is the catalog bench's
// EN-02.

const (
	appBase  = "https://app.example.test"
	entityID = "https://app.example.test/auth/corp/metadata"
	callback = "https://app.example.test/auth/corp/callback"
)

type harness struct {
	t   *testing.T
	idp *samltest.IdP
	set *auth.FederatedSet
}

// newHarness starts a stand-in identity provider and builds a federated set
// with one SAML instance, "corp", configured from its metadata URL.
func newHarness(t *testing.T, extra map[string]any) *harness {
	t.Helper()
	idp, err := samltest.New()
	if err != nil {
		t.Fatal(err)
	}
	idp.Start()
	t.Cleanup(idp.Close)

	cfg := map[string]any{"sp_entity_id": entityID, "idp_metadata_url": idp.MetadataURL()}
	for k, v := range extra {
		cfg[k] = v
	}
	set, err := auth.NewFederatedSet(auth.FederatedConfig{
		Instances:      []auth.FederatedInstance{{Name: "corp", Provider: saml.ProviderName}},
		ProviderConfig: map[string]map[string]any{"corp": cfg},
		CallbackBase:   appBase,
	})
	if err != nil {
		t.Fatalf("NewFederatedSet: %v", err)
	}
	return &harness{t: t, idp: idp, set: set}
}

// begin starts a sign-in: the AuthnRequest the identity provider receives,
// and the state token the browser's cookie carries.
func (h *harness) begin() (samltest.Request, string) {
	h.t.Helper()
	location, token, err := h.set.Begin(context.Background(), "corp")
	if err != nil {
		h.t.Fatalf("Begin: %v", err)
	}
	if !strings.HasPrefix(location, h.idp.SSOURL()+"?") {
		h.t.Fatalf("Begin redirects to %q, want the identity provider's SSO endpoint %s", location, h.idp.SSOURL())
	}
	req, err := samltest.ParseRedirect(location)
	if err != nil {
		h.t.Fatalf("the redirect does not carry an AuthnRequest: %v", err)
	}
	return req, token
}

// post completes the sign-in with what the browser posts to the callback.
func (h *harness) post(token string, form url.Values) (*backend.User, error) {
	h.t.Helper()
	r := httptest.NewRequest(http.MethodPost, callback, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return h.set.Complete(context.Background(), "corp", token, r)
}

// respond is the identity provider's answer to a fresh sign-in.
func (h *harness) respond(opts samltest.Options) (samltest.Form, string) {
	h.t.Helper()
	req, token := h.begin()
	form, err := h.idp.Respond(req, opts)
	if err != nil {
		h.t.Fatal(err)
	}
	return form, token
}

func wantRejected(t *testing.T, err error, why string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: accepted, want refused", why)
	}
	if !errors.Is(err, federated.ErrIdentityRejected) {
		t.Fatalf("%s: refused with %v, want federated.ErrIdentityRejected", why, err)
	}
	t.Logf("%s: refused: %v", why, err)
}

func TestSignIn_HappyPath(t *testing.T) {
	h := newHarness(t, nil)
	req, token := h.begin()
	if req.ACS != callback {
		t.Errorf("the AuthnRequest asks for the Response at %q, want the callback %q", req.ACS, callback)
	}
	if req.Issuer != entityID {
		t.Errorf("the AuthnRequest's Issuer is %q, want the SP entity ID %q", req.Issuer, entityID)
	}
	if req.RelayState == "" {
		t.Error("the redirect carries no RelayState")
	}
	if req.Signed {
		t.Error("without a key pair the AuthnRequest is signed")
	}
	form, err := h.idp.Respond(req, samltest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	user, err := h.post(token, form.Values())
	if err != nil {
		t.Fatalf("a correct Response was refused: %v", err)
	}
	if user.ID != "samltest-user-1" || user.Username != "samltest-user" || user.Email != "samltest-user@example.test" {
		t.Fatalf("identity %+v, want NameID as ID, uid as username, mail as email", user)
	}
}

// A Response the identity provider signs as a whole AND whose assertion it
// signs too is accepted: both signatures verify.
func TestSignIn_SignedResponseAndAssertion(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{SignResponse: true})
	if _, err := h.post(token, form.Values()); err != nil {
		t.Fatalf("refused: %v", err)
	}
}

func TestSignIn_UnsignedResponseRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{UnsignedAssertion: true})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "a Response with no signature at all")
}

// The library accepts an unsigned assertion inside a signed Response. This
// provider requires the assertion's own signature, which is what its
// metadata says (WantAssertionsSigned).
func TestSignIn_SignedResponseUnsignedAssertionRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{UnsignedAssertion: true, SignResponse: true})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "a signed Response around an unsigned assertion")
}

// A Response that carries a signature of its own must verify, even when the
// assertion inside it does: the Destination, InResponseTo and Status the
// Response states are covered by that signature and nothing else.
func TestSignIn_BrokenResponseSignatureRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{SignResponse: true})
	form = tamper(t, form, func(doc *etree.Document) {
		doc.Root().CreateAttr("Consent", "urn:oasis:names:tc:SAML:2.0:consent:obtained")
	})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "a Response whose own signature no longer verifies")
}

func TestSignIn_SignedByAnotherKeyRefused(t *testing.T) {
	h := newHarness(t, nil)
	req, token := h.begin()
	forger, err := samltest.New()
	if err != nil {
		t.Fatal(err)
	}
	forger.EntityID = h.idp.EntityID // same name, different key
	form, err := forger.Respond(req, samltest.Options{SignResponse: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.post(token, form.Values())
	wantRejected(t, err, "a Response signed by a key the metadata does not publish")
}

func TestSignIn_WrongAudienceRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Audience: "https://another-app.example.test/metadata"})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an assertion for another audience")
}

// The library's own audience check passes an assertion with no
// AudienceRestriction; this provider does not.
func TestSignIn_MissingAudienceRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{OmitAudience: true})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an assertion with no audience restriction")
}

// The recipient is checked in the signed assertion, not only in the
// Response's Destination, which an unsigned Response leaves to anyone.
func TestSignIn_WrongRecipientRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Recipient: "https://another-app.example.test/acs"})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an assertion for another recipient")

	form, token = h.respond(samltest.Options{Destination: "https://another-app.example.test/acs"})
	_, err = h.post(token, form.Values())
	wantRejected(t, err, "a Response sent to another destination")
}

func TestSignIn_ExpiredAssertionRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Now: time.Now().Add(-time.Hour)})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an assertion that expired an hour ago")
}

// Past its NotOnOrAfter by more than the allowed skew, though issued
// recently enough: the validity window, not only the issue delay.
func TestSignIn_ShortLivedAssertionPastItsWindowRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Now: time.Now().Add(-80 * time.Second), Lifetime: -5 * time.Minute})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an assertion past NotOnOrAfter plus the clock skew")
}

func TestSignIn_NotYetValidRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Now: time.Now().Add(time.Hour)})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an assertion valid from an hour from now")
}

// A clock a minute apart is inside the skew and accepted.
func TestSignIn_SmallClockSkewAccepted(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Now: time.Now().Add(time.Minute)})
	if _, err := h.post(token, form.Values()); err != nil {
		t.Fatalf("an identity provider a minute ahead was refused: %v", err)
	}
}

func TestSignIn_InResponseToAnotherRequestRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{InResponseTo: "id-not-the-request-this-application-sent"})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "a Response to another request")
}

// Identity-provider-initiated sign-in is off: a Response that answers no
// request is refused even with a state the framework issued.
func TestSignIn_UnsolicitedResponseRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{Unsolicited: true})
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "an unsolicited (IdP-initiated) Response")
}

func TestSignIn_RelayStateMismatchRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{})
	form.RelayState = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_, err := h.post(token, form.Values())
	wantRejected(t, err, "a RelayState that is not this sign-in's")
}

// A replayed Response is refused three ways: with the state the first
// callback spent (the framework), in a new sign-in it does not answer
// (InResponseTo), and — with the framework's custody bypassed — by the
// provider's own memory of the assertion.
func TestSignIn_ReplayRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{})
	if _, err := h.post(token, form.Values()); err != nil {
		t.Fatalf("the first sign-in was refused: %v", err)
	}

	if _, err := h.post(token, form.Values()); err == nil {
		t.Fatal("the same Response with the same state signed in twice")
	}

	// A new sign-in, whose RelayState the attacker knows because they
	// started it: only InResponseTo is left to tell the two apart.
	next, fresh := h.begin()
	form.RelayState = next.RelayState
	_, err := h.post(fresh, form.Values())
	wantRejected(t, err, "a captured Response posted into a new sign-in")
	if !strings.Contains(err.Error(), "InResponseTo") {
		t.Fatalf("the captured Response was refused for another reason than the request it answers: %v", err)
	}
}

func TestProvider_RemembersAnAssertionItAccepted(t *testing.T) {
	idp, err := samltest.New()
	if err != nil {
		t.Fatal(err)
	}
	idp.Start()
	t.Cleanup(idp.Close)
	factory, ok := federated.Lookup(saml.ProviderName)
	if !ok {
		t.Fatal("the blank import did not register saml")
	}
	p, err := factory(backend.Config{Name: "corp", ProviderConfig: map[string]any{"sp_entity_id": entityID, "idp_metadata_url": idp.MetadataURL()}})
	if err != nil {
		t.Fatal(err)
	}
	nonce := "bm9uY2Utbm9uY2Utbm9uY2Utbm9uY2Utbm9uY2U"
	red, err := p.Begin(context.Background(), federated.BeginRequest{CallbackURL: callback, Nonce: nonce})
	if err != nil {
		t.Fatal(err)
	}
	req, err := samltest.ParseRedirect(red.URL)
	if err != nil {
		t.Fatal(err)
	}
	form, err := idp.Respond(req, samltest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	complete := federated.CompleteRequest{Form: form.Values(), Query: url.Values{}, State: red.State, Nonce: nonce}
	if _, err := p.Complete(context.Background(), complete); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	_, err = p.Complete(context.Background(), complete)
	wantRejected(t, err, "the same assertion completed twice with the same state")
}

func TestSignIn_ResponseOutsideThePostBindingRefused(t *testing.T) {
	h := newHarness(t, nil)
	form, token := h.respond(samltest.Options{})

	r := httptest.NewRequest(http.MethodGet, callback+"?"+form.Values().Encode(), nil)
	_, err := h.set.Complete(context.Background(), "corp", token, r)
	wantRejected(t, err, "a Response in the query string")

	form, token = h.respond(samltest.Options{})
	_, err = h.post(token, url.Values{"SAMLart": {"AAQAAMFbLinlXaCM+FIxiDwGOLAy2T71gbpO7ZhNzAgEANlB90ECfpNEVLg="}, "RelayState": {form.RelayState}})
	wantRejected(t, err, "an artifact")

	_, token = h.respond(samltest.Options{})
	_, err = h.post(token, url.Values{})
	wantRejected(t, err, "a callback with no SAMLResponse")
}

// The library leaves these to the caller: it accepts an assertion with no
// NameID or no SubjectConfirmation, and it dereferences Conditions without
// checking it is there. Each is signed by the identity provider and still
// refused.
func TestSignIn_IncompleteAssertionRefused(t *testing.T) {
	for name, opts := range map[string]samltest.Options{
		"no NameID":              {OmitNameID: true},
		"no SubjectConfirmation": {OmitSubjectConfirmation: true},
		"no Conditions":          {OmitConditions: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			form, token := h.respond(opts)
			_, err := h.post(token, form.Values())
			wantRejected(t, err, "a signed assertion with "+name)
		})
	}
}

// Two assertions the identity provider signed, both answering this
// sign-in: the library would take the first that verifies. One Response
// carries one assertion here, so which one signs in is never a question.
func TestSignIn_TwoSignedAssertionsRefused(t *testing.T) {
	h := newHarness(t, nil)
	req, token := h.begin()
	first, err := h.idp.Respond(req, samltest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h.idp.User.NameID = "somebody-else"
	second, err := h.idp.Respond(req, samltest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := second.XML()
	if err != nil {
		t.Fatal(err)
	}
	other := etree.NewDocument()
	if err := other.ReadFromBytes(raw); err != nil {
		t.Fatal(err)
	}
	extra := assertionOf(t, other).Copy()
	form := tamper(t, first, func(doc *etree.Document) { doc.Root().AddChild(extra) })
	_, err = h.post(token, form.Values())
	wantRejected(t, err, "a Response with two signed assertions")
}

// ---- signature wrapping -----------------------------------------------------

// tamper decodes the Response, lets edit change it, and re-encodes it.
func tamper(t *testing.T, form samltest.Form, edit func(doc *etree.Document)) samltest.Form {
	t.Helper()
	raw, err := form.XML()
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	out, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return form.WithXML(out)
}

func assertionOf(t *testing.T, doc *etree.Document) *etree.Element {
	t.Helper()
	return assertionIn(t, doc.Root())
}

func assertionIn(t *testing.T, response *etree.Element) *etree.Element {
	t.Helper()
	for _, el := range response.ChildElements() {
		if el.Tag == "Assertion" {
			return el
		}
	}
	t.Fatal("no assertion in the Response")
	return nil
}

func setNameID(t *testing.T, assertion *etree.Element, value string) {
	t.Helper()
	nameID := assertion.FindElement("./Subject/NameID")
	if nameID == nil {
		t.Fatal("no NameID")
	}
	nameID.SetText(value)
}

// The classic wrapping attacks, each against a correctly signed Response:
// the signed content is changed, or a second assertion is placed where a
// reader might take it while the verifier checks the original.
func TestSignIn_SignatureWrappingRefused(t *testing.T) {
	cases := []struct {
		name string
		opts samltest.Options
		edit func(t *testing.T, doc *etree.Document)
	}{
		{
			name: "the signed assertion's subject is rewritten",
			edit: func(t *testing.T, doc *etree.Document) {
				setNameID(t, assertionOf(t, doc), "admin")
			},
		},
		{
			name: "an unsigned assertion is placed before the signed one",
			edit: func(t *testing.T, doc *etree.Document) {
				signed := assertionOf(t, doc)
				evil := signed.Copy()
				evil.CreateAttr("ID", "id-evil")
				if sig := evil.FindElement("./Signature"); sig != nil {
					evil.RemoveChild(sig)
				}
				setNameID(t, evil, "admin")
				doc.Root().InsertChildAt(signed.Index(), evil)
			},
		},
		{
			name: "the signed assertion is hidden inside an evil one carrying its signature",
			edit: func(t *testing.T, doc *etree.Document) {
				signed := assertionOf(t, doc)
				evil := signed.Copy()
				setNameID(t, evil, "admin")
				// The evil assertion keeps the original's ID and Signature;
				// the original moves into its Subject.
				evil.FindElement("./Subject").AddChild(signed.Copy())
				doc.Root().RemoveChild(signed)
				doc.Root().AddChild(evil)
			},
		},
		{
			name: "the whole signed Response is wrapped in an evil one",
			opts: samltest.Options{SignResponse: true},
			edit: func(t *testing.T, doc *etree.Document) {
				original := doc.Root().Copy()
				evil := doc.Root().Copy()
				evil.CreateAttr("ID", "id-evil-response")
				setNameID(t, assertionIn(t, evil), "admin")
				sig := evil.FindElement("./Signature")
				if sig == nil {
					t.Fatal("the Response is not signed")
				}
				object := sig.CreateElement("ds:Object")
				object.AddChild(original)
				doc.SetRoot(evil)
			},
		},
		{
			name: "an element repeats the signed assertion's ID",
			edit: func(t *testing.T, doc *etree.Document) {
				signed := assertionOf(t, doc)
				ext := doc.Root().CreateElement("samlp:Extensions")
				dup := ext.CreateElement("Evil")
				dup.CreateAttr("ID", signed.SelectAttrValue("ID", ""))
				doc.Root().InsertChildAt(signed.Index(), ext)
			},
		},
		{
			name: "the assertion is replaced by an EncryptedAssertion",
			edit: func(t *testing.T, doc *etree.Document) {
				signed := assertionOf(t, doc)
				enc := etree.NewElement("saml:EncryptedAssertion")
				enc.CreateElement("xenc:EncryptedData").CreateAttr("xmlns:xenc", "http://www.w3.org/2001/04/xmlenc#")
				doc.Root().InsertChildAt(signed.Index(), enc)
				doc.Root().RemoveChild(signed)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			form, token := h.respond(c.opts)
			// The untouched Response would sign in; the tampered one must not.
			_, err := h.post(token, tamper(t, form, func(doc *etree.Document) { c.edit(t, doc) }).Values())
			wantRejected(t, err, c.name)
		})
	}
}

// An XML comment inside a signed NameID does not change the digest
// (exclusive canonicalization drops comments). A reader that took only the
// first text node would see a shorter name than the one the identity
// provider signed (the 2018 comment-truncation class); this one reads the
// whole value.
func TestSignIn_CommentInsideTheNameIDDoesNotTruncateIt(t *testing.T) {
	h := newHarness(t, nil)
	h.idp.User.NameID = "admin@example.test.attacker.example"
	form, token := h.respond(samltest.Options{})
	form = tamper(t, form, func(doc *etree.Document) {
		nameID := assertionOf(t, doc).FindElement("./Subject/NameID")
		nameID.SetText("admin@example.test")
		nameID.CreateComment("")
		nameID.CreateText(".attacker.example")
	})
	user, err := h.post(token, form.Values())
	if err != nil {
		// Refusing it is as good as reading it whole.
		wantRejected(t, err, "a NameID with a comment inside")
		return
	}
	if user.ID != "admin@example.test.attacker.example" {
		t.Fatalf("the NameID with a comment inside was read as %q", user.ID)
	}
}
