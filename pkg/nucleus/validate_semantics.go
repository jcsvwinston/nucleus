// Package nucleus — validate_semantics.go. The layer-3 implementation moved
// to pkg/app (config_validate_layers.go) so the CLI's LoadConfig applies the
// same verdict as the builder and the direct-struct Run — closing the
// "same file, two verdicts" gap for layers 3–4 (the DX audit's class; DX-13
// closed it for unknown keys). This file keeps the public sentinel where
// consumers already import it.
package nucleus

import (
	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ErrInvalidConfigValue is returned when a configuration value is well-typed
// but semantically invalid — out of range, not a recognised enum member, or a
// negative duration (ADR-010 §2 layer 3). Alias of app.ErrInvalidConfigValue
// (the implementation moved to pkg/app so every config consumer shares it);
// errors.Is matches through either name.
var ErrInvalidConfigValue = app.ErrInvalidConfigValue

// validateSemantics applies ADR-010 §2 layer-3 checks. Thin forwarder kept so
// the boot-sequence call sites read at the layer vocabulary of ADR-010.
func validateSemantics(cfg *app.Config) error {
	return app.ValidateSemantics(cfg)
}
