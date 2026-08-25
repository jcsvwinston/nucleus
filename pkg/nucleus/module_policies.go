// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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

// resolveModulePolicyObjects turns ONE declared object into the enforcer
// rows it means. Every object maps to exactly one row except the module's
// own root, which needs two — and that gap is what made ADR-022's promise
// stop short of the one path a module is most likely to serve (QCD-FW-13).
//
// The enforcer matches with keyMatch, where "/consola" and "/consola/" are
// different paths and neither implies the other. So the shortest object the
// validator used to accept, "/", resolved to "<prefix>/" and left
// "<prefix>" itself answering 403 — the module's landing page dead, and the
// operator back to the hand-written row the ADR exists to remove.
//
//	Object ""   → the module root, exactly.
//	Object "/"  → the module's whole surface: root AND subtree.
//	Object "/x" → "<prefix>/x", unchanged.
//
// Two spellings because they are different intents: a module whose root is
// public while its subtree is not declares "", and the common case — "these
// are my routes" — declares "/" and gets both rows.
func resolveModulePolicyObjects(prefix, object string) []string {
	root := strings.TrimSuffix(prefix, "/")
	switch object {
	case "":
		if root == "" {
			return []string{"/"}
		}
		return []string{root}
	case "/":
		if root == "" {
			return []string{"/", "/*"}
		}
		return []string{root, root + "/*"}
	}
	return []string{resolveModulePolicyPath(prefix, object)}
}

// resolveModuleCSRFPath resolves a declared exemption. The CSRF middleware
// matches by RAW PREFIX, not keyMatch, so the module root needs the
// opposite treatment to a policy object: "<prefix>" already covers the
// whole subtree, and it is the trailing slash that breaks things — a module
// at /api/v1/announcements exempting "/" produced "/api/v1/announcements/",
// which does not cover a POST to the collection path itself, so it stayed
// at 419 (QCD-FW-13).
func resolveModuleCSRFPath(prefix, p string) string {
	if p == "" || p == "/" {
		if root := strings.TrimSuffix(prefix, "/"); root != "" {
			return root
		}
		return "/"
	}
	return resolveModulePolicyPath(prefix, p)
}

// validateCSRFExemption rejects an exemption that would switch CSRF off for
// the WHOLE application (QCD-FW-15).
//
// ADR-022 accepts that mounting a module means trusting its routes. It does
// not follow that a module may unprotect its SIBLINGS: a module without a
// Prefix declaring "/" — the natural way to say "my routes" when there is
// no prefix to be relative to — resolved to "/" and disabled CSRF
// everywhere, with no operator veto and no line in the boot log.
func validateCSRFExemption(module, prefix, p string) error {
	if resolveModuleCSRFPath(prefix, p) != "/" {
		return nil
	}
	return fmt.Errorf("%w: module %q CSRFExempt %q resolves to \"/\", which would disable CSRF protection for EVERY route in the application, including other modules\u0027. Give the module a Prefix, or list the exact paths it needs exempted",
		ErrInvalidModulePolicy, module, p)
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
			if p != "" && !strings.HasPrefix(p, "/") {
				return fmt.Errorf("%w: module %q CSRFExempt[%d]: path %q must start with \"/\" (it is resolved against the module Prefix and matched as a raw path prefix; \"\" and \"/\" both mean the module's own surface)", ErrInvalidModulePolicy, name, i, p)
			}
			if err := validateCSRFExemption(name, spec.Prefix(), p); err != nil {
				return fmt.Errorf("%w (CSRFExempt[%d])", err, i)
			}
		}
	}
	return nil
}

func validatePolicyRule(r PolicyRule) error {
	if strings.TrimSpace(r.Subject) == "" {
		return fmt.Errorf("subject is empty (use %q for unauthenticated access)", "anonymous")
	}
	// "" is the module's own root; everything else is a route path
	// relative to the Prefix. Before this the shortest accepted object was
	// "/", which resolved to "<prefix>/" and left the module's root
	// unreachable (QCD-FW-13).
	if r.Object != "" && !strings.HasPrefix(r.Object, "/") {
		return fmt.Errorf("object %q must be a route path starting with \"/\" (relative to the module Prefix; \"\" is the module root, \"/\" is root plus subtree, and keyMatch wildcards like \"/notes/*\" are supported)", r.Object)
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
func moduleCSRFExemptions(specs map[string]ModuleSpec, logger *slog.Logger) []string {
	var out []string
	for _, spec := range sortedModuleSpecs(specs) {
		carrier, ok := spec.(modulePolicyCarrier)
		if !ok {
			continue
		}
		declared := carrier.csrfExemptPaths()
		if len(declared) == 0 {
			continue
		}
		resolved := make([]string, 0, len(declared))
		for _, p := range declared {
			resolved = append(resolved, resolveModuleCSRFPath(spec.Prefix(), p))
		}
		out = append(out, resolved...)
		// ADR-022 promises the operator "the boot log reports each
		// module's loaded rule count" for the Policies/CSRFExempt block.
		// Policies kept that promise; exemptions were appended in silence
		// (QCD-FW-15) — `grep -ci csrf` over a boot log returned 0, so
		// the one declaration that REMOVES a protection was the only one
		// leaving no trace. The resolved paths are logged, not the
		// declared ones, because what matters to an auditor is what the
		// middleware will actually match.
		if logger != nil {
			logger.Info("nucleus: module CSRF exemptions loaded (these routes will NOT be CSRF-checked; a module can only exempt paths under its own Prefix)",
				"module", spec.Name(), "count", len(resolved), "paths", strings.Join(resolved, " "))
		}
	}
	return out
}

// moduleTemplatesCarrier is the unexported view for Module.Templates —
// same off-contract pattern (and same graceful degradation for foreign
// ModuleSpec implementations) as modulePolicyCarrier.
type moduleTemplatesCarrier interface {
	moduleTemplates() fs.FS
}

// moduleTemplateOptions returns one app.WithTemplatesFS option per module
// that declares Templates, in sorted module order, each namespaced by the
// module's name — the wiring behind the Module.Templates contract.
func moduleTemplateOptions(specs map[string]ModuleSpec) []app.Option {
	var opts []app.Option
	for _, spec := range sortedModuleSpecs(specs) {
		carrier, ok := spec.(moduleTemplatesCarrier)
		if !ok {
			continue
		}
		if fsys := carrier.moduleTemplates(); fsys != nil {
			opts = append(opts, app.WithTemplatesFS(spec.Name(), fsys))
		}
	}
	return opts
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
		loaded := 0
		for _, rule := range rules {
			// One declaration can mean more than one row: the module root
			// needs both the exact path and the subtree (see
			// resolveModulePolicyObjects). The log reports rows LOADED,
			// not declarations, so the count matches the enforcer.
			for _, obj := range resolveModulePolicyObjects(spec.Prefix(), rule.Object) {
				var err error
				if rule.Effect == "deny" {
					err = core.Authorizer.Deny(rule.Subject, obj, rule.Action)
				} else {
					err = core.Authorizer.AddPolicy(rule.Subject, obj, rule.Action)
				}
				if err != nil {
					return fmt.Errorf("nucleus: module %q: loading policy (%s, %s, %s): %w", spec.Name(), rule.Subject, obj, rule.Action, err)
				}
				loaded++
			}
		}
		moduleLogger(core).Info("nucleus: module policies loaded into the live enforcer (in-memory only — the host policy file is never written; a host deny row overrides these)",
			"module", spec.Name(), "declarations", len(rules), "rules", loaded)
	}
	return nil
}
