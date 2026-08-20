// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ErrInvalidModulePolicy marks a module-declared PolicyRule or CSRFExempt
// entry that fails validation. Like ErrInvalidJobSpec and
// ErrInvalidWebhookSpec, it fails application boot: a malformed row that were
// silently skipped would leave the route dark with no trace — the exact
// "exit 0 with no effect" class the suite guards against.
var ErrInvalidModulePolicy = errors.New("nucleus: invalid module policy")

// policyActions is the closed set the default authz middleware can ever ask
// for (pkg/app/authz_default.go maps HTTP methods onto exactly these verbs,
// and "*" matches any of them in the Casbin model). A rule with any other
// action would load fine and never match a request — validation rejects it
// instead of letting it lie.
var policyActions = map[string]struct{}{
	"read": {}, "create": {}, "update": {}, "delete": {}, "*": {},
}

// PolicyRule is one RBAC row a module contributes to the application's
// default-deny enforcer, in the same shape as a rbac_policy.csv row:
// `p, Subject, Object, Action, Effect`.
//
// Object is a route path RELATIVE to the module's mount Prefix (a module
// without a Prefix declares full paths) and supports the enforcer's keyMatch
// wildcards ("/notes/*"). Action is one of the framework's CRUD verbs
// (read|create|update|delete) or "*" — not a raw HTTP method. Effect is
// "allow", "deny", or empty (which defaults to "allow", the overwhelmingly
// common case for a module opening its own routes).
//
// Module rows join the live in-memory ruleset only — they are never written
// to the host's policy file — and the Casbin policy effect
// (`some(allow) && !some(deny)`) means a deny row in the host's CSV always
// overrides a module's allow: the module proposes, the operator disposes.
type PolicyRule struct {
	Subject string
	Object  string
	Action  string
	Effect  string
}

// modulePolicyCarrier is the unexported view the framework type-asserts on a
// ModuleSpec to read its declared policies and CSRF exemptions. Like
// moduleIntrospector, only the framework's own moduleSpec[C] wrapper
// implements it, so a foreign ModuleSpec degrades gracefully: it simply
// contributes no rows. Kept off the public ModuleSpec contract on purpose.
type modulePolicyCarrier interface {
	policyRules() []PolicyRule
	csrfExemptPaths() []string
}

// resolveModulePolicyPath joins a module's mount prefix and a declared path,
// preserving the declared path's trailing slash and wildcards (path.Join
// would eat both, and the CSRF exemption match is a raw prefix check where
// "/notes/" vs "/notes" is a real semantic difference).
func resolveModulePolicyPath(prefix, p string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return p
	}
	return prefix + p
}

// validateModulePolicyDeclarations checks every module's Policies and
// CSRFExempt entries. It runs before app.New for the same fail-fast reason
// as config layer 5: a bad declaration must stop boot before any pool or
// telemetry is set up, naming the module and the offending entry.
func validateModulePolicyDeclarations(specs map[string]ModuleSpec) error {
	for _, spec := range sortedModuleSpecs(specs) {
		carrier, ok := spec.(modulePolicyCarrier)
		if !ok {
			continue
		}
		name := spec.Name()
		for i, rule := range carrier.policyRules() {
			if err := validatePolicyRule(rule); err != nil {
				return fmt.Errorf("%w: module %q Policies[%d]: %w", ErrInvalidModulePolicy, name, i, err)
			}
		}
		for i, p := range carrier.csrfExemptPaths() {
			if !strings.HasPrefix(p, "/") {
				return fmt.Errorf("%w: module %q CSRFExempt[%d]: path %q must start with \"/\" (it is resolved against the module Prefix and matched as a raw path prefix)", ErrInvalidModulePolicy, name, i, p)
			}
		}
	}
	return nil
}

func validatePolicyRule(r PolicyRule) error {
	if strings.TrimSpace(r.Subject) == "" {
		return fmt.Errorf("subject is empty (use %q for unauthenticated access)", "anonymous")
	}
	if !strings.HasPrefix(r.Object, "/") {
		return fmt.Errorf("object %q must be a route path starting with \"/\" (relative to the module Prefix; keyMatch wildcards like \"/notes/*\" are supported)", r.Object)
	}
	if _, ok := policyActions[r.Action]; !ok {
		return fmt.Errorf("action %q is not one the authz middleware ever requests — use read|create|update|delete (the framework's CRUD verbs, not HTTP methods) or \"*\"", r.Action)
	}
	switch r.Effect {
	case "", "allow", "deny":
		return nil
	default:
		return fmt.Errorf("effect %q is not \"allow\", \"deny\", or empty (empty defaults to allow)", r.Effect)
	}
}

// moduleCSRFExemptions aggregates every module's declared CSRF exemptions,
// resolved against each module's Prefix, in sorted module order. Inputs are
// already validated by validateModulePolicyDeclarations. Like the automatic
// webhook-prefix exemption, this must feed cfg.CSRFExemptPaths BEFORE
// app.New builds the middleware stack — the exemption list is captured by
// value inside the CSRF middleware closure and cannot be extended later,
// which is why this is a declarative field and not a closure (the closures
// only run after app.New).
func moduleCSRFExemptions(specs map[string]ModuleSpec) []string {
	var out []string
	for _, spec := range sortedModuleSpecs(specs) {
		carrier, ok := spec.(modulePolicyCarrier)
		if !ok {
			continue
		}
		for _, p := range carrier.csrfExemptPaths() {
			out = append(out, resolveModulePolicyPath(spec.Prefix(), p))
		}
	}
	return out
}

// applyModulePolicies loads every module's declared PolicyRules into the
// application's live enforcer, objects resolved against the module Prefix.
// Runs after app.New (the enforcer is consulted through a live pointer on
// every request, so any point before core.Run works) and before module
// OnStart, so module code always observes the final ruleset. With
// WithOpenAuthz there is no enforcer and the rows would be moot — they are
// skipped, matching the middleware they would feed.
func applyModulePolicies(core *app.App, specs []ModuleSpec) error {
	if core == nil || core.Authorizer == nil {
		return nil
	}
	for _, spec := range specs {
		carrier, ok := spec.(modulePolicyCarrier)
		if !ok {
			continue
		}
		rules := carrier.policyRules()
		if len(rules) == 0 {
			continue
		}
		for _, rule := range rules {
			obj := resolveModulePolicyPath(spec.Prefix(), rule.Object)
			var err error
			if rule.Effect == "deny" {
				err = core.Authorizer.Deny(rule.Subject, obj, rule.Action)
			} else {
				err = core.Authorizer.AddPolicy(rule.Subject, obj, rule.Action)
			}
			if err != nil {
				return fmt.Errorf("nucleus: module %q: loading policy (%s, %s, %s): %w", spec.Name(), rule.Subject, obj, rule.Action, err)
			}
		}
		moduleLogger(core).Info("nucleus: module policies loaded into the live enforcer (in-memory only — the host policy file is never written; a host deny row overrides these)",
			"module", spec.Name(), "rules", len(rules))
	}
	return nil
}
