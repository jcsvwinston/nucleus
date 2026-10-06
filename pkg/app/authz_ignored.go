// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"log/slog"
	"strings"
)

// authzConfigIgnored lists the keys of the configuration that ask for
// authorization an application built WithoutDefaults() never builds — it
// has no RBAC enforcer, so nothing they say is enforced (NU-123):
//
//   - rbac_policy_file, whenever it names a file: the policy is never read,
//     and no route is authorized by it;
//   - metrics_public: false, while the metrics endpoint is served: on the
//     default stack it takes the endpoint off the anonymous allow-list, and
//     here the endpoint answers anyone.
//
// The values are the declarations. rbac_policy_file defaults to empty and
// nothing but the configuration writes it. metrics_public defaults to true
// in both loaders; a Config built in Go that serves metrics and leaves it
// false asks for what the default stack would give it — a gated endpoint —
// and is reported as well.
func authzConfigIgnored(effective *Config, metricsServed bool) []string {
	var keys []string
	if strings.TrimSpace(effective.RBACPolicyFile) != "" {
		keys = append(keys, "rbac_policy_file")
	}
	if metricsServed && !effective.MetricsPublic {
		keys = append(keys, "metrics_public")
	}
	return keys
}

// logAuthzIgnored is the NU-123 warning: one structured ERROR line, once per
// application, naming the authorization keys an application built
// WithoutDefaults() ignores, what to do about them, and that the
// configuration stops booting at the major. ERROR for the reason the
// limiter's is: a policy file that is written and never loaded reads, to
// whoever reviews the configuration, as routes that are protected.
//
// There is no option that builds the enforcer on such an application, as
// WithStorage, WithMail and WithRateLimit build theirs: authorization there
// is default-deny over every route (ADR-004), which is the default stack.
func logAuthzIgnored(logger *slog.Logger, effective *Config, keys []string) {
	attrs := []any{"keys", strings.Join(keys, ", ")}
	for _, k := range keys {
		switch k {
		case "rbac_policy_file":
			attrs = append(attrs, "rbac_policy_file", strings.TrimSpace(effective.RBACPolicyFile))
		case "metrics_public":
			attrs = append(attrs, "metrics_path", strings.TrimSpace(effective.MetricsPath))
		}
	}
	attrs = append(attrs,
		"fix", "build the application without WithoutDefaults() so the default stack builds the enforcer "+
			"(default-deny, ADR-004) and loads the policy — or remove the keys, authorize in the handlers, "+
			"and keep the metrics path private at the network layer",
		"deprecation", depAuthzIgnored+": from v2.0.0 this configuration refuses to start")
	logger.Error("authz configuration IGNORED: the configuration asks for authorization and this application is built "+
		"WithoutDefaults(), which builds no RBAC enforcer, so none of it is enforced", attrs...)
}

// depAuthzIgnored is the deprecation notice for an application built
// WithoutDefaults() whose configuration asks for authorization it never
// builds: today the keys are ignored with an ERROR line at boot; from v2.0.0
// the application refuses to start (docs/deprecations/DEP-2026-017-*.md).
const depAuthzIgnored = "DEP-2026-017"
