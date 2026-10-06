// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/routedump"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// modulePolicyGap is one module whose policy rows an application without an
// RBAC enforcer discards (NU-126), with what discarding them costs.
type modulePolicyGap struct {
	// module is the module's name.
	module string
	// rows and deny count the module's declarations, and those of them that
	// are deny rows.
	rows, deny int
	// unguarded lists the module's routes, "METHOD pattern", that its rows
	// refuse an anonymous caller on the default stack — and that answer
	// anyone where there is no enforcer.
	unguarded []string
}

// unenforcedModulePolicies is NU-126: for an application that built no RBAC
// enforcer — one built WithoutDefaults() without WithAuthz() — which modules
// declare rows that would refuse something on the default stack, and what. routes holds the
// routes each module registered, by module name.
//
// A module is reported when it declares a deny row, or when one of its
// routes is one its rows do not open to the anonymous subject. Each route is
// put to a scratch enforcer holding the module's rows, as CheckModule puts
// it: the path with a value in every wildcard, the action the default-deny
// middleware derives from the method, every action for a route that answers
// every method. A module whose rows grant the anonymous subject every action
// on every route it serves — the accounts module's shape — loses nothing
// where nothing is enforced, and is not reported: saying so at ERROR would
// teach the reader to skip the line that matters.
func unenforcedModulePolicies(specs []ModuleSpec, routes map[string][]routedump.Route) []modulePolicyGap {
	var gaps []modulePolicyGap
	for _, spec := range specs {
		carrier, ok := spec.(modulePolicyCarrier)
		if !ok {
			continue
		}
		rules := carrier.policyRules()
		if len(rules) == 0 {
			continue
		}
		scratch, err := authz.New(slog.New(slog.DiscardHandler))
		if err != nil {
			continue
		}
		gap := modulePolicyGap{module: spec.Name(), rows: len(rules)}
		for _, rule := range rules {
			for _, obj := range resolveModulePolicyObjects(spec.Prefix(), rule.Object) {
				if rule.Effect == "deny" {
					_ = scratch.Deny(rule.Subject, obj, rule.Action)
				} else {
					_ = scratch.AddPolicy(rule.Subject, obj, rule.Action)
				}
			}
			if rule.Effect == "deny" {
				gap.deny++
			}
		}
		for _, r := range routes[spec.Name()] {
			actions := []string{methodAction(r.Method)}
			if r.Method == "*" {
				actions = []string{"read", "create", "update", "delete"}
			}
			for _, action := range actions {
				if !scratch.Can(authz.BootstrapSubject, samplePath(r.Pattern), action) {
					gap.unguarded = append(gap.unguarded, r.Method+" "+r.Pattern)
					break
				}
			}
		}
		if gap.deny > 0 || len(gap.unguarded) > 0 {
			gaps = append(gaps, gap)
		}
	}
	return gaps
}

// logModulePoliciesUnenforced is the NU-126 warning: one structured ERROR
// line, once per application, naming the modules whose rows are discarded,
// how many rows and deny rows they declare, the routes that answer anyone
// because of it, what to do, and that the combination stops booting at the
// major. ERROR for the reason NU-123's line is: a module that declares who
// may call its routes reads, to whoever reviews it, as routes that are
// protected.
func logModulePoliciesUnenforced(logger *slog.Logger, gaps []modulePolicyGap) {
	var modules, unguarded []string
	rows, deny := 0, 0
	for _, g := range gaps {
		modules = append(modules, fmt.Sprintf("%s (%d rows, %d deny)", g.module, g.rows, g.deny))
		rows += g.rows
		deny += g.deny
		unguarded = append(unguarded, g.unguarded...)
	}
	attrs := []any{"modules", strings.Join(modules, ", "), "rows", rows, "deny_rows", deny}
	if len(unguarded) > 0 {
		attrs = append(attrs, "unguarded", strings.Join(unguarded, ", "))
	}
	attrs = append(attrs,
		"fix", "add WithAuthz() beside WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithAuthz() — "+
			"so the default-deny enforcer loads the rows (ADR-004); or, for a module of your own, remove its rows and refuse "+
			"the callers in its own middleware or handlers",
		"deprecation", depModulePoliciesUnenforced+": from v2.0.0 this configuration refuses to start without WithAuthz()")
	logger.Error("module policies DISCARDED: mounted modules declare policy rows and this application is built "+
		"WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so none of them is enforced — the routes "+
		"they keep from anonymous callers answer anyone, and their deny rows refuse no one", attrs...)
}

// depModulePoliciesUnenforced is the deprecation notice for an application
// built WithoutDefaults() without WithAuthz() whose modules declare policy
// rows it never enforces: today they are discarded with an ERROR line at
// boot; from v2.0.0 the application refuses to start without WithAuthz(). It
// is the notice for the authorization keys such an application ignores
// (NU-123), which this extends (docs/deprecations/DEP-2026-017-*.md).
const depModulePoliciesUnenforced = "DEP-2026-017"
