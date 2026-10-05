// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package errreport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pointerReporter struct{ told int }

func (p *pointerReporter) ReportError(*http.Request, error) { p.told++ }

// A writer that is a struct value holding a slice cannot be compared with
// ==; installing it twice must not panic the request, and it counts as two.
type valueReporter struct {
	told  *int
	slice []string
}

func (v valueReporter) ReportError(*http.Request, error) { *v.told++ }

func TestWith_SameReporterOnceAndIncomparableOnesSafely(t *testing.T) {
	p := &pointerReporter{}
	n := 0
	v := valueReporter{told: &n, slice: []string{"x"}}

	ctx := With(context.Background(), p)
	ctx = With(ctx, p)
	ctx = With(ctx, v)
	ctx = With(ctx, v)
	ctx = With(ctx, nil)

	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	if got := Report(r, errors.New("boom")); got != 3 {
		t.Fatalf("Report told %d reporters, want 3 (the pointer once, the value twice)", got)
	}
	if p.told != 1 || n != 2 {
		t.Fatalf("pointer told %d, value told %d", p.told, n)
	}
	if Report(r, nil) != 0 || Report(nil, errors.New("x")) != 0 {
		t.Fatal("no error, or no request, reports nothing")
	}
}

// A context derived after With does not change what an earlier one carries.
func TestWith_DoesNotAliasTheParentsList(t *testing.T) {
	a, b, c := &pointerReporter{}, &pointerReporter{}, &pointerReporter{}
	base := With(context.Background(), a)
	left := With(base, b)
	right := With(base, c)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Report(req.WithContext(left), errors.New("x"))
	Report(req.WithContext(right), errors.New("x"))
	if a.told != 2 || b.told != 1 || c.told != 1 {
		t.Fatalf("told a=%d b=%d c=%d, want 2 1 1", a.told, b.told, c.told)
	}
}
