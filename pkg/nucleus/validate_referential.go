// Package nucleus — validate_referential.go. The config-only layer-4
// implementation moved to pkg/app (config_validate_layers.go) next to layer
// 3, for the same reason: every config consumer — builder, direct struct,
// and the CLI's LoadConfig — must reach the same verdict. What stays here is
// the public sentinel (where consumers already import it) and the MODULE
// half of layer 4, which cannot move: modules are registered on the builder
// (Mount), not present in the loaded config.
package nucleus

import (
	"fmt"
	"sort"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ErrInvalidConfigReference is returned when a configuration value is
// individually valid but inconsistent with another related key (ADR-010 §2
// layer 4). Alias of app.ErrInvalidConfigReference; errors.Is matches
// through either name.
var ErrInvalidConfigReference = app.ErrInvalidConfigReference

// validateReferential applies the config-only ADR-010 §2 layer-4 checks.
// Thin forwarder kept so the boot-sequence call sites read at the layer
// vocabulary of ADR-010.
func validateReferential(cfg *app.Config) error {
	return app.ValidateReferential(cfg)
}

// validateModuleRequires is the module half of ADR-010 §2 layer 4 (and the
// §6 boot guarantee): every alias a module declares in Requires() must be a
// configured database. It runs at Run time — modules are registered on the
// builder (Mount), not present in the loaded config — so it cannot fold into
// the config-only ValidateReferential. Modules are checked in sorted name
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
