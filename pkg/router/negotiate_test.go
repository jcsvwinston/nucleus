// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

type negNote struct {
	XMLName xml.Name `json:"-" xml:"note"`
	ID      int      `json:"id" xml:"id"`
	Title   string   `json:"title" xml:"title"`
}

func (n negNote) String() string { return n.Title }

func TestNegotiate_OneHandlerEveryRepresentation(t *testing.T) {
	r := New(quietLogger())
	r.Get("/note", func(c *Context) error {
		return Negotiate(c.Writer, c.Request, http.StatusOK, negNote{ID: 1, Title: "hello"})
	})
	r.Get("/map", func(c *Context) error {
		return Negotiate(c.Writer, c.Request, http.StatusOK, map[string]int{"n": 1})
	})

	cases := []struct {
		path, accept, wantType, wantBody string
	}{
		{"/note", "", "application/json", `"title":"hello"`},
		{"/note", "*/*", "application/json", `"title":"hello"`},
		{"/note", "application/xml", "application/xml", "<title>hello</title>"},
		{"/note", "text/xml", "text/xml", "<note>"},
		{"/note", "text/plain", "text/plain", "hello"},
		{"/note", "text/plain;q=0.5, application/xml;q=0.8", "application/xml", "<note>"},
		{"/note", "application/json;q=0.1, text/plain", "text/plain", "hello"},
		// text/* covers both text types: the server's order puts XML first.
		{"/note", "application/json;q=0.1, text/*", "text/xml", "<note>"},
		// A browser asks for XML before */* — it gets what it ranked.
		{"/note", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", "application/xml", "<note>"},
		// A map has no XML form: the next acceptable type answers.
		{"/map", "application/xml, application/json;q=0.5", "application/json", `"n":1`},
	}
	for _, c := range cases {
		rec := doShape(t, r, http.MethodGet, c.path, c.accept)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), c.wantType) ||
			!strings.Contains(rec.Body.String(), c.wantBody) {
			t.Errorf("GET %s Accept %q → %d %q %s", c.path, c.accept, rec.Code, rec.Header().Get("Content-Type"), rec.Body)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept") {
			t.Errorf("GET %s Accept %q: no Vary: Accept", c.path, c.accept)
		}
	}
}

func TestNegotiate_NothingAcceptableIsA406InTheErrorShape(t *testing.T) {
	r := New(quietLogger())
	r.Get("/map", func(c *Context) error {
		return Negotiate(c.Writer, c.Request, http.StatusOK, map[string]int{"n": 1})
	})
	rec := doShape(t, r, http.MethodGet, "/map", "application/xml")
	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	env := decodeEnvelope(t, rec)
	if env.Error.Code != "NOT_ACCEPTABLE" || env.Error.Details["available"] == nil {
		t.Fatalf("406: %+v", env.Error)
	}
	rec = doShape(t, r, http.MethodGet, "/map", "image/png, application/problem+json;q=0.1")
	p := decodeProblem(t, rec)
	if p.Status != http.StatusNotAcceptable {
		t.Fatalf("406 as a problem: %+v", p)
	}
}
