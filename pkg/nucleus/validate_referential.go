// Package nucleus — validate_referential.go implements the config-level part
// of ADR-010 §2 layer 4 (referential validation): cross-field rules where one
// configuration key's validity depends on another's value. Layer 3
// (validate_semantics.go) validates each field in isolation; this layer
// catches combinations that are individually well-formed but jointly invalid.
//
// It runs in the same two surfaces as layer 3 — AppBuilder.FromConfigFile (at
// load) and the package-level Run (direct-struct) — immediately after
// validateSemantics, on which it relies having run first (it does not
// re-validate field shapes). The OTHER half of layer 4, validating module
// Requires() against the configured database aliases, cannot run here: modules
// are registered on the builder (Mount), not in the config file, so that check
// runs at Run time once both the config and the module set are known (see
// validateModuleRequires below).
package nucleus

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ErrInvalidConfigReference is returned when a configuration value is
// individually valid but inconsistent with another related key (ADR-010 §2
// layer 4). The wrapped message names both keys and the rule they violate.
var ErrInvalidConfigReference = errors.New("nucleus: invalid configuration reference")

// validateReferential applies ADR-010 §2 layer-4 cross-field checks to a
// fully-merged config. Like layer 3 it treats empty/zero as "use the default"
// — a rule fires only when the governing key is explicitly set to the value
// that makes the dependent key mandatory.
func validateReferential(cfg *app.Config) error {
	if cfg == nil {
		return nil
	}

	// mail: the smtp driver needs a host and a port. smtp_port==0 and an
	// empty smtp_host pass layer 3 (they mean "unset"), but once the driver
	// is explicitly "smtp" they are a misconfiguration the mail subsystem
	// would otherwise only reject later, at first send.
	if strings.EqualFold(strings.TrimSpace(cfg.MailDriver), "smtp") {
		if strings.TrimSpace(cfg.SMTPHost) == "" {
			return fmt.Errorf("%w: smtp_host must be set when mail_driver is \"smtp\"", ErrInvalidConfigReference)
		}
		if cfg.SMTPPort <= 0 {
			return fmt.Errorf("%w: smtp_port must be greater than 0 when mail_driver is \"smtp\" (got %d)", ErrInvalidConfigReference, cfg.SMTPPort)
		}
	}

	// session cookie: SameSite=None is only honoured by browsers when the
	// cookie is also Secure; the pair SameSite=None + Secure=false makes
	// browsers drop the cookie outright, silently breaking sessions. With
	// session_cookie_secure now defaulting to true, hitting this requires a
	// deliberate double opt-out — so failing loud at load is the right call.
	if strings.EqualFold(strings.TrimSpace(cfg.SessionCookieSameSite), "none") && !cfg.SessionCookieSecure {
		return fmt.Errorf("%w: session_cookie_samesite=\"none\" requires session_cookie_secure=true (browsers drop a SameSite=None cookie that is not Secure)", ErrInvalidConfigReference)
	}

	return nil
}

// validateModuleRequires is the module half of ADR-010 §2 layer 4 (and the
// §6 boot guarantee): every alias a module declares in Requires() must be a
// configured database. It runs at Run time — modules are registered on the
// builder (Mount), not present in the loaded config — so it cannot fold into
// the config-only validateReferential. Modules are checked in sorted name
// order so the first reported error is deterministic.
func validateModuleRequires(cfg *app.Config, modules map[string]ModuleSpec) error {
	if cfg == nil || len(modules) == 0 {
		return nil
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec := modules[name]
		for _, alias := range spec.Requires() {
			if _, ok := cfg.Databases[alias]; !ok {
				return fmt.Errorf("%w: module %q requires database %q which is not configured", ErrInvalidConfigReference, spec.Name(), alias)
			}
		}
	}
	return nil
}
