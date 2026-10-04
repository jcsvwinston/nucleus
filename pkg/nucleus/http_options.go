// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// Timeout gives the routes it wraps their own request timeout in place of
// the application's request_timeout — longer or shorter:
//
//	r.With(nucleus.Timeout(2 * time.Minute)).Get("/export", export) // more than the global 30s
//	r.With(nucleus.Timeout(2 * time.Second)).Get("/lookup", lookup) // fires first
//
// The duration counts from the moment the request reaches the route; the
// global deadline moves to it, so a longer one is not cut short. When the
// deadline passes the client gets a 503 in the framework's error shape
// (code TIMEOUT) and the handler's context is done with
// context.DeadlineExceeded. It also works as module Middleware, for every
// route of the module. See router.Timeout.
func Timeout(d time.Duration) Middleware { return routerpkg.Timeout(d) }

// WithProblemDetails makes RFC 9457 problem details
// (application/problem+json) the shape of every error the application
// answers: handler errors, binding failures, the router's own 404 and 405,
// timeouts, and the refusals of the CSRF middleware, the rate limiter, the
// authorizer and the bearer middleware. Without it the framework's
// envelope, {"error": {"code", "message", "details"}}, stays the default
// and a client gets problem details only when its Accept header prefers
// application/problem+json. Mirrors app.WithProblemDetails.
//
// The envelope remains the default until the next major so that no
// existing client sees its errors change shape; this option is how an
// application chooses the standard shape today.
func WithProblemDetails() Option { return app.WithProblemDetails() }

// WithProblemDetails appends app.WithProblemDetails() to the option chain:
// problem details for every error the application answers. See the
// package-level WithProblemDetails.
func (b *AppBuilder) WithProblemDetails() *AppBuilder {
	if b.err != nil {
		return b
	}
	b.a.Options = append(b.a.Options, WithProblemDetails())
	return b
}
