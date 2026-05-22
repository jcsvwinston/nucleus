package observe

import (
	"log/slog"
	"sort"
	"strings"
)

// RedactionPlaceholder is the value substituted for a redacted log
// attribute. It is deliberately bracketed and uppercase so a redacted
// field is unmistakable in log output and easy to grep for.
const RedactionPlaceholder = "[REDACTED]"

// defaultRedactedKeys is the built-in set of log attribute keys whose
// values are redacted by NewLogger. Matching is case-insensitive and
// exact — a key is redacted iff its lower-cased form is in this set.
//
// Exact matching is deliberate. Substring or suffix matching (e.g.
// "*_token", "*_key") would catch more secrets but also redact benign
// fields — `page_token`, `cache_key`, `partition_key` — and silently
// hide debugging information. The curated exact-match list is
// predictable; operators extend it via RedactionConfig.ExtraKeys for
// app-specific fields. See ADR-007.
var defaultRedactedKeys = map[string]struct{}{
	// HTTP auth / headers
	"authorization":       {},
	"proxy-authorization": {},
	"www-authenticate":    {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	// API keys and generic secrets
	"api_key":        {},
	"apikey":         {},
	"password":       {},
	"passwd":         {},
	"pwd":            {},
	"secret":         {},
	"client_secret":  {},
	"credentials":    {},
	"encryption_key": {},
	// Tokens
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"session_token": {},
	"bearer_token":  {},
	"auth_token":    {},
	"oauth_token":   {},
	"github_token":  {},
	"slack_token":   {},
	"csrf_token":    {},
	"xsrf-token":    {},
	// Key material
	"private_key":       {},
	"private_key_pem":   {},
	"rsa_private_key":   {},
	"ecdsa_private_key": {},
	// Connection strings / DSNs — these embed credentials inline
	// (postgres://user:pass@host, redis://:pass@host).
	"database_url":      {},
	"db_url":            {},
	"dsn":               {},
	"connection_string": {},
	"redis_url":         {},
	"redis_password":    {},
	"smtp_pass":         {},
	"smtp_password":     {},
	// Cloud credentials
	"aws_secret_access_key": {},
	"aws_session_token":     {},
	"secret_access_key":     {}, // storage.s3.secret_access_key (koanf leaf)
	"account_key":           {}, // storage.azure.account_key (koanf leaf)
	// Nucleus framework config keys (app.Config secret-bearing fields).
	// Their leaf segment is a compound name that the short atomic keys
	// above do not match, so they are listed explicitly. Redacted in both
	// log attributes and `nucleus config print --effective` output.
	"jwt_secret":               {},
	"admin_bootstrap_password": {},
	"admin_cluster_token":      {},
	"session_redis_url":        {},
	"admin_cluster_redis_url":  {},
}

// slogBuiltinKeys are the attribute keys slog itself emits for every
// record (time, level, msg, source). They are delivered to ReplaceAttr
// with an empty `groups` slice. The redactor never touches them even if
// an operator accidentally lists one in RedactionConfig.ExtraKeys —
// redacting `time` or `msg` would silently break log pipelines (Loki,
// Splunk, …) that key off those fields.
var slogBuiltinKeys = map[string]struct{}{
	slog.TimeKey:    {},
	slog.LevelKey:   {},
	slog.MessageKey: {},
	slog.SourceKey:  {},
}

// DefaultRedactedKeys returns a sorted copy of the built-in set of log
// attribute keys whose values NewLogger redacts. It is exposed so
// operators can audit exactly what is redacted by default and decide
// what app-specific keys to add via RedactionConfig.ExtraKeys.
func DefaultRedactedKeys() []string {
	out := make([]string, 0, len(defaultRedactedKeys))
	for k := range defaultRedactedKeys {
		out = append(out, k)
	}
	// Map iteration order is non-deterministic; sort for a stable,
	// auditable result.
	sort.Strings(out)
	return out
}

// RedactionConfig customises secret redaction for
// NewLoggerWithRedaction. The zero value is the secure default:
// redaction enabled, built-in key set, standard placeholder.
type RedactionConfig struct {
	// Disabled turns redaction off entirely. Redaction is ON by default
	// (the security-by-default principle, SPEC.md §2). There is no
	// config-file switch to disable it — turning it off requires this
	// explicit code-level opt-out so the decision surfaces in code
	// review, the same discipline ADR-004 applies to WithOpenAuthz().
	Disabled bool

	// ExtraKeys are additional attribute keys to redact beyond
	// DefaultRedactedKeys. Case-insensitive. Use this for app-specific
	// sensitive fields (e.g. "ssn", "card_number"). slog's own built-in
	// keys (time, level, msg, source) are always ignored here — listing
	// one has no effect, so a stray entry cannot silence timestamps.
	ExtraKeys []string

	// Placeholder overrides the redacted-value string. Empty uses
	// RedactionPlaceholder.
	Placeholder string
}

// newRedactor builds the slog ReplaceAttr function for the given config.
// It returns nil when redaction is disabled, so the caller can leave
// HandlerOptions.ReplaceAttr unset (zero overhead) in that case.
func newRedactor(cfg RedactionConfig) func([]string, slog.Attr) slog.Attr {
	if cfg.Disabled {
		return nil
	}

	// Build the effective key set: defaults plus any extras, all
	// lower-cased. The defaults map is never mutated.
	keys := make(map[string]struct{}, len(defaultRedactedKeys)+len(cfg.ExtraKeys))
	for k := range defaultRedactedKeys {
		keys[k] = struct{}{}
	}
	for _, k := range cfg.ExtraKeys {
		k = strings.ToLower(strings.TrimSpace(k))
		if k != "" {
			keys[k] = struct{}{}
		}
	}

	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = RedactionPlaceholder
	}

	return func(groups []string, a slog.Attr) slog.Attr {
		// Never touch slog's own built-in attrs (time/level/msg/source).
		// They arrive with an empty `groups` slice; guarding here means
		// an operator who accidentally puts "time" or "msg" in ExtraKeys
		// cannot silence their timestamps or messages.
		if len(groups) == 0 {
			if _, builtin := slogBuiltinKeys[a.Key]; builtin {
				return a
			}
		}

		// Match on the attribute key regardless of group nesting — a
		// `password` attr is a secret whether it is top-level or inside
		// a `request` group. strings.ToLower has a fast path that returns
		// the input unchanged (no allocation) when it is already
		// lower-case ASCII, which is the common case for attr keys
		// written as Go string literals.
		if _, ok := keys[strings.ToLower(a.Key)]; ok {
			// Replace the value, keep the key. The original value's type
			// is irrelevant — whatever it was, it is now a fixed string.
			return slog.String(a.Key, placeholder)
		}
		return a
	}
}
