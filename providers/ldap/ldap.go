// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package ldap authenticates against an LDAP directory.
//
// It is a separate Go module on purpose. The seam belongs in the framework
// and the protocol client does not: whoever does not authenticate against a
// directory should not carry an LDAP library, and the framework's
// dependency firewall never has to be argued about (ADR-023, decision 5).
// It ships in the framework's own repository, on the framework's release
// train and in the framework's documentation, so "separate module" is a
// packaging fact rather than a second-class citizenship.
//
// Wire it the way database/sql drivers are wired — import for the side
// effect, then name it in the chain:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/ldap"
//
//	# nucleus.yml
//	auth_backends: [ldap, local]
//	auth:
//	  ldap:
//	    url: "ldaps://dc.corp.local:636"
//	    base_dn: "ou=people,dc=corp,dc=local"
//	    bind_dn: "cn=svc-nucleus,ou=services,dc=corp,dc=local"
//	    bind_password: "${LDAP_BIND_PASSWORD}"
//
// The order in auth_backends is the feature: `[ldap, local]` consults the
// directory first and still lets a local account in on the morning the
// directory does not answer.
package ldap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// BackendName is the name this backend registers under, and the name that
// selects it in auth_backends and under `auth.<name>.*`.
const BackendName = "ldap"

func init() {
	if err := backend.Register(BackendName, New); err != nil {
		// A duplicate name would otherwise make the effective backend
		// depend on import order — the one failure mode that only ever
		// shows up in somebody else's deployment.
		panic("ldap: registering the " + BackendName + " backend: " + err.Error())
	}
}

// Config is the `auth.ldap.*` subtree.
//
// Every field the directory needs is here, and nothing else: this backend
// does not read the environment or any file of its own. A key this struct
// does not declare is an error rather than a silently ignored line.
type Config struct {
	// URL of the directory: ldaps://host:636 for TLS, ldap://host:389
	// otherwise (see StartTLS).
	URL string `koanf:"url" validate:"required"`

	// BaseDN is the subtree the user search starts from.
	BaseDN string `koanf:"base_dn" validate:"required"`

	// BindDN and BindPassword are the service account used to SEARCH for
	// the user. Leave them empty for a directory that allows anonymous
	// search. They are never used to authenticate the person logging in.
	BindDN       string `koanf:"bind_dn"`
	BindPassword string `koanf:"bind_password"`

	// UserFilter selects the user entry. `%s` is replaced with the
	// username AFTER filter-escaping it, so a username containing filter
	// metacharacters cannot change the shape of the query.
	UserFilter string `koanf:"user_filter" default:"(uid=%s)"`

	// Attribute names mapped onto backend.User. An attribute the entry does
	// not carry simply leaves its field empty.
	AttrUsername string `koanf:"attr_username" default:"uid"`
	AttrEmail    string `koanf:"attr_email" default:"mail"`
	AttrName     string `koanf:"attr_name" default:"cn"`

	// Timeout bounds the whole exchange: dial, search and bind.
	Timeout time.Duration `koanf:"timeout" default:"5s"`

	// StartTLS upgrades a plaintext ldap:// connection to TLS before any
	// credential crosses it. Ignored for ldaps://, which is already TLS.
	StartTLS bool `koanf:"start_tls"`

	// InsecureSkipVerify disables certificate verification. It exists for
	// a lab with a self-signed certificate and for nothing else: with it
	// on, anything that can intercept the connection collects every
	// password that crosses it. Turning it on logs a warning at startup.
	InsecureSkipVerify bool `koanf:"insecure_skip_verify"`
}

// Backend authenticates against a directory. Build it through the registry
// (auth_backends) rather than directly; New is exported for tests and for
// an application that wires its chain in Go.
type Backend struct {
	cfg    Config
	dialer func(cfg Config) (conn, error)
}

// New builds the backend from its configuration subtree. It is the
// backend.Factory this package registers.
func New(bc backend.Config) (backend.Backend, error) {
	var cfg Config
	if err := bc.Bind(&cfg); err != nil {
		return nil, err
	}
	return NewWithConfig(cfg)
}

// NewWithConfig builds the backend from an already-decoded Config.
func NewWithConfig(cfg Config) (*Backend, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("ldap: url is required")
	}
	if strings.TrimSpace(cfg.BaseDN) == "" {
		return nil, fmt.Errorf("ldap: base_dn is required")
	}
	if !strings.Contains(cfg.UserFilter, "%s") {
		return nil, fmt.Errorf("ldap: user_filter %q must contain %%s, the placeholder the username is substituted into", cfg.UserFilter)
	}
	if cfg.InsecureSkipVerify {
		slog.Warn("ldap: insecure_skip_verify is on — the directory's certificate is NOT verified, so anything able to intercept this connection collects every password that crosses it. Use it in a lab and nowhere else.",
			"url", cfg.URL)
	}
	if !cfg.StartTLS && strings.HasPrefix(strings.ToLower(cfg.URL), "ldap://") {
		slog.Warn("ldap: the connection is plaintext — passwords cross it unprotected. Use ldaps:// or set start_tls.",
			"url", cfg.URL)
	}
	return &Backend{cfg: cfg, dialer: dial}, nil
}

// Name identifies the backend in logs and configuration.
func (b *Backend) Name() string { return BackendName }

// Authenticate resolves the username to a directory entry and binds as
// that entry with the supplied password.
//
// The three answers of the chain's contract map onto the directory like
// this: the entry is missing or the bind is refused → ErrInvalidCredentials;
// the directory cannot be reached, the service account cannot bind, or the
// search fails → ErrBackendUnavailable, which is what lets the local
// account behind this backend still work.
//
// A failure of the SERVICE ACCOUNT is deliberately not a rejection. The
// person's credentials were never tested, so answering "wrong password"
// would send an operator hunting in the wrong place and — worse — would
// stop the chain instead of falling through to the break-glass account.
func (b *Backend) Authenticate(ctx context.Context, username, password string) (*backend.User, error) {
	// An empty password is rejected WITHOUT touching the directory.
	//
	// RFC 4513 §5.1.2: a simple bind with a DN and an empty password is an
	// UNAUTHENTICATED bind, and a directory is entitled to answer it with
	// success — which would authenticate every account in the company with
	// a blank password.
	//
	// go-ldap v3.4.14 refuses it client-side as well (result code 206), so
	// this is defence in depth rather than the only thing standing there.
	// It stays because the property must not depend on a library keeping a
	// behaviour it is under no obligation to keep, nor on which directory
	// is at the other end; and because rejecting here costs no round trip.
	// The live test asserts the outcome against a real server whichever
	// way that server answers.
	if password == "" {
		return nil, backend.ErrInvalidCredentials
	}
	if strings.TrimSpace(username) == "" {
		return nil, backend.ErrInvalidCredentials
	}

	ctx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	defer cancel()

	c, err := b.dialer(b.cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: dialing %s: %v", backend.ErrBackendUnavailable, b.cfg.URL, err)
	}
	defer c.Close()

	if b.cfg.BindDN != "" {
		if err := c.Bind(b.cfg.BindDN, b.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("%w: the service account %s could not bind: %v", backend.ErrBackendUnavailable, b.cfg.BindDN, err)
		}
	}

	entry, err := b.searchUser(ctx, c, username)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		// The user does not exist. Bind anyway, against a DN that cannot
		// exist, so this path costs the same round trip as a wrong
		// password does.
		//
		// It does not make the backend constant-time and does not claim
		// to: the directory's own timing still varies. What it removes is
		// the difference this code would otherwise ADD — an absent user
		// answering without a network round trip at all, which is a user
		// enumerator anyone can measure from the login form.
		_ = c.Bind("cn=nucleus-no-such-entry,"+b.cfg.BaseDN, password)
		return nil, backend.ErrInvalidCredentials
	}

	if err := c.Bind(entry.DN, password); err != nil {
		if isInvalidCredentials(err) {
			return nil, backend.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("%w: binding as %s: %v", backend.ErrBackendUnavailable, entry.DN, err)
	}

	return b.toUser(entry, username), nil
}

// searchUser returns the single entry matching the username, nil when
// there is none, and an unavailable error when the directory could not
// answer.
func (b *Backend) searchUser(_ context.Context, c conn, username string) (*goldap.Entry, error) {
	// The username is filter-escaped BEFORE substitution. Without this a
	// username of `*)(uid=*` turns the filter into one that matches every
	// entry, and the first one found becomes the account being logged
	// into.
	filter := fmt.Sprintf(b.cfg.UserFilter, goldap.EscapeFilter(username))

	req := goldap.NewSearchRequest(
		b.cfg.BaseDN,
		goldap.ScopeWholeSubtree, goldap.NeverDerefAliases,
		2, int(b.cfg.Timeout.Seconds()), false,
		filter,
		[]string{"dn", b.cfg.AttrUsername, b.cfg.AttrEmail, b.cfg.AttrName},
		nil,
	)

	res, err := c.Search(req)
	if err != nil {
		// "No such object" for the base DN is a configuration mistake, not
		// a rejection: answering "wrong password" would hide a broken
		// base_dn behind a login failure forever.
		return nil, fmt.Errorf("%w: searching %s: %v", backend.ErrBackendUnavailable, b.cfg.BaseDN, err)
	}
	switch len(res.Entries) {
	case 0:
		return nil, nil
	case 1:
		return res.Entries[0], nil
	default:
		// Ambiguous. Picking one would mean the account someone
		// authenticates into depends on directory ordering.
		return nil, nil
	}
}

func (b *Backend) toUser(entry *goldap.Entry, fallbackUsername string) *backend.User {
	name := entry.GetAttributeValue(b.cfg.AttrUsername)
	if name == "" {
		name = fallbackUsername
	}
	return &backend.User{
		ID:       entry.DN,
		Username: name,
		Email:    entry.GetAttributeValue(b.cfg.AttrEmail),
	}
}

// isInvalidCredentials reports whether the directory REFUSED the
// credentials, as opposed to failing for any other reason.
//
// The check is on the protocol result code and never on the message text:
// matching an error string is how a rejection silently becomes an outage —
// or an outage silently becomes a rejection — the day a server changes its
// wording (the same defect this suite fixed in S3's not-found mapping).
//
// ONE code, deliberately. Every other refusal — insufficient access,
// unwilling to perform, a directory-side policy — is treated as
// unavailable, because the chain's contract says an unanticipated failure
// must not be able to lock everyone out. A liberal mapping here would turn
// a misconfigured ACL into "wrong password" for the whole company AND stop
// the chain before the break-glass account, which is precisely the outcome
// the three-answer design exists to prevent. Erring towards unavailable
// costs a fallthrough; erring towards rejection costs the outage.
func isInvalidCredentials(err error) bool {
	if err == nil {
		return false
	}
	var le *goldap.Error
	if errors.As(err, &le) {
		return le.ResultCode == goldap.LDAPResultInvalidCredentials
	}
	return false
}
