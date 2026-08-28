// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/app"
)

// checkAuth reviews the authentication chain from the configuration alone.
//
// WHAT IT DELIBERATELY DOES NOT DO: judge whether a backend is registered.
// This runs inside the `nucleus` binary, which does not import the
// application's provider modules — `auth.RegisteredBackends()` here is the
// CLI's registry, not the app's. Reporting "ldap is not registered" would
// be a false alarm on every correctly configured deployment, and a check
// that cries wolf is a check operators learn to ignore. An unregistered
// name already fails the application's own startup, loudly and with the
// import line (auth.unknownBackendError), which is where that verdict
// belongs.
//
// So this check is confined to what the file alone proves, where it cannot
// be wrong.
func checkAuth(cfg *app.Config, _ string) doctorCheckOutcome {
	if cfg == nil {
		return doctorError("No configuration loaded", nil)
	}

	if len(cfg.AuthBackends) == 0 {
		// Not a finding. An application with no chain either has no login
		// or wires it in Go, and flagging that would train operators to
		// skim this check.
		return doctorPass("auth_backends is empty — no authentication chain is built (an application with no login, or one that wires its own)")
	}

	var errs, warns []string
	remoteOnly := true

	for _, raw := range cfg.AuthBackends {
		name := strings.ToLower(strings.TrimSpace(raw))
		p, known := knownproviders.AuthBackend(name)
		if !known || !p.Remote {
			// Either the application's own user table or a third-party
			// backend this binary knows nothing about. Both count as a
			// path that does not depend on an outside system, which is
			// the conservative reading: it avoids warning about a
			// fallback that does exist.
			remoteOnly = false
			continue
		}
		if p.RequiresConfig && len(cfg.AuthBackendConfig[name]) == 0 {
			errs = append(errs, fmt.Sprintf("%q is in auth_backends but auth.%s.* is empty — it has no address to reach its directory at, so every login through it will fail", name, name))
		}
	}

	// A chain made only of remote backends has no way in on the morning
	// the directory does not answer — and that is the morning somebody
	// needs to log in and fix it. The ordered chain exists precisely so
	// this does not have to be true.
	if remoteOnly {
		warns = append(warns, fmt.Sprintf("every backend in auth_backends (%s) depends on an outside system — when it is unreachable nobody can log in, including whoever would fix it; append a local account to the chain", strings.Join(cfg.AuthBackends, ", ")))
	}

	sort.Strings(errs)
	sort.Strings(warns)

	switch {
	case len(errs) > 0:
		return doctorError(fmt.Sprintf("%d problem(s) in the authentication chain: %s", len(errs), strings.Join(append(errs, warns...), " | ")), nil)
	case len(warns) > 0:
		return doctorWarning(fmt.Sprintf("%d thing(s) to review in the authentication chain: %s", len(warns), strings.Join(warns, " | ")))
	}

	return doctorPass(fmt.Sprintf("authentication chain: %s (consulted in this order; a backend that cannot reach its source is skipped, not treated as a rejection)", strings.Join(cfg.AuthBackends, " → ")))
}
