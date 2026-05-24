// Package nucleus — validate_semantics.go implements ADR-010 §2 layer 3
// (field-semantic validation: ranges, enums, parseable durations). Layers 1
// (syntactic) and 2 (schema / unknown-fields) ship in config.go; this layer
// rejects values that parse and bind cleanly but are out of range, not a
// recognised enum member, or a negative duration — failures that several
// subsystems otherwise handle silently or late (e.g. an unknown session_store
// is rejected only when the session manager is built; an unrecognised
// session_cookie_samesite is silently downgraded to "lax").
//
// It runs in both surfaces ADR-010 §2 names: AppBuilder.FromConfigFile (at
// load, so the error surfaces at Build/Err/Start) and the package-level Run
// (so the direct-struct surface is validated before app.New). The CLI-flags
// and programmatic-override layers of §4 are not validated here.
package nucleus

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ErrInvalidConfigValue is returned when a configuration value is well-typed
// but semantically invalid — out of range, not a recognised enum member, or a
// negative duration (ADR-010 §2 layer 3). The wrapped message names the
// offending key, its value, and the accepted set or range.
var ErrInvalidConfigValue = errors.New("nucleus: invalid configuration value")

const portMax = 65535

// validateSemantics applies ADR-010 §2 layer-3 checks to a fully-merged
// config. Empty strings and zero numerics are accepted: they denote
// "use the framework default" (defaults are applied by app.New), so a
// zero-value or partial config passes — only an explicitly wrong value fails.
func validateSemantics(cfg *app.Config) error {
	if cfg == nil {
		return nil
	}

	// Enums — compared case-insensitively against the exact sets the
	// consumers switch on. Empty is allowed (resolves to the default).
	if err := validateEnum("session_store", cfg.SessionStore, "memory", "sql", "redis"); err != nil {
		return err
	}
	if err := validateEnum("log_level", cfg.LogLevel, "debug", "info", "warn", "warning", "error"); err != nil {
		return err
	}
	if err := validateEnum("log_format", cfg.LogFormat, "json", "text"); err != nil {
		return err
	}
	if err := validateEnum("session_cookie_samesite", cfg.SessionCookieSameSite, "strict", "lax", "none"); err != nil {
		return err
	}

	// Ranges. Port 0 is permitted: for `port` it means "let the OS pick a
	// free port" (the test suite and ephemeral servers rely on it); for
	// `smtp_port` it means "unset" (the mail subsystem rejects 0 loudly only
	// when the smtp driver is actually selected — a referential check left to
	// that layer).
	if err := validatePort("port", cfg.Port); err != nil {
		return err
	}
	if err := validatePort("smtp_port", cfg.SMTPPort); err != nil {
		return err
	}
	if cfg.RateLimitRequests < 0 {
		return fmt.Errorf("%w: rate_limit_requests %d must not be negative (0 disables rate limiting)", ErrInvalidConfigValue, cfg.RateLimitRequests)
	}
	if cfg.RateLimitBurst < 0 {
		return fmt.Errorf("%w: rate_limit_burst %d must not be negative", ErrInvalidConfigValue, cfg.RateLimitBurst)
	}

	// Durations — a negative duration is always a misconfiguration; zero is
	// allowed (typically "no timeout" / "use default").
	for _, d := range []struct {
		key string
		val time.Duration
	}{
		{"read_timeout", cfg.ReadTimeout},
		{"write_timeout", cfg.WriteTimeout},
		{"idle_timeout", cfg.IdleTimeout},
		{"jwt_expiry", cfg.JWTExpiry},
		{"session_lifetime", cfg.SessionLifetime},
		{"session_idle_timeout", cfg.SessionIdleTimeout},
		{"rate_limit_window", cfg.RateLimitWindow},
	} {
		if d.val < 0 {
			return fmt.Errorf("%w: %s %v must not be negative", ErrInvalidConfigValue, d.key, d.val)
		}
	}

	return nil
}

// validateEnum reports an error unless value (trimmed, lower-cased) is empty or
// one of allowed. Matching mirrors the consumers, which all
// strings.ToLower(strings.TrimSpace(...)) before switching.
func validateEnum(key, value string, allowed ...string) error {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return nil
	}
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("%w: %s %q is not one of [%s]", ErrInvalidConfigValue, key, value, strings.Join(allowed, " "))
}

// validatePort accepts 0 through 65535. 0's meaning (OS-assigned vs unset) is
// key-specific and documented at the call site; the range is uniform.
func validatePort(key string, port int) error {
	if port < 0 || port > portMax {
		return fmt.Errorf("%w: %s %d must be in range 0-%d", ErrInvalidConfigValue, key, port, portMax)
	}
	return nil
}
