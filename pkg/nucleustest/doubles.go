// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
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

// Stored returns the bytes the application stored under key, through the
// application's own store — the memory provider (storage.provider: memory)
// or whatever it runs. A missing key fails the test.
func (s *Server) Stored(key string) []byte {
	s.tb.Helper()
	store := s.Runtime().Storage()
	if store == nil {
		s.tb.Fatalf("nucleustest: the application has no storage")
	}
	rc, _, err := store.Get(context.Background(), key)
	if err != nil {
		s.tb.Fatalf("nucleustest: Stored(%q): %v", key, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		s.tb.Fatalf("nucleustest: Stored(%q): read: %v", key, err)
	}
	return data
}

// StoredKeys lists the keys under prefix ("" for everything), sorted.
func (s *Server) StoredKeys(prefix string) []string {
	s.tb.Helper()
	store := s.Runtime().Storage()
	if store == nil {
		s.tb.Fatalf("nucleustest: the application has no storage")
	}
	var keys []string
	marker := ""
	for {
		res, err := store.List(context.Background(), storage.ListOptions{Prefix: prefix, Marker: marker})
		if err != nil {
			s.tb.Fatalf("nucleustest: StoredKeys(%q): %v", prefix, err)
		}
		for _, o := range res.Objects {
			keys = append(keys, o.Key)
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
