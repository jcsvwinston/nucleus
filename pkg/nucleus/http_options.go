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

// WithProblemDetails states that RFC 9457 problem details
// (application/problem+json) are the shape of every error the application
// answers: handler errors, binding failures, the router's own 404 and 405,
// timeouts, and the refusals of the CSRF middleware, the rate limiter, the
// authorizer and the bearer middleware. That is the default since v2.0.0
// (DEP-2026-022), so the option changes nothing; it makes the choice
// explicit in the composition root and is refused beside WithErrorEnvelope.
// Mirrors app.WithProblemDetails.
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

// WithErrorEnvelope makes the framework's envelope, {"error": {"code",
// "message", "details"}}, the shape of every error the application answers
// instead of the default RFC 9457 problem details (DEP-2026-022). A client
// that negotiates application/problem+json through its Accept header still
// gets problem details. It is the opposite of WithProblemDetails, and
// passing both is refused. Mirrors app.WithErrorEnvelope.
func WithErrorEnvelope() Option { return app.WithErrorEnvelope() }

// WithErrorEnvelope appends app.WithErrorEnvelope() to the option chain: the
// framework's envelope for every error the application answers. See the
// package-level WithErrorEnvelope.
func (b *AppBuilder) WithErrorEnvelope() *AppBuilder {
	if b.err != nil {
		return b
	}
	b.a.Options = append(b.a.Options, WithErrorEnvelope())
	return b
}
