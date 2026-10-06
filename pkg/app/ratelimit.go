// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"log/slog"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// WithRateLimit mounts the rate limiter the configuration declares —
// rate_limit_requests, rate_limit_window, rate_limit_burst,
// rate_limit_by_route, rate_limit_by_role — on an application built
// WithoutDefaults(), which mounts none. The limiter goes where the default
// path puts it: after the API-key read (WithAPIKeys) and before the request
// interceptors, so it keys a request by the identity already in its
// context — the key's owner, per tenant — and by its client IP otherwise.
// An application built WithoutDefaults() decodes no bearer token ahead of
// it unless it also carries WithAuthz(), which decodes it where the default
// stack does (NU-125): without that option a bearer-authenticated request is
// keyed by its address and rate_limit_by_role sees every caller as
// anonymous; with it, by the token's user and role.
//
// It mounts nothing while rate_limit_requests is 0, the default: there is no
// limit to enforce. That is why the api starter carries it — a
// rate_limit_requests line in nucleus.yml is all it then takes.
//
// Without it, an application built WithoutDefaults() whose configuration
// sets rate_limit_requests above 0 still starts with the limit unenforced,
// as it always did, but no longer without a word: the boot log carries one
// ERROR line naming this option (NU-122). From v2.0.0 that configuration
// refuses to start (DEP-2026-016).
//
// On an application built with the defaults it changes nothing: the limiter
// is one of them. It is not router.WithRateLimit, which takes its numbers in
// Go and mounts the limiter outermost, before any identity is known.
func WithRateLimit() Option {
	return func(o *appOptions) { o.withRateLimit = true }
}

// mountRateLimit mounts the limiter rate_limit_* describes, and nothing while
// rate_limit_requests is 0. The default path calls it right after the
// API-key read; an application built WithoutDefaults() calls it there too,
// through WithRateLimit.
func (a *App) mountRateLimit() {
	if a.Config == nil || a.Config.RateLimitRequests <= 0 {
		return
	}
	a.Router.Use(router.RateLimitFromPolicy(router.RateLimitPolicy{
		Requests: a.Config.RateLimitRequests,
		Window:   a.Config.RateLimitWindow,
		Burst:    a.Config.RateLimitBurst,
		ByRoute:  a.Config.RateLimitByRoute,
		ByRole:   a.Config.RateLimitByRole,
	}))
}

// rateLimitIgnored reports whether the configuration sets a limit — which
// an application built WithoutDefaults() without WithRateLimit() never
// mounts. The value is the declaration: its default, 0, is no limit, and
// nothing but the configuration writes it (unlike mail_driver, which
// nucleustest rewrites), so a Config built in Go that sets it is reported
// too.
func rateLimitIgnored(effective *Config) bool {
	return effective.RateLimitRequests > 0
}

// logRateLimitIgnored is the NU-122 warning, NU-99's for the limiter: one
// structured ERROR line, once per application, saying the configured limit
// is IGNORED, which option mounts it, and that the configuration stops
// booting at the major. ERROR because a limit that is written and not
// enforced is the one a reviewer reads and trusts — a login form open to
// brute force behind a configuration that says it is not; not a refusal,
// because it booted yesterday.
func logRateLimitIgnored(logger *slog.Logger, effective *Config) {
	logger.Error("rate_limit_requests IGNORED: the configuration sets a rate limit and this application is built "+
		"WithoutDefaults() without WithRateLimit(), so no limiter is mounted and no request is refused",
		"requests", effective.RateLimitRequests,
		"window", effective.RateLimitWindow.String(),
		"fix", "add WithRateLimit() beside WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithRateLimit(), "+
			"or app.New(cfg, app.WithoutDefaults(), app.WithRateLimit()) — or set rate_limit_requests to 0",
		"deprecation", depRateLimitIgnored+": from v2.0.0 this configuration refuses to start")
}

// depRateLimitIgnored is the deprecation notice for an application built
// WithoutDefaults() whose configuration sets a rate limit it does not mount:
// today the limit is ignored with an ERROR line at boot; from v2.0.0 the
// application refuses to start (docs/deprecations/DEP-2026-016-*.md).
const depRateLimitIgnored = "DEP-2026-016"
