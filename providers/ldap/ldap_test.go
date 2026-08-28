// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package ldap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// fakeConn records what the backend asked the directory to do, so the
// tests can assert on the exchange itself — which is where the security
// properties of this backend live.
type fakeConn struct {
	binds       []bindCall
	filters     []string
	entries     []*goldap.Entry
	searchErr   error
	bindErrs    map[string]error // DN -> error returned by Bind
	defaultBind error
}

type bindCall struct{ dn, password string }

func (f *fakeConn) Bind(dn, password string) error {
	f.binds = append(f.binds, bindCall{dn, password})
	if err, ok := f.bindErrs[dn]; ok {
		return err
	}
	return f.defaultBind
}

func (f *fakeConn) Search(req *goldap.SearchRequest) (*goldap.SearchResult, error) {
	f.filters = append(f.filters, req.Filter)
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return &goldap.SearchResult{Entries: f.entries}, nil
}

func (f *fakeConn) Close() error { return nil }

func entry(dn string, attrs map[string]string) *goldap.Entry {
	e := &goldap.Entry{DN: dn}
	for name, value := range attrs {
		e.Attributes = append(e.Attributes, &goldap.EntryAttribute{Name: name, Values: []string{value}})
	}
	return e
}

func testBackend(t *testing.T, c *fakeConn, mutate ...func(*Config)) *Backend {
	t.Helper()
	cfg := Config{
		URL:          "ldaps://dc.corp.local:636",
		BaseDN:       "ou=people,dc=corp,dc=local",
		UserFilter:   "(uid=%s)",
		AttrUsername: "uid",
		AttrEmail:    "mail",
		AttrName:     "cn",
		Timeout:      5 * time.Second,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	b, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	b.dialer = func(Config) (conn, error) { return c, nil }
	return b
}

// RFC 4513 §5.1.2: a simple bind with a DN and an empty password is an
// UNAUTHENTICATED bind, which a directory is entitled to answer with
// SUCCESS. A backend that forwards it authenticates every account in the
// directory with a blank password.
//
// This test fabricates the unsafe condition — a directory that accepts
// every bind — and requires the backend to reject anyway, without even
// opening a connection. A happy-path test would not have caught it.
func TestAuthenticate_EmptyPasswordIsRejectedWithoutTouchingTheDirectory(t *testing.T) {
	c := &fakeConn{
		entries:     []*goldap.Entry{entry("uid=ana,ou=people,dc=corp,dc=local", map[string]string{"uid": "ana"})},
		defaultBind: nil, // this directory says yes to everything
	}
	dialed := false
	b := testBackend(t, c)
	b.dialer = func(Config) (conn, error) { dialed = true; return c, nil }

	user, err := b.Authenticate(context.Background(), "ana", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("an empty password must be rejected, got user=%v err=%v", user, err)
	}
	if dialed {
		t.Error("an empty password must not reach the directory at all")
	}
	if len(c.binds) != 0 {
		t.Errorf("no bind may be attempted with an empty password, got %v", c.binds)
	}
}

// A username carrying filter metacharacters must not be able to change the
// SHAPE of the query. Unescaped, `*)(uid=*` turns `(uid=%s)` into a filter
// that matches every entry, and the first one found becomes the account
// being logged into.
func TestAuthenticate_UsernameIsFilterEscaped(t *testing.T) {
	c := &fakeConn{}
	b := testBackend(t, c)

	_, _ = b.Authenticate(context.Background(), "*)(uid=*", "secret")

	if len(c.filters) != 1 {
		t.Fatalf("expected one search, got %v", c.filters)
	}
	got := c.filters[0]
	if strings.Contains(got, "*)(uid=*") {
		t.Fatalf("the username reached the filter unescaped: %s", got)
	}
	if want := "(uid=" + goldap.EscapeFilter("*)(uid=*") + ")"; got != want {
		t.Errorf("filter = %q, want %q", got, want)
	}
}

// An absent user must cost the same round trip a wrong password does.
// Answering without binding at all is a user enumerator measurable from
// the login form.
func TestAuthenticate_AbsentUserStillBinds(t *testing.T) {
	c := &fakeConn{} // no entries
	b := testBackend(t, c)

	_, err := b.Authenticate(context.Background(), "nobody", "secret")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("an absent user must be a rejection, got %v", err)
	}
	if len(c.binds) != 1 {
		t.Fatalf("the absent-user path must still bind once, got %v", c.binds)
	}
	if c.binds[0].password != "secret" {
		t.Errorf("the equalising bind must carry the supplied password, got %q", c.binds[0].password)
	}
	if !strings.Contains(c.binds[0].dn, "no-such-entry") {
		t.Errorf("the equalising bind must target a DN that cannot exist, got %q", c.binds[0].dn)
	}
}

// Two matches is ambiguous. Picking one would make the account somebody
// authenticates into depend on directory ordering.
func TestAuthenticate_AmbiguousMatchIsRejected(t *testing.T) {
	c := &fakeConn{entries: []*goldap.Entry{
		entry("uid=ana,ou=a,dc=corp,dc=local", map[string]string{"uid": "ana"}),
		entry("uid=ana,ou=b,dc=corp,dc=local", map[string]string{"uid": "ana"}),
	}}
	b := testBackend(t, c)

	if _, err := b.Authenticate(context.Background(), "ana", "secret"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("an ambiguous match must be a rejection, got %v", err)
	}
}

func TestAuthenticate_Succeeds(t *testing.T) {
	c := &fakeConn{entries: []*goldap.Entry{
		entry("uid=ana,ou=people,dc=corp,dc=local", map[string]string{
			"uid": "ana", "mail": "ana@corp.local", "cn": "Ana Ruiz",
		}),
	}}
	b := testBackend(t, c)

	user, err := b.Authenticate(context.Background(), "ana", "correcta")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if user.ID != "uid=ana,ou=people,dc=corp,dc=local" {
		t.Errorf("ID must be the entry's DN, got %q", user.ID)
	}
	if user.Username != "ana" || user.Email != "ana@corp.local" {
		t.Errorf("attributes did not map: %+v", user)
	}
	last := c.binds[len(c.binds)-1]
	if last.dn != user.ID || last.password != "correcta" {
		t.Errorf("the final bind must be as the found entry with the supplied password, got %+v", last)
	}
}

// The three answers of the chain's contract, one per row. The distinction
// between "rejected" and "could not reach the source" is what lets a local
// break-glass account still work, so it is asserted per failure mode
// rather than assumed.
func TestAuthenticate_ThreeAnswers(t *testing.T) {
	const dn = "uid=ana,ou=people,dc=corp,dc=local"
	found := []*goldap.Entry{entry(dn, map[string]string{"uid": "ana"})}

	for _, tc := range []struct {
		name    string
		conn    *fakeConn
		dialErr error
		cfg     func(*Config)
		want    error
	}{
		{
			name: "the directory refuses the credentials",
			conn: &fakeConn{entries: found, bindErrs: map[string]error{
				dn: goldap.NewError(goldap.LDAPResultInvalidCredentials, errors.New("invalid credentials")),
			}},
			want: auth.ErrInvalidCredentials,
		},
		{
			name:    "the directory cannot be dialled",
			conn:    &fakeConn{entries: found},
			dialErr: errors.New("connection refused"),
			want:    auth.ErrBackendUnavailable,
		},
		{
			name: "the search fails",
			conn: &fakeConn{searchErr: errors.New("size limit exceeded")},
			want: auth.ErrBackendUnavailable,
		},
		{
			name: "the service account cannot bind",
			conn: &fakeConn{entries: found, bindErrs: map[string]error{
				"cn=svc,dc=corp,dc=local": goldap.NewError(goldap.LDAPResultInvalidCredentials, errors.New("invalid credentials")),
			}},
			cfg:  func(c *Config) { c.BindDN = "cn=svc,dc=corp,dc=local"; c.BindPassword = "rotated-away" },
			want: auth.ErrBackendUnavailable,
		},
		{
			name: "the user's bind fails for an unanticipated reason",
			conn: &fakeConn{entries: found, bindErrs: map[string]error{
				dn: goldap.NewError(goldap.LDAPResultUnwillingToPerform, errors.New("unwilling to perform")),
			}},
			want: auth.ErrBackendUnavailable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutators := []func(*Config){}
			if tc.cfg != nil {
				mutators = append(mutators, tc.cfg)
			}
			b := testBackend(t, tc.conn, mutators...)
			if tc.dialErr != nil {
				b.dialer = func(Config) (conn, error) { return nil, tc.dialErr }
			}
			_, err := b.Authenticate(context.Background(), "ana", "secret")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// A service-account failure deserves its own assertion: it must not stop
// the chain. "Wrong password" would both send an operator hunting in the
// wrong place and skip the break-glass account behind this backend.
func TestAuthenticate_ServiceAccountFailureDoesNotStopTheChain(t *testing.T) {
	c := &fakeConn{defaultBind: goldap.NewError(goldap.LDAPResultInvalidCredentials, errors.New("invalid credentials"))}
	b := testBackend(t, c, func(cfg *Config) {
		cfg.BindDN = "cn=svc,dc=corp,dc=local"
		cfg.BindPassword = "rotated-away"
	})
	_, err := b.Authenticate(context.Background(), "ana", "secret")
	if errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("a broken service account must not be reported as wrong user credentials")
	}
	if !errors.Is(err, auth.ErrBackendUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestNewWithConfig_RejectsUnusableConfiguration(t *testing.T) {
	base := Config{URL: "ldaps://dc:636", BaseDN: "dc=corp", UserFilter: "(uid=%s)", Timeout: time.Second}
	for _, tc := range []struct {
		name string
		cfg  func(*Config)
		want string
	}{
		{"no url", func(c *Config) { c.URL = "" }, "url is required"},
		{"no base_dn", func(c *Config) { c.BaseDN = "" }, "base_dn is required"},
		{"filter without placeholder", func(c *Config) { c.UserFilter = "(uid=fixed)" }, "must contain %s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.cfg(&cfg)
			_, err := NewWithConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// The backend must be reachable the way an operator reaches it: by name
// from auth_backends, with its settings arriving through the subtree the
// framework binds. This is the wire the whole arc exists to build.
func TestRegisteredBackend_BuildsFromItsConfigSubtree(t *testing.T) {
	registered := auth.RegisteredBackends()
	if !contains(registered, BackendName) {
		t.Fatalf("importing this package must register %q, got %v", BackendName, registered)
	}

	chain, err := auth.NewChainFrom(auth.ChainConfig{
		Backends: []string{BackendName},
		ProviderConfig: map[string]map[string]any{
			BackendName: {
				"url":     "ldaps://dc.corp.local:636",
				"base_dn": "ou=people,dc=corp,dc=local",
			},
		},
	})
	if err != nil {
		t.Fatalf("the backend must build from its subtree: %v", err)
	}
	if names := chain.Names(); len(names) != 1 || names[0] != BackendName {
		t.Fatalf("chain = %v", names)
	}
}

// A subtree key the Config struct does not declare is an error, not a
// silently ignored line: a typo in a directory URL must not sit unnoticed
// until the morning it matters.
func TestRegisteredBackend_UnknownSubtreeKeyIsAnError(t *testing.T) {
	_, err := auth.NewChainFrom(auth.ChainConfig{
		Backends: []string{BackendName},
		ProviderConfig: map[string]map[string]any{
			BackendName: {
				"url":      "ldaps://dc.corp.local:636",
				"base_dn":  "ou=people,dc=corp,dc=local",
				"bas_dn":   "typo",
				"whatever": 1,
			},
		},
	})
	if err == nil {
		t.Fatal("an undeclared key in the backend's subtree must fail")
	}
	if !strings.Contains(err.Error(), "bas_dn") {
		t.Errorf("the error must name the offending key, got %v", err)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
