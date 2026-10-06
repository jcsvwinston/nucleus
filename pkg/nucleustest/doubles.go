// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// The doubles: what the application SENT OUT during the test, read back.
// Mail, files, jobs and outgoing HTTP are the four things a flow emits that
// a test of the flow needs to see and must not let leave the process.

// ---- mail -------------------------------------------------------------------

// SentMail returns every message the application sent so far, oldest first.
// It reads the memory mail driver, which the kit selects when the
// application would otherwise discard mail (mail_driver empty or noop) and
// which an application selects itself with mail_driver: memory. On any
// other driver the test is asking a question the configuration cannot
// answer, and the kit says so.
func (s *Server) SentMail() []mail.Message {
	s.tb.Helper()
	return s.memoryMail().Sent()
}

// ResetMail forgets the messages sent so far.
func (s *Server) ResetMail() {
	s.tb.Helper()
	s.memoryMail().Reset()
}

func (s *Server) memoryMail() *mail.MemorySender {
	s.tb.Helper()
	mem, ok := s.Runtime().Mailer().(*mail.MemorySender)
	if !ok {
		s.tb.Fatalf("nucleustest: the application's mail driver is %q (%T), not memory: set mail_driver: memory (the kit does it for you when the driver is noop) to read what was sent", s.app.Config.MailDriver, s.Runtime().Mailer())
	}
	return mem
}

// captureMail selects the memory mail driver when the application would
// discard mail anyway. A driver the author chose — smtp, a plugin — is kept:
// that test wants the real thing.
func captureMail(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "noop":
		return "memory"
	}
	return driver
}

// ---- storage --------------------------------------------------------------------

// Stored returns the bytes the application stored under key, from the
// application's own store — the memory provider (storage.provider: memory)
// or whatever it runs. A missing key fails the test.
//
// Stored reads the store below its tenant scoping, so key is the key as the
// backend holds it. In a single-tenant application that is the key the
// application wrote. In a multi-tenant one a tenant's file sits under the
// tenant's prefix ("acme/avatars/ana.png"); StoredFor reads it by the key
// the tenant's request used. The read is the test's, not the application's:
// it never meets the storage policy for operations without a tenant — the
// one-time WARN about the shared key space, or the failure under
// multitenant.require_tenant_storage — and leaves that WARN to the
// application's own code (NU-119).
func (s *Server) Stored(key string) []byte {
	s.tb.Helper()
	return s.readStored(s.baseStore(), fmt.Sprintf("Stored(%q)", key), key)
}

// StoredKeys lists the keys under prefix ("" for everything), sorted, as the
// backend holds them: below tenant scoping, like Stored. In a multi-tenant
// application that is every tenant's keys, each under its tenant's prefix;
// StoredKeysFor lists one tenant's.
func (s *Server) StoredKeys(prefix string) []string {
	s.tb.Helper()
	return s.listStored(s.baseStore(), fmt.Sprintf("StoredKeys(%q)", prefix), prefix, "")
}

// StoredFor returns the bytes the application stored under key while serving
// tenant: key is the key as the application's code wrote it, which the store
// keeps under the tenant's prefix. tenant is the id the application resolves
// (multitenant.resolver, app.TenantFromContext), compared the way the
// resolver compares it: trimmed and lower-cased. A missing key fails the
// test, and so does an application that is not multi-tenant
// (multitenant.enabled), whose keys carry no tenant.
//
//	srv.Post("/avatars", img, nucleustest.WithHeader("X-Tenant-ID", "acme"))
//	data := srv.StoredFor("acme", "avatars/ana.png") // stored as acme/avatars/ana.png
func (s *Server) StoredFor(tenant, key string) []byte {
	s.tb.Helper()
	return s.readStored(s.tenantStore("StoredFor", tenant), fmt.Sprintf("StoredFor(%q, %q)", tenant, key), key)
}

// StoredKeysFor lists the keys tenant's requests stored under prefix ("" for
// all of them), sorted and without the tenant's prefix — the keys StoredFor
// reads, as the application's code wrote them. Another tenant's keys are
// never in the list.
func (s *Server) StoredKeysFor(tenant, prefix string) []string {
	s.tb.Helper()
	store := s.tenantStore("StoredKeysFor", tenant)
	return s.listStored(store, fmt.Sprintf("StoredKeysFor(%q, %q)", tenant, prefix), prefix, normalizeTenant(tenant)+"/")
}

// baseStore is the application's store below its tenant scoping: the store
// the TenantStore wraps, through as many tenant layers as there are. Other
// wrappers are kept — they are part of how the backend answers.
func (s *Server) baseStore() storage.Store {
	s.tb.Helper()
	store := s.Runtime().Storage()
	if store == nil {
		s.tb.Fatalf("nucleustest: the application has no storage")
	}
	for {
		ts, ok := store.(*storage.TenantStore)
		if !ok || ts == nil {
			return store
		}
		store = ts.Unwrap()
	}
}

// tenantStore is the base store scoped to tenant the way the application
// scopes the operations of a request that resolved to it: a TenantStore
// whose tenant is always tenant, so the keys get exactly the prefix the
// application's own store gives them.
func (s *Server) tenantStore(fn, tenant string) storage.Store {
	s.tb.Helper()
	if !s.app.Config.MultiTenant.Enabled {
		s.tb.Fatalf("nucleustest: %s(%q, …): the application is not multi-tenant (multitenant.enabled is false), so its keys carry no tenant; read them with %s", fn, tenant, strings.TrimSuffix(fn, "For"))
	}
	tenant = normalizeTenant(tenant)
	if tenant == "" {
		s.tb.Fatalf("nucleustest: %s: the tenant is empty; the shared (unprefixed) key space is what %s reads", fn, strings.TrimSuffix(fn, "For"))
	}
	return storage.NewTenantStore(s.baseStore(), func(context.Context) string { return tenant })
}

// normalizeTenant compares a tenant id the way the request-scope resolver
// does: the header or subdomain it reads is trimmed and lower-cased.
func normalizeTenant(tenant string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(tenant)), "/")
}

// readStored reads key from store; call names the helper in a failure.
func (s *Server) readStored(store storage.Store, call, key string) []byte {
	s.tb.Helper()
	rc, _, err := store.Get(context.Background(), key)
	if err != nil {
		s.tb.Fatalf("nucleustest: %s: %v", call, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		s.tb.Fatalf("nucleustest: %s: read: %v", call, err)
	}
	return data
}

// listStored lists every key under prefix, following the pages, sorted;
// trim is removed from the front of each key and call names the helper in a
// failure.
func (s *Server) listStored(store storage.Store, call, prefix, trim string) []string {
	s.tb.Helper()
	var keys []string
	marker := ""
	for {
		res, err := store.List(context.Background(), storage.ListOptions{Prefix: prefix, Marker: marker})
		if err != nil {
			s.tb.Fatalf("nucleustest: %s: %v", call, err)
		}
		for _, o := range res.Objects {
			keys = append(keys, strings.TrimPrefix(o.Key, trim))
		}
		if !res.Truncated || res.NextMarker == "" {
			break
		}
		marker = res.NextMarker
	}
	sort.Strings(keys)
	return keys
}

// ---- tasks ----------------------------------------------------------------------

// EnqueuedTasks returns every task the application enqueued so far, oldest
// first, whether or not a worker has run it: the record the in-process
// provider keeps (tasks.EnqueueRecorder). The jobs runtime exists once a
// module registers a job; before that there is nothing to record, and the
// kit says so.
func (s *Server) EnqueuedTasks() []tasks.EnqueueRecord {
	s.tb.Helper()
	return s.enqueueRecorder().Enqueued()
}

// ResetEnqueuedTasks forgets the record so far.
func (s *Server) ResetEnqueuedTasks() {
	s.tb.Helper()
	s.enqueueRecorder().ResetEnqueued()
}

func (s *Server) enqueueRecorder() tasks.EnqueueRecorder {
	s.tb.Helper()
	mgr := s.Runtime().Tasks()
	if mgr == nil {
		s.tb.Fatalf("nucleustest: the application has no task manager: the jobs runtime exists once a module registers a job")
	}
	rec, ok := mgr.(tasks.EnqueueRecorder)
	if !ok {
		s.tb.Fatalf("nucleustest: the task manager is %T, which keeps no record of enqueues; the in-process provider (jobs_provider: memory) does", mgr)
	}
	return rec
}

// ---- outgoing HTTP ----------------------------------------------------------------

// HTTPRecorder is a server that stands in for another service: it records
// every request the application makes to it and answers what the test told
// it to. Point the application at rec.URL through its configuration (a
// webhook URL, an API base) or give code that takes an *http.Client
// rec.Client(), which sends every request here whatever host it names.
type HTTPRecorder struct {
	URL string

	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []RecordedRequest
	status int
	body   []byte
	header http.Header
}

// RecordedRequest is one request the application made, body read in full.
type RecordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
	At     time.Time
}

// NewHTTPRecorder starts a recorder that answers 200 with an empty body
// until Respond says otherwise. It is closed when the test ends.
func NewHTTPRecorder(tb testingTB) *HTTPRecorder {
	tb.Helper()
	rec := &HTTPRecorder{status: http.StatusOK, header: http.Header{}}
	rec.srv = httptest.NewServer(http.HandlerFunc(rec.serve))
	rec.URL = rec.srv.URL
	if c, ok := tb.(interface{ Cleanup(func()) }); ok {
		c.Cleanup(rec.srv.Close)
	}
	return rec
}

func (r *HTTPRecorder) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	r.mu.Lock()
	r.reqs = append(r.reqs, RecordedRequest{
		Method: req.Method, Path: req.URL.Path, Query: req.URL.Query(),
		Header: req.Header.Clone(), Body: body, At: time.Now().UTC(),
	})
	status, resp, header := r.status, r.body, r.header.Clone()
	r.mu.Unlock()
	for k, vs := range header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// Respond sets what the recorder answers from now on.
func (r *HTTPRecorder) Respond(status int, body string, header ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status, r.body = status, []byte(body)
	r.header = http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		r.header.Set(header[i], header[i+1])
	}
}

// Requests returns every request received so far, oldest first.
func (r *HTTPRecorder) Requests() []RecordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordedRequest, len(r.reqs))
	copy(out, r.reqs)
	return out
}

// Reset forgets the requests received so far.
func (r *HTTPRecorder) Reset() {
	r.mu.Lock()
	r.reqs = nil
	r.mu.Unlock()
}

// Client returns an *http.Client that sends every request to the recorder,
// whatever host the request names — for code that takes a client and calls
// a real service by its real URL.
func (r *HTTPRecorder) Client() *http.Client {
	return &http.Client{Transport: r.Transport(), Timeout: 5 * time.Second}
}

// Transport is Client's transport, for code that takes a RoundTripper.
func (r *HTTPRecorder) Transport() http.RoundTripper {
	target, _ := url.Parse(r.URL)
	return &redirectTransport{target: target, inner: http.DefaultTransport}
}

type redirectTransport struct {
	target *url.URL
	inner  http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return t.inner.RoundTrip(clone)
}
