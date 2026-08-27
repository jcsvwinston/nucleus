// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package ldap

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
)

// conn is the slice of the LDAP client this backend uses. It exists so the
// tests can drive every branch — a refused bind, a directory that does not
// answer, an ambiguous search — without a directory, while the live tests
// still run against a real one.
type conn interface {
	Bind(username, password string) error
	Search(req *goldap.SearchRequest) (*goldap.SearchResult, error)
	Close() error
}

// dial opens the connection described by the configuration.
//
// TLS comes from the URL scheme (ldaps://) or from StartTLS on a plaintext
// port. The timeout bounds the dial and every subsequent operation on the
// connection: the LDAP client is not context-aware, so a context deadline
// shorter than Timeout is not propagated into a call already in flight —
// stated here rather than implied, because a timeout that silently does
// not apply is worse than no timeout at all.
func dial(cfg Config) (conn, error) {
	host, err := hostFromURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // opt-in, warned about at construction
	}

	c, err := goldap.DialURL(cfg.URL,
		goldap.DialWithDialer(&net.Dialer{Timeout: cfg.Timeout}),
		goldap.DialWithTLSConfig(tlsCfg),
	)
	if err != nil {
		return nil, err
	}
	c.SetTimeout(cfg.Timeout)

	if cfg.StartTLS && strings.HasPrefix(strings.ToLower(cfg.URL), "ldap://") {
		if err := c.StartTLS(tlsCfg); err != nil {
			c.Close()
			// A failed upgrade is NOT a fallback to plaintext. The
			// password would cross the wire unprotected on a connection
			// the operator asked to be encrypted.
			return nil, fmt.Errorf("start_tls is on but the upgrade failed: %w", err)
		}
	}
	return c, nil
}

// hostFromURL extracts the host for TLS name verification. Getting this
// wrong means verifying the certificate against the wrong name, which
// verifies nothing.
func hostFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ldap: url %q is not a valid URL: %w", raw, err)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("ldap: url %q has no host", raw)
	}
	return u.Hostname(), nil
}

// ensure the concrete client satisfies the interface the backend uses.
var _ conn = (*goldap.Conn)(nil)
