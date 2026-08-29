// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package ldap

import (
	"errors"
	"strings"
	"testing"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend/backendtest"
)

// This backend is the framework's own first provider, so it is also the
// first consumer of the conformance suite — and the argument for the suite
// is only worth as much as its author's willingness to be graded by it.
//
// The properties below are already pinned one by one, by mutation, in
// ldap_test.go. That is not duplication: those tests assert how THIS
// backend implements them (which bind it sends, which filter it escapes),
// while the suite asserts the properties a third party must satisfy
// without knowing anything about LDAP. If the two ever disagree, the
// contract moved and one of them is stale.
func TestConformance(t *testing.T) {
	backendtest.Run(t, backendtest.Suite{
		New:           func() (backend.Backend, error) { return conformanceBackend() },
		ValidUser:     "ana",
		ValidPassword: "correcta",
		UnknownUser:   "nadie",
		Unavailable:   unreachableBackend,
	})
}

// conformanceBackend is a directory that behaves the way a real one does:
// it finds ana, accepts her password and refuses every other, and returns
// nothing for a name it does not hold.
func conformanceBackend() (*Backend, error) {
	const anaDN = "uid=ana,ou=people,dc=corp,dc=local"
	b, err := NewWithConfig(conformanceConfig())
	if err != nil {
		return nil, err
	}
	b.dialer = func(Config) (conn, error) {
		return &directoryConn{
			users: map[string]string{anaDN: "correcta"},
			entries: map[string]*goldap.Entry{
				"ana": entry(anaDN, map[string]string{"uid": "ana", "mail": "ana@corp.local", "cn": "Ana"}),
			},
		}, nil
	}
	return b, nil
}

// unreachableBackend is the SAME backend pointed at a directory that does
// not answer. It is the check the suite calls the most consequential one:
// reporting an outage as a rejection stops the chain and locks out the
// local account kept for exactly that morning.
func unreachableBackend() (backend.Backend, error) {
	b, err := NewWithConfig(conformanceConfig())
	if err != nil {
		return nil, err
	}
	b.dialer = func(Config) (conn, error) {
		return nil, errors.New("dial tcp 203.0.113.1:636: connect: connection refused")
	}
	return b, nil
}

// directoryConn answers like a directory rather than like a fixture: the
// bind succeeds only for the DN/password pair it holds, and the search
// returns an entry only for a name it knows. fakeConn in ldap_test.go
// records the exchange to assert ON it; this one just behaves.
type directoryConn struct {
	users   map[string]string        // DN -> password
	entries map[string]*goldap.Entry // username -> entry
}

// Bind answers the way RFC 4513 §5.1.2 entitles a directory to answer,
// which is the whole reason this fixture is not a convenience: a simple
// bind carrying a DN and an EMPTY password is an unauthenticated bind, and
// a conforming directory may answer it with SUCCESS. A fixture that
// rejected it instead would let a backend that forwards empty passwords
// pass the suite — the exact failure ADR-027 was written about, and one
// this file reproduced on its first draft.
func (d *directoryConn) Bind(dn, password string) error {
	if password == "" {
		return nil // unauthenticated bind: the dangerous, conforming answer
	}
	if want, ok := d.users[dn]; ok && want == password {
		return nil
	}
	return &goldap.Error{ResultCode: goldap.LDAPResultInvalidCredentials}
}

func (d *directoryConn) Search(req *goldap.SearchRequest) (*goldap.SearchResult, error) {
	res := &goldap.SearchResult{}
	for name, e := range d.entries {
		if searchFilterNames(req.Filter, name) {
			res.Entries = append(res.Entries, e)
		}
	}
	return res, nil
}

func (d *directoryConn) Close() error { return nil }

func conformanceConfig() Config {
	return Config{
		URL:          "ldaps://dc.corp.local:636",
		BaseDN:       "ou=people,dc=corp,dc=local",
		UserFilter:   "(uid=%s)",
		AttrUsername: "uid",
		AttrEmail:    "mail",
		AttrName:     "cn",
		Timeout:      5 * time.Second,
	}
}

// searchFilterNames reports whether the filter the backend built selects
// this username. The backend escapes the name before interpolating it, so
// matching on the escaped form is also what proves the escaping did not
// change an ordinary name.
func searchFilterNames(filter, username string) bool {
	return strings.Contains(filter, "="+goldap.EscapeFilter(username)+")")
}
