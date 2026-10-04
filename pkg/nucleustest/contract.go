// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"encoding/json"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

// The contract from the test's side (A10). An application that serves its
// OpenAPI document (WithOpenAPIDocument) can be held to it from any test:
// AssertConforms looks up the operation the document declares for the
// request that produced a response and checks the response against it —
// a declared status, a declared content type, a body that matches the
// schema. A handler that starts returning a field the document does not
// promise as required, or drops one it does, fails the test that calls it
// instead of the client that reads it.

// Document returns the OpenAPI document the application serves, fetched
// from its route once per server. It fails the test when the application
// serves none.
func (s *Server) Document() *openapi.Document {
	s.tb.Helper()
	if s.document != nil {
		return s.document
	}
	if s.app.APIDocument == nil {
		s.tb.Fatalf("nucleustest: the application serves no OpenAPI document: build it with WithOpenAPIDocument to test against its contract")
	}
	pattern := strings.TrimSpace(s.app.APIDocument.Pattern)
	if pattern == "" {
		pattern = "/openapi.json"
	}
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	resp := s.Get(pattern)
	if resp.Status != 200 {
		s.tb.Fatalf("nucleustest: GET %s → %d: %s", pattern, resp.Status, truncate(resp.Body, 512))
	}
	var doc openapi.Document
	if err := json.Unmarshal(resp.Body, &doc); err != nil {
		s.tb.Fatalf("nucleustest: the document at %s is not an OpenAPI document: %v", pattern, err)
	}
	s.document = &doc
	return s.document
}

// AssertConforms fails the test when resp departs from the operation the
// application's document declares for the request that produced it,
// listing every departure.
func (s *Server) AssertConforms(tb testingTB, resp Response) {
	tb.Helper()
	doc := s.Document()
	route, ok := doc.FindOperation(resp.Method, resp.Path)
	if !ok {
		tb.Fatalf("nucleustest: the document declares no operation for %s %s", resp.Method, resp.Path)
		return
	}
	if errs := doc.ValidateResponse(route, resp.Status, resp.Header, resp.Body); len(errs) > 0 {
		lines := make([]string, len(errs))
		for i, e := range errs {
			lines[i] = "  " + e.String()
		}
		tb.Fatalf("nucleustest: %s %s → %d does not conform to %s %s in the document:\n%s\nbody: %s",
			resp.Method, resp.Path, resp.Status, strings.ToUpper(resp.Method), route.Template, strings.Join(lines, "\n"), truncate(resp.Body, 512))
	}
}
