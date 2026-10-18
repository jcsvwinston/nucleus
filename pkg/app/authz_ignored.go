// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"
)

// authzConfigIgnored lists the keys of the configuration that ask for
// authorization an application built WithoutDefaults() without WithAuthz()
// never builds — it has no RBAC enforcer, so nothing they say is enforced
// (NU-123):
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

// authzIgnoredError is the NU-123 refusal: the error New returns when the
// configuration asks for authorization an application built WithoutDefaults()
// without WithAuthz() would never enforce — a policy file that is written and
// never loaded reads, to whoever reviews the configuration, as routes that are
// protected. It names the keys, what to do about them — WithAuthz() first,
// the option that builds the enforcer as WithStorage, WithMail and
// WithRateLimit build theirs — and the notice; the headline is the v1.x log
// line's, so the detection rule the migration assistant documents still
// finds it.
func authzIgnoredError(effective *Config, keys []string) error {
	details := []string{"keys=" + strings.Join(keys, ", ")}
	for _, k := range keys {
		switch k {
		case "rbac_policy_file":
			details = append(details, "rbac_policy_file="+strings.TrimSpace(effective.RBACPolicyFile))
		case "metrics_public":
			details = append(details, "metrics_path="+strings.TrimSpace(effective.MetricsPath))
		}
	}
	return fmt.Errorf("authz configuration IGNORED: the configuration asks for authorization and this application is built "+
		"WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so none of it is enforced (%s) — add WithAuthz() "+
		"beside WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithAuthz(), or "+
		"app.New(cfg, app.WithoutDefaults(), app.WithAuthz()) — so the default-deny enforcer (ADR-004) loads the policy and "+
		"gates the metrics path; or remove the keys, authorize in the handlers, and keep the metrics path private at the "+
		"network layer (%s: refused since v2.0.0 without WithAuthz())", strings.Join(details, "; "), depAuthzIgnored)
}

// depAuthzIgnored is the deprecation notice for an application built
// WithoutDefaults() without WithAuthz() whose configuration asks for
// authorization it never builds: the combination was reported with an ERROR
// line on the v1.x line and refuses to start since v2.0.0 unless WithAuthz()
// builds the enforcer (docs/deprecations/DEP-2026-017-*.md).
const depAuthzIgnored = "DEP-2026-017"
