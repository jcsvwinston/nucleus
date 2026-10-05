// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package saml

import (
	"strings"

	crewjam "github.com/crewjam/saml"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// The attribute names identity providers commonly use, looked for when the
// configuration names none: the short names (Shibboleth's FriendlyName,
// Keycloak's and Okta's defaults), the OIDs of the eduPerson/inetOrgPerson
// profile, and the claim URIs of ADFS and Microsoft Entra ID.
var (
	defaultEmailAttributes = []string{
		"email", "mail", "emailAddress",
		"urn:oid:0.9.2342.19200300.100.1.3",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	}
	defaultUsernameAttributes = []string{
		"username", "uid", "preferred_username",
		"urn:oid:0.9.2342.19200300.100.1.1",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
	}
)

// identityFrom maps a verified assertion onto the framework's identity, the
// same shape the OIDC provider returns, so the callback records it under the
// same session keys:
//
//   - ID is the NameID — the subject the identity provider asserts.
//   - Email is the email attribute, or the NameID when its format is
//     emailAddress and no attribute carries one.
//   - Username is the username attribute, then the email, then the NameID.
//   - Roles are the values of the role attribute, when one is configured.
func (p *Provider) identityFrom(a *crewjam.Assertion) (*backend.User, error) {
	nameID := strings.TrimSpace(a.Subject.NameID.Value)
	if nameID == "" {
		return nil, rejected("the assertion's NameID is empty")
	}
	attrs := attributeValues(a)
	user := &backend.User{ID: nameID}

	user.Email = firstValue(attrs, namesOr(p.cfg.EmailAttribute, defaultEmailAttributes))
	if user.Email == "" && p.cfg.EmailAttribute == "" && a.Subject.NameID.Format == string(crewjam.EmailAddressNameIDFormat) {
		user.Email = nameID
	}
	user.Username = firstValue(attrs, namesOr(p.cfg.UsernameAttribute, defaultUsernameAttributes))
	if user.Username == "" {
		user.Username = user.Email
	}
	if user.Username == "" {
		user.Username = nameID
	}
	if role := strings.TrimSpace(p.cfg.RoleAttribute); role != "" {
		user.Roles = attrs[role]
		if len(user.Roles) > 0 {
			user.Role = user.Roles[0]
		}
	}
	return user, nil
}

// attributeValues indexes every attribute of the assertion by its Name and
// by its FriendlyName. Blank values are dropped.
func attributeValues(a *crewjam.Assertion) map[string][]string {
	out := map[string][]string{}
	for _, statement := range a.AttributeStatements {
		for _, attr := range statement.Attributes {
			var values []string
			for _, v := range attr.Values {
				if s := strings.TrimSpace(v.Value); s != "" {
					values = append(values, s)
				}
			}
			if len(values) == 0 {
				continue
			}
			for _, key := range []string{attr.Name, attr.FriendlyName} {
				if key != "" {
					out[key] = append(out[key], values...)
				}
			}
		}
	}
	return out
}

func namesOr(configured string, defaults []string) []string {
	if c := strings.TrimSpace(configured); c != "" {
		return []string{c}
	}
	return defaults
}

func firstValue(attrs map[string][]string, names []string) string {
	for _, n := range names {
		if v := attrs[n]; len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
