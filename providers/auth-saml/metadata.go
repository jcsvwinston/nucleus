// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package saml

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	crewjam "github.com/crewjam/saml"
	xrv "github.com/mattermost/xml-roundtrip-validator"

	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// maxMetadata bounds a metadata document. One identity provider's metadata
// is a few kilobytes; a federation aggregate can be tens of megabytes, and
// pointing a single service provider at one is a configuration this
// provider refuses rather than downloads.
const maxMetadata = 4 << 20

// metadataSource is where the identity provider's metadata comes from: a
// file read once at boot, or a URL fetched on first use and refreshed.
type metadataSource struct {
	url      string
	entityID string
	client   *http.Client
	refresh  time.Duration

	mu        sync.Mutex
	current   *crewjam.EntityDescriptor
	fetchedAt time.Time
	retryAt   time.Time
}

func newMetadataSource(cfg Config, client *http.Client) (*metadataSource, error) {
	file, link := strings.TrimSpace(cfg.IdPMetadataFile), strings.TrimSpace(cfg.IdPMetadataURL)
	switch {
	case file == "" && link == "":
		return nil, errors.New("saml: one of idp_metadata_url and idp_metadata_file is required (the identity provider's SAML metadata)")
	case file != "" && link != "":
		return nil, errors.New("saml: idp_metadata_url and idp_metadata_file are both set; set one")
	}
	src := &metadataSource{entityID: strings.TrimSpace(cfg.IdPEntityID), refresh: cfg.MetadataRefresh}

	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("saml: read idp_metadata_file: %w", err)
		}
		if len(raw) > maxMetadata {
			return nil, fmt.Errorf("saml: idp_metadata_file is %d bytes, over the %d this provider reads", len(raw), maxMetadata)
		}
		idp, err := parseIdPMetadata(raw, src.entityID, crewjam.TimeNow())
		if err != nil {
			return nil, fmt.Errorf("saml: idp_metadata_file %s: %w", file, err)
		}
		src.current = idp
		return src, nil
	}

	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("saml: idp_metadata_url %q is not an absolute URL", link)
	}
	if err := allowedScheme(u); err != nil {
		return nil, fmt.Errorf("saml: idp_metadata_url: %w", err)
	}
	src.url = u.String()
	// A redirect may not take the fetch from https to plain http.
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return allowedScheme(req.URL)
	}
	src.client = &c
	return src, nil
}

// allowedScheme accepts https anywhere and http for a loopback address only:
// the metadata carries the certificates every signature is checked against,
// so it has to arrive over a channel nobody in between can rewrite.
func allowedScheme(u *url.URL) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("%s is plain http: the identity provider's certificates have to arrive over https (http is accepted for a loopback address only)", u.Redacted())
	}
	return fmt.Errorf("%s: the scheme must be https", u.Redacted())
}

// get returns the identity provider's metadata, fetching it when there is
// none yet or when it is older than the refresh interval. A refresh that
// fails keeps the metadata already loaded, and is tried again a minute
// later; with nothing loaded the error is the provider being unavailable,
// not the person being rejected. Metadata whose validUntil has passed is
// not used at all.
func (m *metadataSource) get(ctx context.Context) (*crewjam.EntityDescriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	expired := m.current != nil && !m.current.ValidUntil.IsZero() && now.After(m.current.ValidUntil)
	if m.url == "" {
		if expired {
			return nil, fmt.Errorf("%w: saml: the identity provider's metadata file expired at %s (validUntil); replace it",
				federated.ErrProviderUnavailable, m.current.ValidUntil.UTC().Format(time.RFC3339))
		}
		return m.current, nil
	}
	if expired {
		// Metadata past its validUntil is not used, not even while a
		// refresh is failing: it is what the identity provider said to
		// stop trusting.
		m.current = nil
	}
	fresh := m.current != nil && now.Sub(m.fetchedAt) < m.refresh
	if fresh || (m.current != nil && now.Before(m.retryAt)) {
		return m.current, nil
	}
	idp, err := m.fetch(ctx)
	if err != nil {
		m.retryAt = now.Add(time.Minute)
		if m.current != nil {
			return m.current, nil
		}
		return nil, fmt.Errorf("%w: saml: identity provider metadata: %v", federated.ErrProviderUnavailable, err)
	}
	m.current, m.fetchedAt, m.retryAt = idp, now, time.Time{}
	return idp, nil
}

func (m *metadataSource) fetch(ctx context.Context) (*crewjam.EntityDescriptor, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/samlmetadata+xml, application/xml, text/xml")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", m.url, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadata+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxMetadata {
		return nil, fmt.Errorf("%s is over the %d bytes this provider reads", m.url, maxMetadata)
	}
	return parseIdPMetadata(raw, m.entityID, crewjam.TimeNow())
}

// parseIdPMetadata reads an EntityDescriptor, or picks one out of an
// EntitiesDescriptor, and checks it describes an identity provider this
// service provider can use: a SingleSignOnService for the HTTP-Redirect
// binding and at least one signing certificate. Metadata whose validUntil
// has passed is refused. The metadata's own signature is not verified: its
// integrity is the channel's (a file the operator placed, or https).
func parseIdPMetadata(raw []byte, entityID string, now time.Time) (*crewjam.EntityDescriptor, error) {
	if err := xrv.Validate(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("the metadata is not XML that survives a round trip: %w", err)
	}
	var candidates []crewjam.EntityDescriptor
	var single crewjam.EntityDescriptor
	if err := xml.Unmarshal(raw, &single); err == nil {
		candidates = append(candidates, single)
	} else {
		var many crewjam.EntitiesDescriptor
		if err2 := xml.Unmarshal(raw, &many); err2 != nil {
			return nil, fmt.Errorf("the document is neither an EntityDescriptor nor an EntitiesDescriptor: %v", err)
		}
		candidates = flattenEntities(many)
	}

	var idps []crewjam.EntityDescriptor
	for _, e := range candidates {
		if len(e.IDPSSODescriptors) == 0 {
			continue
		}
		if entityID != "" && e.EntityID != entityID {
			continue
		}
		idps = append(idps, e)
	}
	switch {
	case len(idps) == 0 && entityID != "":
		return nil, fmt.Errorf("no identity provider with entity ID %q in the metadata", entityID)
	case len(idps) == 0:
		return nil, errors.New("the metadata describes no identity provider (no IDPSSODescriptor)")
	case len(idps) > 1:
		return nil, fmt.Errorf("the metadata describes %d identity providers; set idp_entity_id to the one to use", len(idps))
	}
	idp := idps[0]
	if strings.TrimSpace(idp.EntityID) == "" {
		return nil, errors.New("the identity provider's metadata has no entityID")
	}
	if !idp.ValidUntil.IsZero() && now.After(idp.ValidUntil) {
		return nil, fmt.Errorf("the metadata of %s expired at %s (validUntil)", idp.EntityID, idp.ValidUntil.UTC().Format(time.RFC3339))
	}
	redirect, signing := false, false
	for _, d := range idp.IDPSSODescriptors {
		for _, s := range d.SingleSignOnServices {
			if s.Binding == crewjam.HTTPRedirectBinding && s.Location != "" {
				redirect = true
			}
		}
		for _, k := range d.KeyDescriptors {
			if (k.Use == "" || k.Use == "signing") && len(k.KeyInfo.X509Data.X509Certificates) > 0 {
				signing = true
			}
		}
	}
	if !redirect {
		return nil, fmt.Errorf("the metadata of %s has no SingleSignOnService with the HTTP-Redirect binding", idp.EntityID)
	}
	if !signing {
		return nil, fmt.Errorf("the metadata of %s publishes no signing certificate, so no assertion from it could be verified", idp.EntityID)
	}
	return &idp, nil
}

func flattenEntities(e crewjam.EntitiesDescriptor) []crewjam.EntityDescriptor {
	out := append([]crewjam.EntityDescriptor(nil), e.EntityDescriptors...)
	for _, nested := range e.EntitiesDescriptors {
		out = append(out, flattenEntities(nested)...)
	}
	return out
}

// metadataContentType is the media type of SAML metadata (SAML metadata
// §4.1.1).
const metadataContentType = "application/samlmetadata+xml"

// ServiceMetadata returns this application's service-provider metadata —
// the document the identity provider is configured from — for the callback
// URL the framework serves. FederatedSignIn serves it at
// /auth/<instance>/metadata. The method uses no type of the framework so
// that this module builds against the release it pins.
//
// It advertises what this provider does and nothing else: one
// AssertionConsumerService with the HTTP-POST binding, assertions wanted
// signed, a signing certificate when one is configured, and no encryption
// key (encrypted assertions are refused).
func (p *Provider) ServiceMetadata(_ context.Context, callbackURL string) (string, []byte, error) {
	acs, err := parseCallbackURL(callbackURL)
	if err != nil {
		return "", nil, err
	}
	sp := p.serviceProvider(nil, acs)
	md := sp.Metadata()
	for i := range md.SPSSODescriptors {
		d := &md.SPSSODescriptors[i]
		var keys []crewjam.KeyDescriptor
		for _, k := range d.KeyDescriptors {
			if k.Use == "signing" {
				keys = append(keys, k)
			}
		}
		d.KeyDescriptors = keys
		var acsList []crewjam.IndexedEndpoint
		for _, e := range d.AssertionConsumerServices {
			if e.Binding == crewjam.HTTPPostBinding {
				acsList = append(acsList, e)
			}
		}
		d.AssertionConsumerServices = acsList
		if p.nameIDFormat == crewjam.UnspecifiedNameIDFormat {
			d.NameIDFormats = nil
		}
	}
	body, err := xml.MarshalIndent(md, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("saml: encode the service-provider metadata: %w", err)
	}
	return metadataContentType, append([]byte(xml.Header), body...), nil
}
