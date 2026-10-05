// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package errreport carries, in a request's context, the reporters that want
// the error behind a 500 the framework answers — and hands them that error.
//
// The contract a reporter implements is interceptor.ErrorReporter (ADR-029,
// "Errors behind a 500"). It lives here, under internal/, so that the two
// halves of the framework that meet over it — the interceptor chain that
// finds a reporter and the router that has the error — share a context key
// nobody else can forge or read.
package errreport

import (
	"context"
	"net/http"
	"reflect"
)

// Reporter is interceptor.ErrorReporter, written out: the router cannot
// import the interceptor contract for one method set, and an interface with
// the same methods is the same contract to the compiler.
type Reporter interface {
	ReportError(r *http.Request, err error)
}

type key struct{}

// With returns ctx carrying rep after the reporters it already carries. A
// reporter already there is not added again: an interceptor that hands the
// request down with the writer it received makes the interceptor outside it
// look like a second reporter, and it would be told about every error twice.
func With(ctx context.Context, rep Reporter) context.Context {
	if rep == nil {
		return ctx
	}
	have, _ := ctx.Value(key{}).([]Reporter)
	for _, r := range have {
		if same(r, rep) {
			return ctx
		}
	}
	next := make([]Reporter, len(have), len(have)+1)
	copy(next, have)
	return context.WithValue(ctx, key{}, append(next, rep))
}

// Report hands err to every reporter the request's context carries, in the
// order the interceptors that installed them were declared, and returns how
// many it told. A reporter that panics does not stop the ones after it, and
// does not turn the 500 being answered into a crashed request: a reporter
// is a bystander to the error, not part of handling it.
func Report(r *http.Request, err error) int {
	if r == nil || err == nil {
		return 0
	}
	reps, _ := r.Context().Value(key{}).([]Reporter)
	for _, rep := range reps {
		func() {
			defer func() { _ = recover() }()
			rep.ReportError(r, err)
		}()
	}
	return len(reps)
}

// same reports whether a and b are the same reporter. Two interface values
// whose dynamic type is not comparable cannot be compared with == (it
// panics), so those are treated as different reporters.
func same(a, b Reporter) bool {
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb || !ta.Comparable() {
		return false
	}
	return a == b
}
