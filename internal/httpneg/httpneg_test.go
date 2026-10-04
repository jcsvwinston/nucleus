// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package httpneg

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestQualityTakesTheMostSpecificRange(t *testing.T) {
	cases := []struct {
		accept, typ string
		want        float64
	}{
		{"", JSON, 1},
		{"*/*", JSON, 1},
		{"application/json", JSON, 1},
		{"application/json", Problem, 0},
		{"application/*;q=0.4", Problem, 0.4},
		{"*/*;q=0.1, application/*;q=0.5, application/json;q=0.9", JSON, 0.9},
		{"application/json;q=0.9, */*;q=1", JSON, 0.9},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", JSON, 0.8},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", HTML, 1},
		{"application/json;q=abc", JSON, 0},
		{"APPLICATION/JSON", JSON, 1},
		{"garbage", JSON, 0},
	}
	for _, c := range cases {
		if got := Quality(c.accept, c.typ); got != c.want {
			t.Errorf("Quality(%q, %q) = %v, want %v", c.accept, c.typ, got, c.want)
		}
	}
}

func TestRankedKeepsServerOrderOnTies(t *testing.T) {
	offers := []string{"application/json", "application/xml", "text/plain"}
	cases := []struct {
		accept string
		want   []string
	}{
		{"", offers},
		{"*/*", offers},
		{"text/plain, application/json;q=0.5", []string{"text/plain", "application/json"}},
		{"application/xml;q=0.9, application/json;q=0.9", []string{"application/json", "application/xml"}},
		{"image/png", nil},
	}
	for _, c := range cases {
		got := Ranked(c.accept, offers...)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Ranked(%q) = %v, want %v", c.accept, got, c.want)
		}
	}
}

func TestWantsProblem(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"*/*", false},
		{"application/json", false},
		{"application/problem+json", true},
		{"application/problem+json, application/json", false},
		{"application/problem+json, application/json;q=0.9", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		if got := WantsProblem(r); got != c.want {
			t.Errorf("WantsProblem(Accept %q) = %v, want %v", c.accept, got, c.want)
		}
		// The application's choice wins over every Accept.
		r = r.WithContext(WithProblemDefault(context.Background()))
		if !WantsProblem(r) {
			t.Errorf("WantsProblem with the application's opt-in (Accept %q) = false", c.accept)
		}
	}
}

func TestProblemContentTypeFallsBackToTheTypeTheClientNamed(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept", "application/json")
	if got := ProblemContentType(r); got != JSON {
		t.Errorf("Accept application/json → %q, want %q", got, JSON)
	}
	r.Header.Set("Accept", "*/*")
	if got := ProblemContentType(r); got != Problem {
		t.Errorf("Accept */* → %q, want %q", got, Problem)
	}
}

func TestWantsJSONError(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"*/*", false},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false},
		{"text/plain", false},
		{"application/json", true},
		{"application/problem+json", true},
		{"application/json, text/html", false},
		{"application/json, text/html;q=0.5", true},
		{"application/*", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		if got := WantsJSONError(r); got != c.want {
			t.Errorf("WantsJSONError(Accept %q) = %v, want %v", c.accept, got, c.want)
		}
	}
}
