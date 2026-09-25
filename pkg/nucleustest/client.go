// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// The client half of the kit: what a test says to the application and how
// it reads the answer. Before this file the kit handed the author a bare
// *http.Client and a URL; every JSON round trip was the author's own marshal,
// request, status check and decode, no cookie survived from one request to
// the next, and a route behind the CSRF middleware or behind a session could
// not be reached without reproducing the middleware's protocol by hand.

// Response is what a request produced, read in full so the test can look at
// it more than once. Status is the HTTP status code.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body into v and fails the test when it is not JSON. A
// nil v only checks that the body parses.
func (r Response) JSON(tb testingTB, v any) {
	tb.Helper()
	if v == nil {
		v = new(any)
	}
	if err := json.Unmarshal(r.Body, v); err != nil {
		tb.Fatalf("nucleustest: response is not the JSON expected (status %d): %v\n%s", r.Status, err, truncate(r.Body, 512))
	}
}

// String returns the body as text.
func (r Response) String() string { return string(r.Body) }

// testingTB is the subset of testing.TB the response helpers need; it lets
// the kit be driven from a benchmark as well as a test.
type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// RequestOption adjusts one request before it is sent.
type RequestOption func(*http.Request)

// WithHeader sets a header on the request.
func WithHeader(key, value string) RequestOption {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

// WithBearer sends a bearer token — usually one from MintToken.
func WithBearer(token string) RequestOption {
	return WithHeader("Authorization", "Bearer "+token)
}

// WithQuery adds a query parameter.
func WithQuery(key, value string) RequestOption {
	return func(r *http.Request) {
		q := r.URL.Query()
		q.Add(key, value)
		r.URL.RawQuery = q.Encode()
	}
}

// Request sends one request to the application and reads the whole answer.
//
// body may be nil, a []byte or a string (sent as-is), an io.Reader, or any
// other value, which is encoded as JSON with Content-Type application/json.
// Accept defaults to application/json; an option can override any header.
// Cookies the application set on earlier requests ride along (see Cookies),
// and cookies it sets on this one are kept. A transport failure is fatal —
// the application is in this process, so an unreachable server is a bug in
// the test, never a condition to assert on.
func (s *Server) Request(method, path string, body any, opts ...RequestOption) Response {
	s.tb.Helper()
	var rd io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = strings.NewReader(b)
	case io.Reader:
		rd = b
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			s.tb.Fatalf("nucleustest: encode request body: %v", err)
		}
		rd = bytes.NewReader(raw)
		contentType = "application/json"
	}
	req, err := http.NewRequest(method, s.URL(path), rd)
	if err != nil {
		s.tb.Fatalf("nucleustest: build %s %s: %v", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, opt := range opts {
		opt(req)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.tb.Fatalf("nucleustest: %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		s.tb.Fatalf("nucleustest: read %s %s: %v", method, path, err)
	}
	return Response{Status: resp.StatusCode, Header: resp.Header, Body: raw}
}

// Get, Post, Put, Patch and Delete are Request with the method filled in.
func (s *Server) Get(path string, opts ...RequestOption) Response {
	s.tb.Helper()
	return s.Request(http.MethodGet, path, nil, opts...)
}

func (s *Server) Post(path string, body any, opts ...RequestOption) Response {
	s.tb.Helper()
	return s.Request(http.MethodPost, path, body, opts...)
}

func (s *Server) Put(path string, body any, opts ...RequestOption) Response {
	s.tb.Helper()
	return s.Request(http.MethodPut, path, body, opts...)
}

func (s *Server) Patch(path string, body any, opts ...RequestOption) Response {
	s.tb.Helper()
	return s.Request(http.MethodPatch, path, body, opts...)
}

func (s *Server) Delete(path string, opts ...RequestOption) Response {
	s.tb.Helper()
	return s.Request(http.MethodDelete, path, nil, opts...)
}

// ---- cookies ----------------------------------------------------------------

// Cookies returns the cookies the client currently holds for the
// application — what the application set, as the next request will send it.
func (s *Server) Cookies() []*http.Cookie {
	u, err := url.Parse(s.BaseURL)
	if err != nil || s.client.Jar == nil {
		return nil
	}
	return s.client.Jar.Cookies(u)
}

// SetCookie stores a cookie in the client as if the application had set it.
func (s *Server) SetCookie(c *http.Cookie) {
	s.tb.Helper()
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		s.tb.Fatalf("nucleustest: parse base URL: %v", err)
	}
	if c.Path == "" {
		c.Path = "/"
	}
	s.client.Jar.SetCookies(u, []*http.Cookie{c})
}

// loopbackJar is the client's cookie jar. The application issues its
// session and CSRF cookies with Secure=true — the production-safe default —
// and the test server speaks plain HTTP on loopback, which the standard jar
// treats as insecure and so drops every one of them. Browsers make an
// exception for localhost; this jar makes the same one, by storing the
// cookies without the flag. Nothing else about them changes.
type loopbackJar struct{ inner http.CookieJar }

func newLoopbackJar() (*loopbackJar, error) {
	inner, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &loopbackJar{inner: inner}, nil
}

func (j *loopbackJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	kept := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		cc := *c
		cc.Secure = false
		kept = append(kept, &cc)
	}
	j.inner.SetCookies(u, kept)
}

func (j *loopbackJar) Cookies(u *url.URL) []*http.Cookie { return j.inner.Cookies(u) }

// ---- CSRF ---------------------------------------------------------------------

// csrfCookieName and csrfHeaderName are the router's defaults; the
// application's config has no knob for either, so the kit needs none.
const (
	csrfCookieName = "_csrf"
	csrfHeaderName = "X-CSRF-Token"
)

// CSRFToken returns the CSRF token the application issued to this client,
// asking for one with a harmless GET when none is held yet. It is empty when
// the application does not run the CSRF middleware (csrf_enabled).
func (s *Server) CSRFToken() string {
	s.tb.Helper()
	if tok := s.heldCSRFToken(); tok != "" {
		return tok
	}
	_ = s.Get("/healthz")
	return s.heldCSRFToken()
}

func (s *Server) heldCSRFToken() string {
	for _, c := range s.Cookies() {
		if c.Name == csrfCookieName && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// WithCSRF sends the CSRF token in the header the middleware reads, fetching
// one first when the client holds none. On an application without the
// middleware it sends nothing.
func (s *Server) WithCSRF() RequestOption {
	s.tb.Helper()
	tok := s.CSRFToken()
	return func(r *http.Request) {
		if tok != "" {
			r.Header.Set(csrfHeaderName, tok)
		}
	}
}

// ---- sessions -------------------------------------------------------------------

// SignIn opens a session in the application's own session store with the
// given values and stores its cookie in the client, so the routes that read
// the session see a signed-in user on every following request — without a
// password, a login form or the flows around them, which the test of a route
// that merely needs a user has no business exercising. It returns the
// session token. The keys are the application's: pkg/accounts reads
// accounts.SessionKeyAccountID and accounts.SessionKeyEmail, and
// SignInAccount fills those two.
func (s *Server) SignIn(values map[string]string) string {
	s.tb.Helper()
	rt := s.Runtime()
	sm := rt.Session()
	if sm == nil {
		s.tb.Fatalf("nucleustest: SignIn: the application has no session manager")
	}
	scs := sm.SCS()
	ctx, err := scs.Load(context.Background(), "")
	if err != nil {
		s.tb.Fatalf("nucleustest: SignIn: open session: %v", err)
	}
	for k, v := range values {
		scs.Put(ctx, k, v)
	}
	token, expiry, err := scs.Commit(ctx)
	if err != nil {
		s.tb.Fatalf("nucleustest: SignIn: commit session: %v", err)
	}
	s.SetCookie(&http.Cookie{
		Name:     scs.Cookie.Name,
		Value:    token,
		Path:     scs.Cookie.Path,
		Expires:  expiry,
		HttpOnly: true,
	})
	return token
}

// SignInAccount is SignIn for an account the way pkg/accounts records one
// after a successful login: the account id and the e-mail under the keys the
// module reads.
func (s *Server) SignInAccount(accountID, email string) string {
	s.tb.Helper()
	return s.SignIn(map[string]string{
		"account_id":    accountID,
		"account_email": email,
	})
}

// SignOut drops the session cookie from the client. The session itself
// stays in the store until it expires or the application destroys it; what
// this ends is the client's part of it.
func (s *Server) SignOut() {
	s.tb.Helper()
	rt := s.Runtime()
	name := "session"
	if sm := rt.Session(); sm != nil {
		name = sm.SCS().Cookie.Name
	}
	s.SetCookie(&http.Cookie{Name: name, Value: "", Path: "/", Expires: time.Unix(0, 0), MaxAge: -1})
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return fmt.Sprintf("%s… (%d bytes)", b[:n], len(b))
}
