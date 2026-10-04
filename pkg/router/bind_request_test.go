// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

type listFilter struct {
	Page   int        `query:"page" validate:"omitempty,min=1"`
	Size   *uint8     `query:"size"`
	Sort   string     `query:"sort" validate:"omitempty,oneof=name date"`
	Tags   []string   `query:"tag"`
	IDs    []int64    `query:"id"`
	Since  time.Time  `query:"since"`
	Addr   netip.Addr `query:"addr"`
	Ignore string     `query:"-"`
	Plain  string
}

func asDomain(t *testing.T, err error) *gferrors.DomainError {
	t.Helper()
	var de *gferrors.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("error %v (%T) is not a DomainError", err, err)
	}
	return de
}

func TestBindQuery_ConvertsEveryKindItDocuments(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet,
		"/?page=3&size=20&sort=name&tag=a&tag=&tag=b&id=7&id=9&since=2026-10-01&addr=10.0.0.1&Ignore=x&Plain=y", nil)
	var f listFilter
	f.Plain = "kept"
	if err := BindQuery(r, &f); err != nil {
		t.Fatalf("BindQuery: %v", err)
	}
	if f.Page != 3 || f.Size == nil || *f.Size != 20 || f.Sort != "name" {
		t.Fatalf("scalars: %+v", f)
	}
	if !reflect.DeepEqual(f.Tags, []string{"a", "b"}) || !reflect.DeepEqual(f.IDs, []int64{7, 9}) {
		t.Fatalf("repeated keys: tags %v ids %v", f.Tags, f.IDs)
	}
	if !f.Since.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("time: %v", f.Since)
	}
	if f.Addr != netip.MustParseAddr("10.0.0.1") {
		t.Fatalf("TextUnmarshaler: %v", f.Addr)
	}
	if f.Ignore != "" || f.Plain != "kept" {
		t.Fatalf("a skipped or untagged field was bound: ignore %q plain %q", f.Ignore, f.Plain)
	}
}

func TestBindQuery_AConversionErrorIsA400NamingTheParameter(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=abc", nil)
	var f listFilter
	de := asDomain(t, BindQuery(r, &f))
	if de.StatusCode != http.StatusBadRequest || !strings.Contains(de.Message, `query parameter "page"`) {
		t.Fatalf("got %d %q", de.StatusCode, de.Message)
	}
	details, _ := de.Details.(map[string]string)
	if details["page"] == "" || strings.Contains(details["page"], "abc") {
		t.Fatalf("details must name the parameter without echoing the value: %#v", de.Details)
	}
	// An overflow is a conversion error too.
	de = asDomain(t, BindQuery(httptest.NewRequest(http.MethodGet, "/?size=300", nil), &f))
	if de.StatusCode != http.StatusBadRequest || !strings.Contains(de.Message, `"size"`) {
		t.Fatalf("overflow: %d %q", de.StatusCode, de.Message)
	}
}

func TestBindQuery_AValidationFailureNamesTheParameterNotTheGoField(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=-1&sort=size", nil)
	var f listFilter
	de := asDomain(t, BindQuery(r, &f))
	if de.StatusCode != http.StatusUnprocessableEntity || de.Code != "VALIDATION_FAILED" {
		t.Fatalf("got %d %s", de.StatusCode, de.Code)
	}
	details, _ := de.Details.(map[string]string)
	if details["page"] == "" || details["sort"] == "" {
		t.Fatalf("validation details must use the query names: %#v", de.Details)
	}
	if _, ok := details["Page"]; ok {
		t.Fatalf("the Go field name leaked into the details: %#v", details)
	}
}

func TestBindQuery_RejectsANonStructTarget(t *testing.T) {
	var n int
	if de := asDomain(t, BindQuery(httptest.NewRequest(http.MethodGet, "/", nil), &n)); de.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", de.StatusCode)
	}
}

type showInput struct {
	ID      int64    `path:"id" json:"id"`
	Org     string   `path:"org"`
	TraceID string   `header:"x-trace-id"`
	Accepts []string `header:"Accept-Language"`
	Name    string   `json:"name" validate:"required"`
	Age     int      `json:"age"`
	Secret  string   // untagged: no binder touches it
}

// serve registers h for pattern on a bare Mux and serves one request, so
// path values come from the real route match.
func serveRoute(t *testing.T, method, pattern string, req *http.Request, h Handler) *httptest.ResponseRecorder {
	t.Helper()
	m := NewMux()
	switch method {
	case http.MethodGet:
		m.Get(pattern, h)
	case http.MethodPost:
		m.Post(pattern, h)
	case http.MethodPut:
		m.Put(pattern, h)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	return rec
}

func TestBindPathAndHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/orgs/acme/notes/42", nil)
	req.Header.Set("X-Trace-Id", "t-1")
	req.Header.Add("Accept-Language", "en")
	req.Header.Add("Accept-Language", "es")
	// Each binder validates the whole struct, so the body's required
	// field is filled in by hand here.
	got := showInput{Name: "set-by-hand"}
	rec := serveRoute(t, http.MethodGet, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		if err := BindPath(c.Request, &got); err != nil {
			return err
		}
		return BindHeaders(c.Request, &got)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got.ID != 42 || got.Org != "acme" {
		t.Fatalf("path: %+v", got)
	}
	if got.TraceID != "t-1" || !reflect.DeepEqual(got.Accepts, []string{"en", "es"}) {
		t.Fatalf("headers: %+v", got)
	}

	// A path value that does not convert names the path parameter.
	req = httptest.NewRequest(http.MethodGet, "/orgs/acme/notes/forty-two", nil)
	rec = serveRoute(t, http.MethodGet, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		in := showInput{Name: "set-by-hand"}
		return BindPath(c.Request, &in)
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `path parameter \"id\"`) {
		t.Fatalf("bad path value: %d %s", rec.Code, rec.Body)
	}
}

func TestBindRequest_TheBodyCannotOverwriteThePathOrAnUntaggedField(t *testing.T) {
	body := `{"id": 999, "name": "note", "age": 3, "Secret": "from-body", "Org": "evil"}`
	req := httptest.NewRequest(http.MethodPut, "/orgs/acme/notes/42?ignored=1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-Trace-Id", "t-2")
	var got showInput
	rec := serveRoute(t, http.MethodPut, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		return BindRequest(c.Request, &got)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got.ID != 42 || got.Org != "acme" {
		t.Fatalf("the body overwrote a path field: %+v", got)
	}
	if got.Secret != "" {
		t.Fatalf("the body set an untagged field: %+v", got)
	}
	if got.Name != "note" || got.Age != 3 || got.TraceID != "t-2" {
		t.Fatalf("body and header: %+v", got)
	}
}

func TestBindRequest_ValidatesOnceAndReadsOnlyJSONBodies(t *testing.T) {
	// No body: the required body field fails validation, named as in JSON.
	req := httptest.NewRequest(http.MethodPost, "/orgs/acme/notes/1", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := serveRoute(t, http.MethodPost, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		var in showInput
		return BindRequest(c.Request, &in)
	})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"name"`) {
		t.Fatalf("empty body: %d %s", rec.Code, rec.Body)
	}

	// A form body is not read: BindRequest leaves it to BindForm.
	req = httptest.NewRequest(http.MethodPost, "/orgs/acme/notes/1", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = serveRoute(t, http.MethodPost, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		var in showInput
		return BindRequest(c.Request, &in)
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("form body: %d %s", rec.Code, rec.Body)
	}

	// A GET's body is never read.
	req = httptest.NewRequest(http.MethodGet, "/orgs/acme/notes/1", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = serveRoute(t, http.MethodGet, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		var in showInput
		return BindRequest(c.Request, &in)
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET body: %d %s", rec.Code, rec.Body)
	}

	// Malformed JSON is a 400; an oversized body a 413 — Bind's discipline.
	req = httptest.NewRequest(http.MethodPost, "/orgs/acme/notes/1", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", "application/json")
	rec = serveRoute(t, http.MethodPost, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		var in showInput
		return BindRequest(c.Request, &in)
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON: %d %s", rec.Code, rec.Body)
	}
	big := `{"name":"` + strings.Repeat("x", maxJSONBodyBytes) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/orgs/acme/notes/1", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec = serveRoute(t, http.MethodPost, "/orgs/{org}/notes/{id}", req, func(c *Context) error {
		var in showInput
		return BindRequest(c.Request, &in)
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized +json body: %d", rec.Code)
	}
	var env struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("oversized body code: %s", rec.Body)
	}
}

type embeddedPage struct {
	Page int `query:"page"`
}

func TestBindQuery_FlattensEmbeddedStructs(t *testing.T) {
	var in struct {
		embeddedPage
		Q string `query:"q"`
	}
	if err := BindQuery(httptest.NewRequest(http.MethodGet, "/?page=2&q=go", nil), &in); err != nil {
		t.Fatal(err)
	}
	if in.Page != 2 || in.Q != "go" {
		t.Fatalf("%+v", in)
	}
}
