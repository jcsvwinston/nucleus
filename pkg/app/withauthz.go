// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// WithAuthz gives an application built WithoutDefaults() the default stack's
// authorization, and nothing else of that stack: the RBAC enforcer
// (App.Authorizer) with its default-deny middleware (ADR-004), mounted where
// the default stack mounts them.
//
// In request order, the layers it adds around the ones the application
// already carries are:
//
//  1. the bearer decode — a valid token's claims reach the context of every
//     request, so the API-key read, the rate limiter (WithRateLimit), the
//     request interceptors and the handlers see the caller's identity
//     (QCD-FW-25); a request without a token, or with an invalid one,
//     proceeds as anonymous;
//  2. the API-key read (WithAPIKeys), the rate limiter (WithRateLimit) and
//     the request interceptors, in that order, as without this option;
//  3. the default-deny gate, last: a request is allowed when one of its
//     subjects is — the token's user id and role, an API key's owner and
//     each of its scopes as `scope:<name>`, the account a session signed
//     in, then `anonymous`.
//
// The enforcer is built as the default stack builds it: from
// rbac_policy_file when set (a path that does not exist fails boot), from a
// policy file found in the default locations otherwise, and with the
// framework's bootstrap allow-list for the anonymous subject — /healthz,
// /livez, /readyz, /login, /.well-known/jwks.json, /static/* and /metrics,
// the last left out under metrics_public: false. The rows mounted modules
// declare in Module.Policies are loaded into it by pkg/nucleus.
//
// With no policy file and no module rows, default-deny means what it says:
// every registered route outside the allow-list — /debug/pprof under
// profiling_enabled and the /realtime/{topic} channels of WithRealtime
// among them — answers an anonymous request 403 until a row allows it, and
// a path no route serves answers 404. The boot log says so in one line:
// "authz: default-deny with 0 policy rows", with the routes that still
// answer anyone.
//
// What it changes on a core-only application: rbac_policy_file and
// metrics_public: false are enforced instead of reported as ignored
// (DEP-2026-017), modules' policy rows are loaded instead of discarded
// (DEP-2026-017), the profiler is guarded instead of reported as unguarded
// (DEP-2026-018), an API key's scopes are subjects of the gate, and the
// realtime topics answer only whom a policy grants. It is the explicit
// opt-in those notices accept at v2.0.0.
//
// WithOpenAuthz() switches the gate off here as on the default stack: the
// enforcer is still built and the bearer still decoded. On an application
// built with the defaults WithAuthz changes nothing: authorization is one of
// them. The api starter does not carry it; add it to the starter's chain —
// nucleus.New().FromConfigFile("nucleus.yml").WithoutDefaults().WithAuthz()
// — to authorize its routes against a policy.
func WithAuthz() Option {
	return func(o *appOptions) { o.withAuthz = true }
}

// buildAuthorizer builds the RBAC enforcer ADR-004 describes into
// App.Authorizer: the policy file rbac_policy_file names (or one found in the
// default locations) and the bootstrap allow-list for the anonymous subject,
// less /metrics under metrics_public: false. It says at boot what the
// enforcer then refuses, unless WithOpenAuthz() mounts no gate to consult it.
// The default stack calls it; an application built WithoutDefaults() calls
// it through WithAuthz.
func (a *App) buildAuthorizer(effective *Config) error {
	rbacPath, err := rbacPolicyPath(effective)
	if err != nil {
		// DX-2 corollary: an explicit rbac_policy_file that does not exist
		// used to boot the app into total default-deny with a WARN telling
		// the operator to set the key they had already set. A filename typo
		// fails startup naming the path instead.
		return wrapOp("New RBAC policy file", err)
	}
	enforcer, err := authz.New(a.Logger, rbacPath)
	if err != nil {
		return wrapOp("New RBAC enforcer", err)
	}
	// The rows the policy file carries, counted before the allow-list is
	// seeded so the boot line says what the operator wrote.
	rows, _ := enforcer.GetPolicy()
	roles, _ := enforcer.GetGroupingPolicy()
	// `metrics_public: false` keeps the Prometheus endpoint out of the
	// anonymous bootstrap allow-list, so it falls under default-deny and
	// requires an explicit policy grant (or WithOpenAuthz). Default true —
	// the historical scrape-friendly posture, documented in Config.
	var seedSkip []string
	if !effective.MetricsPublic {
		seedSkip = append(seedSkip, "/metrics")
	}
	if err := enforcer.SeedBootstrapAllowListExcluding(seedSkip...); err != nil {
		return wrapOp("New RBAC bootstrap allow-list", err)
	}
	a.Authorizer = enforcer
	if !a.openAuthz {
		a.logDefaultDeny(rbacPath, len(rows), len(roles), seedSkip)
	}
	return nil
}

// logDefaultDeny is the boot line of an enforcing application: how many
// policy rows the enforcer loaded and from where, and what that means for a
// request — a route no row allows answers 403. With no rows it is a WARN
// naming the routes that still answer anyone and how rows get written: an
// application whose every route answers 403 is a working default-deny, and
// the most common first surprise.
//
// Module rows are not counted: pkg/nucleus loads them after app.New, and
// logs a line per module saying how many.
func (a *App) logDefaultDeny(policyPath string, rows, roles int, skipped []string) {
	noun := "policy rows"
	if rows == 1 {
		noun = "policy row"
	}
	from := ""
	if policyPath != "" {
		from = " from " + policyPath
	}
	attrs := []any{"bootstrap_routes", strings.Join(bootstrapRoutes(skipped), ", ")}
	if policyPath != "" {
		attrs = append([]any{"policy_path", policyPath, "policy_rows", rows, "role_rows", roles}, attrs...)
	}
	head := fmt.Sprintf("authz: default-deny with %d %s%s — ", rows, noun, from)
	if rows == 0 {
		a.Logger.Warn(head+"an anonymous request reaches only the bootstrap routes, and every other registered route answers 403 "+
			"until a row allows it: write rows in rbac_policy_file, declare Module.Policies on a mounted module, or call "+
			"App.Authorizer.AddPolicy (ADR-004); app.WithOpenAuthz() skips enforcement entirely", attrs...)
		return
	}
	a.Logger.Info(head+"a registered route no row allows answers 403", attrs...)
}

// bootstrapRoutes lists the routes the bootstrap allow-list opens to the
// anonymous subject, less the ones the configuration took off it.
func bootstrapRoutes(skipped []string) []string {
	var routes []string
	for _, rule := range authz.BootstrapAllowList() {
		keep := true
		for _, s := range skipped {
			if rule.Object == s {
				keep = false
			}
		}
		if keep {
			routes = append(routes, rule.Object)
		}
	}
	return routes
}

// mountBearerDecode decodes a bearer token, when the application verifies
// tokens, ahead of everything that asks who the caller is: the API-key read,
// the rate limiter, the request interceptors and the gate (QCD-FW-1,
// QCD-FW-25). Optional: a request without a token, or with an invalid one,
// proceeds claimless and is the anonymous subject.
func (a *App) mountBearerDecode() {
	if a.JWT != nil {
		a.Router.Use(a.JWT.OptionalJWTMiddleware())
	}
}

// mountAuthzGate mounts the default-deny middleware over the enforcer
// buildAuthorizer built — last in the chain, after the identity, the limiter
// and the interceptors — or, under WithOpenAuthz(), nothing, with the WARN
// that says so.
func (a *App) mountAuthzGate() {
	if a.openAuthz {
		a.Logger.Warn(
			"authz: WithOpenAuthz() in effect — no authorization checks will run on user routes. " +
				"This is unsafe outside development (see ADR-004).",
		)
		return
	}
	a.Router.Use(buildDefaultAuthzMiddleware(a.Authorizer, a.Logger, a.Session))
}
