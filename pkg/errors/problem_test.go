// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewProblem_CarriesTheDomainErrorAndTheRequestPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/notes?draft=1", nil)
	err := fmt.Errorf("wrapped: %w", ValidationFailed(map[string]string{"title": "this field is required"}))
	p := NewProblem(r, err)
	if p.Type != "about:blank" || p.Title != "Unprocessable Entity" || p.Status != http.StatusUnprocessableEntity ||
		p.Detail != "validation failed" || p.Instance != "/api/notes" || p.Code != "VALIDATION_FAILED" {
		t.Fatalf("%+v", p)
	}
	if d, _ := p.Details.(map[string]string); d["title"] == "" {
		t.Fatalf("details: %#v", p.Details)
	}
}

func TestNewProblem_AnUnclassifiedErrorIsA500WithoutItsText(t *testing.T) {
	p := NewProblem(nil, errors.New("pq: relation \"secrets\" does not exist"))
	if p.Status != http.StatusInternalServerError || p.Code != "INTERNAL_ERROR" || strings.Contains(p.Detail, "secrets") || p.Instance != "" {
		t.Fatalf("%+v", p)
	}
	// A status outside the HTTP range is a 500, not a panic in WriteHeader.
	if p := NewProblem(nil, &DomainError{Code: "X", Message: "m"}); p.Status != http.StatusInternalServerError {
		t.Fatalf("status 0 → %d", p.Status)
	}
}

func TestWriteProblem_ContentTypeAndBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	WriteProblem(rec, r, Conflict("taken"))
	if rec.Code != http.StatusConflict || rec.Header().Get("Content-Type") != "application/problem+json; charset=utf-8" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "title", "status", "detail", "instance", "code"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("member %q missing: %s", k, rec.Body)
		}
	}
	if _, ok := m["details"]; ok {
		t.Fatalf("an error without details must not carry the member: %s", rec.Body)
	}
}

func TestWriteError_AnswersTheEnvelopeUnlessAskedForAProblem(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	WriteError(rec, r, Forbidden("no"), nil)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":{"code":"FORBIDDEN","message":"no"}}` {
		t.Fatalf("envelope changed: %s", got)
	}
	r.Header.Set("Accept", "application/problem+json")
	rec = httptest.NewRecorder()
	WriteError(rec, r, Forbidden("no"), nil)
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), ProblemContentType) || !strings.Contains(rec.Body.String(), `"status":403`) {
		t.Fatalf("problem: %q %s", rec.Header().Get("Content-Type"), rec.Body)
	}
}
