// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// config_validate_layers.go implements ADR-010 §2 layers 3 and 4 on the
// application Config: field-semantic validation (ranges, enums, parseable
// durations) and config-level referential validation (cross-field rules).
// Layers 1 (syntactic) and 2 (schema / unknown-fields) ship in config.go
// and config_validate.go.
//
// These layers were born in pkg/nucleus and ran only on its two surfaces
// (FromConfigFile and the direct-struct Run) — the CLI's LoadConfig skipped
// them, so `log_level: verbose` failed `go run .` but sailed through every
// `nucleus <cmd>`: the "same file, two verdicts" class the DX audit named
// (DX-13 closed it for unknown keys; this closes layers 3–4). They live
// here so every config consumer — builder, direct struct, and all 38 CLI
// commands — applies the same verdict; pkg/nucleus re-exports the error
// sentinels for compatibility.
package app

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ErrInvalidConfigValue is returned when a configuration value is well-typed
// but semantically invalid — out of range, not a recognised enum member, or a
// negative duration (ADR-010 §2 layer 3). The wrapped message names the
// offending key, its value, and the accepted set or range.
var ErrInvalidConfigValue = errors.New("nucleus: invalid configuration value")

// ErrInvalidConfigReference is returned when a configuration value is
// individually valid but inconsistent with another related key (ADR-010 §2
// layer 4). The wrapped message names both keys and the rule they violate.
var ErrInvalidConfigReference = errors.New("nucleus: invalid configuration reference")

const validatePortMax = 65535

// ValidateSemantics applies ADR-010 §2 layer-3 checks to a fully-merged
// config. Empty strings and zero numerics are accepted: they denote
// "use the framework default" (defaults are applied by app.New), so a
// zero-value or partial config passes — only an explicitly wrong value fails.
func ValidateSemantics(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	// Enums — compared case-insensitively against the exact sets the
	// consumers switch on. Empty is allowed (resolves to the default).
	if err := validateConfigEnum("session_store", cfg.SessionStore, "memory", "sql", "redis"); err != nil {
		return err
	}
	if err := validateConfigEnum("log_level", cfg.LogLevel, "debug", "info", "warn", "warning", "error"); err != nil {
		return err
	}
	if err := validateConfigEnum("log_format", cfg.LogFormat, "json", "text"); err != nil {
		return err
	}
	// auth_backends: a name that is not registered can only fail at the
	// first login attempt, which is the worst possible moment to discover
	// a typo in an authentication list. Rejecting at load turns it into a
	// boot error that names the registered backends.
	//
	// A duplicate is rejected too: the list is ORDERED, so repeating a
	// name is a mistake rather than a harmless no-op — somebody meant to
	// write a different one.
	seenBackend := map[string]struct{}{}
	for i, name := range cfg.AuthBackends {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return fmt.Errorf("%w: auth_backends[%d] is empty", ErrInvalidConfigValue, i)
		}
		if _, dup := seenBackend[normalized]; dup {
			return fmt.Errorf("%w: auth_backends[%d] repeats %q — the list is an ordered chain, so a repeat is a mistake, not a no-op", ErrInvalidConfigValue, i, name)
		}
		seenBackend[normalized] = struct{}{}
	}

	// trusted_proxies: an entry the matcher cannot parse is DISCARDED, and
	// with one entry and a typo the list comes out empty — trusts() then
	// answers false forever and the RealIP middleware never rewrites
	// anything. It fails in the safe direction, which is exactly why it
	// went unnoticed: behind a real load balancer the rate limiter puts all
	// of the internet in one bucket and the audit trail names the balancer
	// as the author of every request, while `check`, `doctor` and `config
	// print` all reported the configuration as written (QCD-FW-17).
	//
	// Parsing here mirrors router.newTrustedProxyMatcher exactly: a CIDR,
	// or a bare IP that matches only itself. Rejecting at load is what
	// makes the silence impossible.
	for i, entry := range cfg.TrustedProxies {
		raw := strings.TrimSpace(entry)
		if raw == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(raw); err == nil {
			continue
		}
		if ip := net.ParseIP(raw); ip != nil {
			continue
		}
		return fmt.Errorf("%w: trusted_proxies[%d] %q is neither an IP address nor a CIDR range — the proxy matcher would discard it, leaving the entry with no effect and X-Forwarded-For unread", ErrInvalidConfigValue, i, entry)
	}

	if err := validateConfigEnum("session_cookie_samesite", cfg.SessionCookieSameSite, "strict", "lax", "none"); err != nil {
		return err
	}
	if err := validateConfigEnum("jobs_provider", cfg.JobsProvider, "memory", "asynq"); err != nil {
		return err
	}
	// Referential within the jobs keys: the asynq provider cannot run
	// without Redis, and the failure should name the config keys here
	// rather than surface later as a provider construction error.
	if strings.EqualFold(strings.TrimSpace(cfg.JobsProvider), "asynq") && strings.TrimSpace(cfg.JobsRedisURL) == "" {
		return fmt.Errorf("%w: jobs_provider \"asynq\" requires jobs_redis_url", ErrInvalidConfigValue)
	}
	if cfg.JobsConcurrency < 0 {
		return fmt.Errorf("%w: jobs_concurrency %d must not be negative (0 uses the provider default)", ErrInvalidConfigValue, cfg.JobsConcurrency)
	}

	// Ranges. Port 0 is permitted: for `port` it means "let the OS pick a
	// free port" (the test suite and ephemeral servers rely on it); for
	// `smtp_port` it means "unset" (the mail subsystem rejects 0 loudly only
	// when the smtp driver is actually selected — a referential check left to
	// that layer).
	if err := validateConfigPort("port", cfg.Port); err != nil {
		return err
	}
	if err := validateConfigPort("smtp_port", cfg.SMTPPort); err != nil {
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

// ValidateReferential applies ADR-010 §2 layer-4 cross-field checks to a
// fully-merged config. Like layer 3 it treats empty/zero as "use the default"
// — a rule fires only when the governing key is explicitly set to the value
// that makes the dependent key mandatory. It relies on ValidateSemantics
// having run first (it does not re-validate field shapes). The module half
// of layer 4 — Requires() against configured database aliases — lives in
// pkg/nucleus, where the module set exists.
func ValidateReferential(cfg *Config) error {
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

	// session cookie prefixes: "__Host-" and "__Secure-" are enforced by the
	// BROWSER, which silently drops a cookie whose attributes contradict its
	// name. The pairing is therefore part of what the configuration means,
	// not plumbing of the session subsystem — and the session builder's own
	// guard (kept, as defence in depth) fires at boot, long after
	// `nucleus config validate` has already called the file good.
	if err := validateCookiePrefix(cfg); err != nil {
		return err
	}

	return nil
}

// validateCookiePrefix applies the RFC 6265bis cookie-name prefix rules to
// the session cookie. Both prefixes are opt-in: a name that carries neither
// is unconstrained.
func validateCookiePrefix(cfg *Config) error {
	name := strings.TrimSpace(cfg.SessionCookieName)

	switch {
	case strings.HasPrefix(name, "__Host-"):
		if !cfg.SessionCookieSecure {
			return fmt.Errorf("%w: session_cookie_name %q uses the __Host- prefix, which requires session_cookie_secure=true", ErrInvalidConfigReference, name)
		}
		if strings.TrimSpace(cfg.SessionCookieDomain) != "" {
			return fmt.Errorf("%w: session_cookie_name %q uses the __Host- prefix, which forbids setting session_cookie_domain (got %q)", ErrInvalidConfigReference, name, cfg.SessionCookieDomain)
		}
		if path := strings.TrimSpace(cfg.SessionCookiePath); path != "/" {
			return fmt.Errorf("%w: session_cookie_name %q uses the __Host- prefix, which requires session_cookie_path=/ (got %q)", ErrInvalidConfigReference, name, cfg.SessionCookiePath)
		}
	case strings.HasPrefix(name, "__Secure-"):
		if !cfg.SessionCookieSecure {
			return fmt.Errorf("%w: session_cookie_name %q uses the __Secure- prefix, which requires session_cookie_secure=true", ErrInvalidConfigReference, name)
		}
	}

	return nil
}

// validateConfigEnum reports an error unless value (trimmed, lower-cased) is
// empty or one of allowed. Matching mirrors the consumers, which all
// strings.ToLower(strings.TrimSpace(...)) before switching.
func validateConfigEnum(key, value string, allowed ...string) error {
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

// validateConfigPort accepts 0 through 65535. 0's meaning (OS-assigned vs
// unset) is key-specific and documented at the call site; the range is
// uniform.
func validateConfigPort(key string, port int) error {
	if port < 0 || port > validatePortMax {
		return fmt.Errorf("%w: %s %d must be in range 0-%d", ErrInvalidConfigValue, key, port, validatePortMax)
	}
	return nil
}
