// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package saml

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	xrv "github.com/mattermost/xml-roundtrip-validator"
)

// The XML namespaces a Response is read in.
const (
	nsProtocol  = "urn:oasis:names:tc:SAML:2.0:protocol"
	nsAssertion = "urn:oasis:names:tc:SAML:2.0:assertion"
	nsDSig      = "http://www.w3.org/2000/09/xmldsig#"

	bearerMethod = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
)

// verify decides whether a decoded Response is a sign-in this application
// accepts, and returns the assertion when it is.
//
// The signature work is the library's (crewjam/saml over goxmldsig): this
// function never verifies a signature itself. What it adds is policy the
// library leaves open, applied in three steps:
//
//  1. Shape, before any signature is looked at: one Response, exactly one
//     plaintext Assertion in the whole document, no EncryptedAssertion, and
//     no two elements sharing an ID. The signature-wrapping attacks hide a
//     second assertion or a second element with the signed ID somewhere
//     the verifier and the reader disagree about; a document that has
//     neither leaves them nothing to work with.
//  2. The library's verification, twice. The first pass is the document as
//     it came: a signature on the Response, if there is one, must verify.
//     The library accepts an unsigned assertion inside a signed Response;
//     the second pass removes the Response's own signature, so the library
//     requires — and verifies — the assertion's. Both must succeed, and
//     they must return the same assertion.
//  3. What the library checks only when the element is present: an
//     audience restriction naming this application (checkAudience, called
//     by the library), a subject with a NameID, and a bearer confirmation
//     bound to this sign-in's request and callback. Then the replay cache.
func (p *Provider) verify(idp *crewjam.EntityDescriptor, acs *url.URL, raw []byte, requestID string) (assertion *crewjam.Assertion, err error) {
	assertionID, unsignedResponse, err := checkShape(raw)
	if err != nil {
		return nil, err
	}

	sp := p.serviceProvider(idp, acs)
	first, err := parseGuarded(sp, raw, requestID, *acs)
	if err != nil {
		return nil, err
	}
	second, err := parseGuarded(sp, unsignedResponse, requestID, *acs)
	if err != nil {
		return nil, err
	}
	if first.ID != assertionID || second.ID != assertionID {
		return nil, rejected("the verified assertion is not the one the Response carries")
	}

	if err := checkSubject(second, requestID, acs.String()); err != nil {
		return nil, err
	}
	if !p.replay.claim(idp.EntityID, second) {
		return nil, rejected(fmt.Sprintf("assertion %s was already used to sign in", second.ID))
	}
	return second, nil
}

// checkShape is step 1 of verify. It returns the ID of the one assertion
// and the document with the Response's own signature removed, for the
// second pass.
func checkShape(raw []byte) (string, []byte, error) {
	if err := xrv.Validate(bytes.NewReader(raw)); err != nil {
		return "", nil, rejected("the Response is not XML that survives a round trip: " + err.Error())
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return "", nil, rejected("the Response does not parse: " + err.Error())
	}
	root := doc.Root()
	if root == nil || root.Tag != "Response" || root.NamespaceURI() != nsProtocol {
		return "", nil, rejected("the document is not a samlp:Response")
	}

	var assertions, encrypted []*etree.Element
	ids := map[string]bool{}
	var walk func(el *etree.Element) error
	walk = func(el *etree.Element) error {
		if id := el.SelectAttrValue("ID", ""); id != "" {
			if ids[id] {
				return rejected(fmt.Sprintf("two elements carry the ID %q", id))
			}
			ids[id] = true
		}
		if el.NamespaceURI() == nsAssertion {
			switch el.Tag {
			case "Assertion":
				assertions = append(assertions, el)
			case "EncryptedAssertion":
				encrypted = append(encrypted, el)
			}
		}
		for _, child := range el.ChildElements() {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return "", nil, err
	}

	switch {
	case len(encrypted) > 0:
		return "", nil, rejected("encrypted assertions are not supported: configure the identity provider to sign the assertion and not encrypt it")
	case len(assertions) == 0:
		return "", nil, rejected("the Response carries no assertion")
	case len(assertions) > 1:
		return "", nil, rejected(fmt.Sprintf("the Response carries %d assertions; exactly one is accepted", len(assertions)))
	}
	assertion := assertions[0]
	if assertion.Parent() != root {
		return "", nil, rejected("the assertion is not a child of the Response")
	}
	id := assertion.SelectAttrValue("ID", "")
	if id == "" {
		return "", nil, rejected("the assertion has no ID")
	}

	// The second pass's document: the Response without its own signature.
	// Nothing else changes, so the assertion's signature — computed over
	// the assertion alone — still verifies when, and only when, it is valid.
	stripped := doc.Copy()
	for _, child := range stripped.Root().ChildElements() {
		if child.Tag == "Signature" && child.NamespaceURI() == nsDSig {
			stripped.Root().RemoveChild(child)
		}
	}
	unsigned, err := stripped.WriteToBytes()
	if err != nil {
		return "", nil, fmt.Errorf("saml: re-encode the Response: %w", err)
	}
	return id, unsigned, nil
}

// parseGuarded runs the library's verification. The library dereferences
// optional elements of a verified assertion without checking them (an
// assertion with no Subject or no Conditions panics it), so a panic is
// recovered here and is a rejection: it can only follow a signature that
// verified, and it is still not a sign-in this provider accepts.
func parseGuarded(sp *crewjam.ServiceProvider, raw []byte, requestID string, acs url.URL) (assertion *crewjam.Assertion, err error) {
	defer func() {
		if r := recover(); r != nil {
			assertion, err = nil, rejected(fmt.Sprintf("the assertion is missing an element the profile requires (%v)", r))
		}
	}()
	assertion, err = sp.ParseXMLResponse(raw, []string{requestID}, acs)
	if err != nil {
		var invalid *crewjam.InvalidResponseError
		if errors.As(err, &invalid) && invalid.PrivateErr != nil {
			// The library's Error() is a fixed string on purpose; the
			// detail is for the operator's log, never the browser.
			return nil, rejected(invalid.PrivateErr.Error())
		}
		var status crewjam.ErrBadStatus
		if errors.As(err, &status) {
			return nil, rejected("the identity provider answered " + status.Status)
		}
		return nil, rejected(err.Error())
	}
	if assertion == nil {
		return nil, rejected("the library returned no assertion")
	}
	return assertion, nil
}

// checkAudience is the library's audience hook. Its default accepts an
// assertion with no AudienceRestriction at all; this one requires at least
// one, and every one of them to name this application (SAML core §2.5.1.4:
// each restriction is evaluated independently).
func (p *Provider) checkAudience(a *crewjam.Assertion) error {
	if a.Conditions == nil || len(a.Conditions.AudienceRestrictions) == 0 {
		return errors.New("the assertion has no AudienceRestriction")
	}
	for _, r := range a.Conditions.AudienceRestrictions {
		if r.Audience.Value != p.cfg.SPEntityID {
			return fmt.Errorf("the assertion's audience is %q, not this application's entity ID", r.Audience.Value)
		}
	}
	return nil
}

// checkSubject is step 3 of verify for what the library checks only when
// it is there: it validates each SubjectConfirmation present, and accepts
// an assertion that has none.
func checkSubject(a *crewjam.Assertion, requestID, acs string) error {
	if a.Subject == nil || a.Subject.NameID == nil || a.Subject.NameID.Value == "" {
		return rejected("the assertion names no subject (no NameID)")
	}
	if a.Conditions == nil || a.Conditions.NotOnOrAfter.IsZero() {
		return rejected("the assertion's Conditions carry no NotOnOrAfter")
	}
	for _, c := range a.Subject.SubjectConfirmations {
		d := c.SubjectConfirmationData
		if c.Method == bearerMethod && d != nil && d.InResponseTo == requestID && d.Recipient == acs && !d.NotOnOrAfter.IsZero() {
			return nil
		}
	}
	return rejected("the assertion has no bearer SubjectConfirmation bound to this sign-in's request and callback")
}

// replayCache remembers the assertions that signed somebody in until they
// expire. The framework already spends a sign-in's state on its first
// callback and the Response must answer that sign-in's request, so a
// replay has to defeat both before it reaches this; the cache is what the
// Web SSO profile asks of a service provider regardless (SAML profiles
// §4.1.4.5), and it holds when the custody around the provider does not.
//
// It is per process. Several replicas behind a load balancer each keep
// their own, as they each keep their own pending sign-ins.
type replayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// maxReplayEntries bounds the cache. When it is full of assertions that
// have not expired, a new one is refused rather than an old one forgotten.
const maxReplayEntries = 100_000

func newReplayCache() *replayCache { return &replayCache{seen: map[string]time.Time{}} }

// claim records the assertion and reports whether it was new.
func (c *replayCache) claim(issuer string, a *crewjam.Assertion) bool {
	now := crewjam.TimeNow()
	expires := a.Conditions.NotOnOrAfter
	for _, sc := range a.Subject.SubjectConfirmations {
		if d := sc.SubjectConfirmationData; d != nil && d.NotOnOrAfter.After(expires) {
			expires = d.NotOnOrAfter
		}
	}
	expires = expires.Add(crewjam.MaxClockSkew)
	key := issuer + "\x00" + a.ID

	c.mu.Lock()
	defer c.mu.Unlock()
	if until, ok := c.seen[key]; ok && now.Before(until) {
		return false
	}
	if len(c.seen) >= maxReplayEntries {
		for k, until := range c.seen {
			if !now.Before(until) {
				delete(c.seen, k)
			}
		}
		if len(c.seen) >= maxReplayEntries {
			return false
		}
	}
	c.seen[key] = expires
	return true
}
