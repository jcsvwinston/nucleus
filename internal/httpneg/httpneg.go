// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package httpneg reads what a client asked for in its Accept header and
// carries the application's choice of error shape on the request context.
//
// It is the one place both error writers — pkg/errors (WriteError, used by
// the authorizer and the bearer middleware) and pkg/router (handler errors,
// the router's own 404 and 405, the timeout) — take the same decision from,
// so a client gets one shape for every error whoever wrote it.
package httpneg

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// Media types the error writers choose between.
const (
	JSON    = "application/json"
	Problem = "application/problem+json"
	HTML    = "text/html"
	Text    = "text/plain"
)

type problemDefaultKey struct{}

// WithProblemDefault marks ctx: errors answered on this request are problem
// details (RFC 9457) whatever the Accept header prefers, because the
// application chose that shape.
func WithProblemDefault(ctx context.Context) context.Context {
	return context.WithValue(ctx, problemDefaultKey{}, true)
}

// ProblemDefault reports whether the application chose problem details as
// its error shape for this request.
func ProblemDefault(ctx context.Context) bool {
	v, _ := ctx.Value(problemDefaultKey{}).(bool)
	return v
}

// WantsProblem reports whether an error answered to r is a problem details
// document rather than the framework's envelope: always when the
// application opted in, otherwise only when the client prefers
// application/problem+json to application/json (a strictly higher
// quality). A client that sends no Accept header, or */*, gets the envelope
// — the shape every client got before problem details existed.
func WantsProblem(r *http.Request) bool {
	if r == nil {
		return false
	}
	if ProblemDefault(r.Context()) {
		return true
	}
	accept := AcceptHeader(r)
	return Quality(accept, Problem) > Quality(accept, JSON)
}

// ProblemContentType is the Content-Type a problem details body is sent
// with: application/problem+json, unless the client accepts application/json
// and not application/problem+json — then application/json, so a client that
// named only the plain type is not handed a type it did not list.
func ProblemContentType(r *http.Request) string {
	if r == nil {
		return Problem
	}
	accept := AcceptHeader(r)
	if Quality(accept, Problem) == 0 && Quality(accept, JSON) > 0 {
		return JSON
	}
	return Problem
}

// WantsJSONError reports whether a client should get the router's own
// errors (the 404 for a path nobody serves, the 405 for a method the path
// does not take) as JSON rather than as Go's plain text: when it prefers a
// JSON type to both text/html and text/plain. A browser (text/html first)
// and a client that sends */* or nothing keep the plain text.
func WantsJSONError(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := AcceptHeader(r)
	best := Quality(accept, JSON)
	if q := Quality(accept, Problem); q > best {
		best = q
	}
	if best == 0 {
		return false
	}
	text := Quality(accept, HTML)
	if q := Quality(accept, Text); q > text {
		text = q
	}
	return best > text
}

// AcceptHeader joins every Accept field of r into one list.
func AcceptHeader(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.Join(r.Header.Values("Accept"), ",")
}

// acceptRange is one media range of an Accept header with its quality.
type acceptRange struct {
	typ, sub string
	q        float64
}

func parseAccept(accept string) []acceptRange {
	var out []acceptRange
	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mt, params, _ := strings.Cut(part, ";")
		typ, sub, ok := strings.Cut(strings.ToLower(strings.TrimSpace(mt)), "/")
		if !ok || typ == "" || sub == "" {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
				continue
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			q = f
		}
		out = append(out, acceptRange{typ: typ, sub: sub, q: q})
	}
	return out
}

// Quality returns the quality the Accept header gives mediaType: that of
// the most specific range that matches it (RFC 9110 §12.5.1 — type/subtype
// over type/* over */*), 0 when none does. An empty header accepts every
// type at 1, as the RFC says a request without the field does.
func Quality(accept, mediaType string) float64 {
	if strings.TrimSpace(accept) == "" {
		return 1
	}
	typ, sub, _ := strings.Cut(strings.ToLower(mediaType), "/")
	best, spec := 0.0, -1
	for _, ar := range parseAccept(accept) {
		s := -1
		switch {
		case ar.typ == typ && ar.sub == sub:
			s = 2
		case ar.typ == typ && ar.sub == "*":
			s = 1
		case ar.typ == "*" && ar.sub == "*":
			s = 0
		}
		if s < 0 {
			continue
		}
		if s > spec || (s == spec && ar.q > best) {
			best, spec = ar.q, s
		}
	}
	return best
}

// Ranked returns the offers the Accept header accepts (quality above 0),
// highest quality first; offers of equal quality keep the order they were
// given in, which is the server's preference.
func Ranked(accept string, offers ...string) []string {
	type scored struct {
		offer string
		q     float64
	}
	var acc []scored
	for _, o := range offers {
		if q := Quality(accept, o); q > 0 {
			acc = append(acc, scored{o, q})
		}
	}
	// Insertion sort: stable, and the lists are a handful long.
	for i := 1; i < len(acc); i++ {
		for j := i; j > 0 && acc[j].q > acc[j-1].q; j-- {
			acc[j], acc[j-1] = acc[j-1], acc[j]
		}
	}
	out := make([]string, len(acc))
	for i, s := range acc {
		out[i] = s.offer
	}
	return out
}
