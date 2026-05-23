// Package nucleus — config.go implements the configuration loader
// surfaced by `AppBuilder.FromConfigFile`. ADR-010 §2 names this as
// Phase 2 work. Phase 2a (PR #73) shipped the single-file YAML loader
// with the 1 MiB size cap, schema strict-unknown-fields validation,
// and did-you-mean hints. Phase 2b (#74) layered on top:
//
//   - TOML and JSON parsers (extension-based dispatch).
//   - Multi-file merge with last-file-wins semantics, deep-merge for
//     maps, and replace-by-default for scalars and lists.
//   - `_append` / `_remove` suffix operators that survive the parser
//     round-trip in all three formats and provide additive/subtractive
//     semantics for list/map collections (ADR-010 §3).
//   - `null` reverts the key to its struct default — except for the
//     non-nullable security keys named in ADR-010 §14, where `null`
//     is a boot error rather than a silent revert-to-default.
//   - Mixed-format file lists (one .yaml + one .toml, for example)
//     emit a startup warning by default and are rejected outright when
//     `AppBuilder.WithConfigStrict(true)` is in force.
//
// Phase 2c (this commit) layers the ADR-010 §15 strict-mode startup
// guard:
//
//   - `AppBuilder.WithUnknownFields(UnknownFieldsStrict | UnknownFieldsWarn)`
//     toggles strict schema validation per builder. Warn mode emits a
//     `WARN`-level slog event listing the offending keys and proceeds
//     with the load; strict mode (the default) keeps the Phase 2a
//     reject-with-`ErrUnknownConfigKeys` behaviour.
//   - `NUCLEUS_ENV=production` (case-insensitive, whitespace-trimmed;
//     constant `EnvProduction`) is the operator escape hatch: when set,
//     the loader forces the mode back to strict regardless of code-level
//     configuration and emits a `WARN` recording the override.
//   - Two startup `WARN`s for visibility: one when warn mode is active
//     outside production ("do not deploy to production"), one when the
//     production override fires.
//
// The package-level `Run(App)` and the direct-struct surface never
// traverse this loader — only the builder-chain `FromConfigFile` does.
//
// Phase 3 shipped the effective-config tooling (3a: `LoadEffective` +
// `config print --effective`), the auth-gated `/_/config` endpoint (3b), and
// the env-layer + `file:line` provenance (3.1). The `NUCLEUS_`-prefixed env
// layer is applied here (see loadMerged / applyEnvLayer), so the FromConfigFile
// path honours the ADR-010 §4 precedence `defaults < files < env`.
//
// What's still deferred:
//
//   - Layer 3 range/enum semantic validation (out of the 4-phase
//     slicing; tracked as a follow-up).
//   - The CLI-flags and programmatic-override layers of ADR-010 §4.
package nucleus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	jsonparser "github.com/knadh/koanf/parsers/json"
	tomlparser "github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	yamlnode "go.yaml.in/yaml/v3"
)

// MaxConfigFileBytes is the per-file size cap enforced by
// FromConfigFile before invoking any format parser. The cap is the
// ADR-010 §17 compliance item — it eliminates the parser-DoS class
// (anchor expansion / deep nesting) that `gopkg.in/yaml.v3` is not
// hardened against by itself, and applies uniformly to TOML and JSON
// for consistency. 1 MiB is generous for application configuration in
// practice while still small enough to make a pathological file fail
// loud rather than wedge the process.
const MaxConfigFileBytes = 1 << 20 // 1 MiB

// ErrConfigFileTooLarge is returned when a configuration file exceeds
// MaxConfigFileBytes. Callers can errors.Is against this sentinel to
// distinguish a configuration-management problem (file is genuinely
// too big — split it) from a parser-side problem (bad content).
var ErrConfigFileTooLarge = errors.New("nucleus: configuration file exceeds the per-file size cap")

// ErrUnsupportedConfigFormat is returned when FromConfigFile is asked
// to parse a file whose extension is not recognised. Phase 2a
// supported only `.yaml` / `.yml`; Phase 2b adds `.toml` and `.json`.
// Anything else (`.ini`, `.xml`, …) surfaces this sentinel.
var ErrUnsupportedConfigFormat = errors.New("nucleus: unsupported configuration file format")

// ErrUnknownConfigKeys is returned when strict schema validation
// (the default for FromConfigFile) finds keys in the loaded file
// that do not map to any field on `app.Config` or its nested
// structs. The error's Error() reproduces the offending keys with
// "did you mean …?" hints when a close match exists.
var ErrUnknownConfigKeys = errors.New("nucleus: unknown configuration key(s)")

// ErrSecurityKeyNotNullable is returned when a configuration file
// sets one of the non-nullable security keys to `null` / `~`. ADR-010
// §14 lists the keys whose null-revert would be a silent security
// degradation (e.g. `cors_origins: null` reverting to
// `corsAllowAll: true`). On these keys, null is a boot error rather
// than a revert-to-default.
var ErrSecurityKeyNotNullable = errors.New("nucleus: security key may not be null")

// ErrMixedConfigFormats is returned by FromConfigFile when the
// configured paths use a mix of formats (e.g. one `.yaml` plus one
// `.toml`) AND `AppBuilder.WithConfigStrict(true)` is in force. With
// strict mode off (the default), a mixed-format file list emits a
// `WARN`-level slog event but proceeds with the merge.
var ErrMixedConfigFormats = errors.New("nucleus: configuration files mix incompatible formats")

// ErrInvalidUnknownFieldsMode is returned by
// `AppBuilder.WithUnknownFields` when the supplied mode is neither
// `UnknownFieldsStrict` nor `UnknownFieldsWarn`. The error is
// deferred onto the builder so the misuse surfaces at `Build` /
// `Start` / `Serve` time alongside any other deferred chain error.
var ErrInvalidUnknownFieldsMode = errors.New("nucleus: WithUnknownFields requires mode \"strict\" or \"warn\"")

// UnknownFieldsStrict and UnknownFieldsWarn are the two values
// accepted by `AppBuilder.WithUnknownFields`. ADR-010 §15 specifies
// the strings; the framework exports them as constants so callers
// can avoid string-literal typos at the call site (`nucleus.UnknownFieldsWarn`
// reads better than `"warn"` in IDEs and survives refactors). New
// modes are not anticipated — if one ever lands it joins the union
// here and the validation in `WithUnknownFields` is extended.
const (
	UnknownFieldsStrict = "strict"
	UnknownFieldsWarn   = "warn"
)

// EnvProduction is the value of the `NUCLEUS_ENV` environment
// variable that the framework treats as "production-strict":
// regardless of `WithUnknownFields("warn")`, the loader rejects
// unknown configuration keys when `NUCLEUS_ENV=production` is set.
// ADR-010 §15 specifies this as the operator escape hatch against a
// developer who accidentally left warn mode in a production build.
const EnvProduction = "production"

// nucleusEnv is the indirection the loader uses to read
// `NUCLEUS_ENV`. Tests reach this hook via `t.Setenv("NUCLEUS_ENV",
// ...)` against the underlying process environment — the hook itself
// is not reassigned. Centralising the read keeps a single audit
// surface for any future override (build tag, secret manager) that
// needs to feed the loader's production-strict decision.
var nucleusEnv = func() string { return os.Getenv("NUCLEUS_ENV") }

// isProductionEnv reports whether the calling process is running
// under `NUCLEUS_ENV=production`. `strings.TrimSpace` strips all
// leading and trailing Unicode whitespace (spaces, tabs, newlines)
// so a stray newline at the end of an env-var assignment cannot
// silently downgrade the strict override; the equality compare is
// case-insensitive so `Production` / `PRODUCTION` qualify alongside
// `production`. Any value other than the trimmed-and-case-folded
// `production` (constant `EnvProduction`) — including `prod`,
// `prd`, `live`, `staging` — does not trigger the override.
func isProductionEnv() bool {
	return strings.EqualFold(strings.TrimSpace(nucleusEnv()), EnvProduction)
}

// configFormat identifies the parser dispatched for a given file
// extension. The set is intentionally finite — extending it requires
// a parser, a CHANGELOG entry, and (for any format whose parser
// historically has CVEs against it) the MaxConfigFileBytes guard.
type configFormat int

const (
	formatUnknown configFormat = iota
	formatYAML
	formatTOML
	formatJSON
)

func (f configFormat) String() string {
	switch f {
	case formatYAML:
		return "yaml"
	case formatTOML:
		return "toml"
	case formatJSON:
		return "json"
	default:
		return "unknown"
	}
}

// detectFormat returns the configFormat for a given path's extension.
// `.yml` is folded into `.yaml`. An unrecognised extension yields
// formatUnknown, which the caller surfaces as ErrUnsupportedConfigFormat.
func detectFormat(path string) configFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return formatYAML
	case ".toml":
		return formatTOML
	case ".json":
		return formatJSON
	default:
		return formatUnknown
	}
}

// parserFor returns the koanf.Parser instance for a given format. The
// returned parser is reused per call — koanf parsers are stateless.
func parserFor(f configFormat) (koanf.Parser, bool) {
	switch f {
	case formatYAML:
		return yaml.Parser(), true
	case formatTOML:
		return tomlparser.Parser(), true
	case formatJSON:
		return jsonparser.Parser(), true
	default:
		return nil, false
	}
}

// operatorSuffixAppend / operatorSuffixRemove are the merge-time
// suffix operators per ADR-010 §3. They are preferred over `<key>+` /
// `<key>-` because YAML / TOML / JSON parsers all treat `+` and `-`
// as part of the key name with no special semantics, while the
// underscore-suffixed form round-trips cleanly through every parser
// the loader supports today.
const (
	operatorSuffixAppend = "_append"
	operatorSuffixRemove = "_remove"
)

// defaultNonNullableSecurityKeys lists the configuration keys that
// the merge engine refuses to set to null. ADR-010 §3 and §14: a
// null on these keys is a boot error, not a silent revert-to-default
// — reverting would be a silent security degradation (e.g.
// `jwt_secret: null` leaving the framework with no token signing key
// when `jwt_keys[]` is also empty).
//
// Today the only key in the active set is `jwt_secret`. ADR-010 §14
// also names four forward-compat slots that do not yet exist in
// `app.Config` (`cors.origins`, `auth.providers`, `authz.policy_path`,
// `session.secret`); they are deliberately NOT in the active set
// because their final koanf tag is not pinned yet — `app.Config`'s
// existing fields use the flat-underscore convention (`jwt_secret`,
// `session_cookie_secure`) rather than the dotted notation ADR-010
// prefers in prose. Adding the dotted form here would create a
// dormant guard that never fires once the subsystems land under
// flat-underscore tags. The right place to add each forward-compat
// key is the PR that wires its subsystem into `app.Config`, using
// the exact `koanf:"..."` tag that subsystem ships with.
var defaultNonNullableSecurityKeys = []string{
	"jwt_secret",
}

// isNonNullableSecurityKey reports whether the given flat-dotted
// configuration key is in the non-nullable security set.
func isNonNullableSecurityKey(key string) bool {
	for _, k := range defaultNonNullableSecurityKeys {
		if k == key {
			return true
		}
	}
	return false
}

// sourceKindDefault is the ConfigSource.Kind for a value that resolves to
// the framework struct default (no config file set it, or a file reverted
// it with `null`). File-sourced values use the format string ("yaml",
// "toml", "json") from configFormat.String().
const sourceKindDefault = "default"

// sourceKindEnv is the ConfigSource.Kind for a value supplied by a
// `NUCLEUS_`-prefixed environment variable (ADR-010 §4 env layer, wired into
// the FromConfigFile path in Phase 3.1). For env-sourced keys, ConfigSource.Path
// holds the originating variable name (e.g. `NUCLEUS_PORT`) so the effective
// view can render `[env:NUCLEUS_PORT]`.
const sourceKindEnv = "env"

// envVarPrefix is the environment-variable prefix the loader honours, matching
// `app.LoadConfig`. `NUCLEUS_PORT` → `port`; nested keys use a double
// underscore: `NUCLEUS_DATABASES__ANALYTICS__URL` → `databases.analytics.url`.
const envVarPrefix = "NUCLEUS_"

// ConfigSource identifies where an effective configuration value came from
// (ADR-010 §5 / Phase 3, compliance #6). Kind is "default" for struct
// defaults, one of "yaml"/"toml"/"json" for a file, or "env" for a
// `NUCLEUS_`-prefixed environment override (Phase 3.1). Path is the file path
// for file kinds, the originating variable name for "env", and empty for
// defaults. The CLI-flags and programmatic-override layers of ADR-010 §4 are
// not applied in the FromConfigFile path, so they never appear as a Kind here.
type ConfigSource struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
	// Line is the 1-based source line a file-sourced key was defined on
	// (Phase 3.1). Populated for YAML files only — TOML positions are
	// available only via go-toml's explicitly-unstable API, and JSON has no
	// standard line API, so both report kind+path with Line == 0. Omitted
	// (zero) for the "default", "env", and "runtime" kinds.
	Line int `json:"line,omitempty"`
}

// EffectiveValue is one resolved configuration key with its origin. Value
// holds observe.RedactionPlaceholder (and Redacted is true) when the key is
// sensitive per the canonical redaction list.
type EffectiveValue struct {
	Key      string       `json:"key"`
	Value    any          `json:"value"`
	Source   ConfigSource `json:"source"`
	Redacted bool         `json:"redacted,omitempty"`
}

// EffectiveConfig is the fully-merged configuration with per-key provenance,
// sorted by Key. It backs both `nucleus config print --effective` and the
// /_/config endpoint (ADR-010 Phase 3).
type EffectiveConfig struct {
	Values []EffectiveValue `json:"values"`
}

// LoadEffective merges the given config files exactly as FromConfigFile
// would (struct defaults < file[0] < … < file[N-1]) and returns the
// effective configuration with per-key provenance and the canonical
// redaction applied (observe.DefaultRedactedKeys()). It is the shared entry
// point for `nucleus config print --effective` and the /_/config endpoint.
//
// Sensitive values are redacted using only the canonical redaction list;
// pass extraKeys to extend it via the same observe.RedactionConfig.ExtraKeys
// mechanism the logger uses — there is no second redaction surface.
func LoadEffective(paths []string, extraKeys ...string) (EffectiveConfig, error) {
	return loadEffective(paths, configLoadOptions{}, extraKeys)
}

func loadEffective(paths []string, opts configLoadOptions, extraKeys []string) (EffectiveConfig, error) {
	k, sources, err := loadMerged(paths, opts)
	if err != nil {
		return EffectiveConfig{}, err
	}

	all := k.All()
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	redactSet := redactionSet(extraKeys)
	values := make([]EffectiveValue, 0, len(keys))
	for _, key := range keys {
		src, ok := sources[key]
		if !ok {
			// A key present in the merged result but absent from the
			// source map can only be a default-derived key the seeding
			// missed; attribute it to the default.
			src = ConfigSource{Kind: sourceKindDefault}
		}
		val := all[key]
		redacted := false
		if shouldRedactKey(key, redactSet) {
			val = observe.RedactionPlaceholder
			redacted = true
		}
		values = append(values, EffectiveValue{Key: key, Value: val, Source: src, Redacted: redacted})
	}
	return EffectiveConfig{Values: values}, nil
}

// redactionSet builds the lookup set of sensitive leaf-key names from the
// canonical observe.DefaultRedactedKeys() plus any operator-supplied
// extras, all lower-cased for case-insensitive matching.
func redactionSet(extraKeys []string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, k := range observe.DefaultRedactedKeys() {
		set[strings.ToLower(k)] = struct{}{}
	}
	for _, k := range extraKeys {
		set[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}
	return set
}

// shouldRedactKey reports whether a dotted effective-config key is
// sensitive. It matches the leaf segment against the canonical set, then
// applies a structural rule for nested datasource URLs: a `databases.<alias>.url`
// (or `.dsn`) holds a connection string that embeds credentials, so it is
// treated as the canonical `database_url` / `dsn` key. The rule introduces
// no new key names — it maps a structural pattern onto entries that are
// already in the canonical list, so the "one redaction surface" invariant
// (ADR-010 §5 / compliance #13) holds.
func shouldRedactKey(dottedKey string, set map[string]struct{}) bool {
	leaf := dottedKey
	if i := strings.LastIndex(dottedKey, "."); i >= 0 {
		leaf = dottedKey[i+1:]
	}
	if _, ok := set[strings.ToLower(leaf)]; ok {
		return true
	}
	if strings.HasPrefix(dottedKey, "databases.") {
		switch strings.ToLower(leaf) {
		case "url":
			_, ok := set["database_url"]
			if !ok {
				_, ok = set["db_url"]
			}
			return ok
		case "dsn":
			_, ok := set["dsn"]
			return ok
		}
	}
	return false
}

// loadFromFile is the single-file convenience wrapper around
// loadFromFiles. It is retained as the entry point used by the
// existing Phase 2a tests; multi-file callers go through
// loadFromFiles directly via AppBuilder.FromConfigFile.
func loadFromFile(path string) (*app.Config, error) {
	return loadFromFiles([]string{path}, configLoadOptions{})
}

// configLoadOptions carries the toggles the AppBuilder threads into
// the loader. The struct lets Phase 2c grow new fields without
// churning the `loadFromFiles` signature: today it carries the
// mixed-format strict flag (`WithConfigStrict(true)`) and the
// unknown-fields mode (`WithUnknownFields("strict"|"warn")`).
type configLoadOptions struct {
	strict        bool
	unknownFields string // "strict" (default) or "warn"
}

// loadFromFiles is the Phase 2b multi-file loader. The precedence
// chain inside the file list is:
//
//	struct defaults < file[0] < file[1] < … < file[N-1]
//
// For each file, in order:
//
//  1. Detect format from extension; reject unknown.
//  2. Read with the 1 MiB cap (eliminates parser-DoS classes).
//  3. Parse to a fresh `*koanf.Koanf` instance.
//  4. Walk the file's flat keys: extract `_append` / `_remove`
//     operators (apply against the running result); detect `null`
//     values (revert to default — unless the key is in the
//     non-nullable security set, in which case fail loud).
//  5. Strict-schema-check whatever remains in the file's koanf
//     instance — same Phase 2a rules.
//  6. Deep-merge the cleaned file into the running result.
//
// Mixed-format detection runs across the path list before any file
// is read. With `WithConfigStrict(true)`, mixed formats are a boot
// error; otherwise a single WARN is emitted via the default slog
// logger and the load proceeds.
func loadFromFiles(paths []string, opts configLoadOptions) (*app.Config, error) {
	k, _, err := loadMerged(paths, opts)
	if err != nil {
		return nil, err
	}

	var cfg app.Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("nucleus: unmarshal merged configuration: %w", err)
	}
	// Apply the same runtime-config normalisation `app.LoadConfig`
	// uses before returning — multi-tenant / multi-site / admin /
	// database alias canonicalisation. Without this, callers that
	// hold the returned `*Config` directly (Phase 3 `/_/config`,
	// future tests) would see an un-normalised view. `app.New` would
	// re-normalise downstream, but the contract for the public
	// loader is "your Config matches what app.LoadConfig would
	// produce".
	app.NormalizeRuntimeConfig(&cfg)
	return &cfg, nil
}

// loadMerged runs the Phase 2 layered merge (struct defaults < file[0] <
// … < file[N-1]) and returns the merged koanf together with a per-key
// provenance map (ADR-010 Phase 3, compliance #6). It is the shared core
// behind loadFromFiles (which unmarshals to *app.Config) and loadEffective
// (which flattens to an EffectiveConfig).
//
// Source attribution is by snapshot-and-diff: every key seeded from the
// struct defaults starts as ConfigSource{Kind: sourceKindDefault}; after
// each file is processed (operators, null-reverts, deep-merge) any key
// whose effective value changed or first appeared is attributed to that
// file, and any key that disappeared is dropped. A `null` revert points the
// key back at the default (the effective value IS the default), not at the
// file that wrote the null. After the file loop, the `NUCLEUS_`-prefixed env
// layer is applied (Phase 3.1) and its keys are attributed to "env"; the CLI-
// flags and programmatic-override layers of ADR-010 §4 are not applied in this
// path, so no key is ever attributed to them here.
func loadMerged(paths []string, opts configLoadOptions) (*koanf.Koanf, map[string]ConfigSource, error) {
	if len(paths) == 0 {
		return nil, nil, errors.New("nucleus: FromConfigFile requires at least one path")
	}

	// Format detection up front: catch unknown extensions and mixed
	// formats before any file is opened.
	formats := make([]configFormat, len(paths))
	for i, p := range paths {
		if p == "" {
			return nil, nil, fmt.Errorf("nucleus: FromConfigFile path[%d] is empty", i)
		}
		formats[i] = detectFormat(p)
		if formats[i] == formatUnknown {
			return nil, nil, fmt.Errorf("%w: extension of %q is not one of .yaml/.yml/.toml/.json", ErrUnsupportedConfigFormat, p)
		}
	}
	if err := checkMixedFormats(paths, formats, opts.strict); err != nil {
		return nil, nil, err
	}

	// Resolve the effective unknown-fields mode for this load.
	// ADR-010 §15: NUCLEUS_ENV=production overrides any code-level
	// WithUnknownFields("warn") back to strict, and emits a WARN
	// slog event recording the override. WithUnknownFields("warn")
	// active outside production emits a different WARN that names
	// the operator footgun ("do not deploy to production").
	effectiveUnknownFields := opts.unknownFields
	if effectiveUnknownFields == "" {
		effectiveUnknownFields = UnknownFieldsStrict
	}
	prodEnv := isProductionEnv()
	if effectiveUnknownFields == UnknownFieldsWarn && prodEnv {
		slog.Default().Warn("nucleus: production environment overrides WithUnknownFields to STRICT",
			"env_var", "NUCLEUS_ENV",
			"env_value", EnvProduction,
			"requested_mode", UnknownFieldsWarn,
			"effective_mode", UnknownFieldsStrict,
		)
		effectiveUnknownFields = UnknownFieldsStrict
	} else if effectiveUnknownFields == UnknownFieldsWarn {
		slog.Default().Warn("nucleus: unknown-fields mode is WARN, not STRICT — do not deploy to production",
			"mode", UnknownFieldsWarn,
			"override_env_var", "NUCLEUS_ENV",
			"override_env_value", EnvProduction,
		)
	}

	// Initialise the running koanf with struct defaults. Every file's
	// cleaned content is deep-merged onto this — last-file-wins for
	// scalars, deep-merge for maps, replace-by-default for lists.
	k := koanf.New(".")
	if err := k.Load(structs.Provider(defaultsForConfig(), "koanf"), nil); err != nil {
		return nil, nil, fmt.Errorf("nucleus: load defaults: %w", err)
	}

	// Keep a separate read-only koanf of just the defaults so that
	// the null operator ("revert to default") can pull the original
	// default value regardless of what intermediate files set the
	// key to.
	defaultsK := koanf.New(".")
	if err := defaultsK.Load(structs.Provider(defaultsForConfig(), "koanf"), nil); err != nil {
		return nil, nil, fmt.Errorf("nucleus: snapshot defaults: %w", err)
	}

	// Provenance: every default-derived key starts attributed to the
	// struct defaults. Each file re-attributes the keys it changes.
	sources := make(map[string]ConfigSource)
	for key := range k.All() {
		sources[key] = ConfigSource{Kind: sourceKindDefault}
	}

	schemaKeys := app.ContractConfigKeyPatterns()
	for i, path := range paths {
		data, err := readFileWithCap(path, MaxConfigFileBytes)
		if err != nil {
			return nil, nil, err
		}

		parser, ok := parserFor(formats[i])
		if !ok {
			// Defensive — format was validated above.
			return nil, nil, fmt.Errorf("%w: no parser registered for %s", ErrUnsupportedConfigFormat, formats[i])
		}

		fileK := koanf.New(".")
		if err := fileK.Load(rawbytes.Provider(data), parser); err != nil {
			return nil, nil, fmt.Errorf("nucleus: parse %s: %w", path, err)
		}

		// Capture the file's null-revert keys before processOperatorsAndNull
		// strips them: a `null` reverts the key to its default, so its
		// provenance is the default, not this file.
		nullReverts := make(map[string]struct{})
		for key, val := range fileK.All() {
			if val == nil && !isNonNullableSecurityKey(key) {
				nullReverts[key] = struct{}{}
			}
		}

		// Snapshot the running result so changed/new keys can be
		// attributed to this file after the merge.
		before := k.All()

		// Apply _append / _remove operators against the running result,
		// handle null security keys, and strip operator / null keys
		// from fileK so the subsequent strict-schema check sees only
		// real keys and so the Merge does not overwrite the operator's
		// result.
		if err := processOperatorsAndNull(k, fileK, defaultsK, path); err != nil {
			return nil, nil, err
		}

		// Layer 2 schema, same as Phase 2a — after operators have
		// been stripped so a key like `cors_origins_append` does not
		// trip the unknown-keys check. ADR-010 §15: in `warn` mode
		// the loader emits a per-file `WARN` listing the offending
		// keys but proceeds with the merge; in `strict` mode (the
		// default, also forced by NUCLEUS_ENV=production) the load
		// rejects with `ErrUnknownConfigKeys`.
		if unknown := unknownKeys(fileK.All(), schemaKeys); len(unknown) > 0 {
			if effectiveUnknownFields == UnknownFieldsWarn {
				slog.Default().Warn("nucleus: unknown configuration key(s) ignored under WithUnknownFields(\"warn\"); strict mode would reject",
					"path", path,
					"keys", unknown,
				)
				// Strip the unknown keys from fileK so the
				// subsequent Merge does not splash unrecognised
				// values into the running config.
				for _, k := range unknown {
					fileK.Delete(k)
				}
			} else {
				return nil, nil, formatUnknownKeys(unknown, schemaKeys, path)
			}
		}

		// Deep-merge the file's cleaned content into the running
		// result. koanf.Merge deep-merges nested maps and overwrites
		// scalars — exactly the ADR-010 §3 semantics for plain keys.
		if err := k.Merge(fileK); err != nil {
			return nil, nil, fmt.Errorf("nucleus: merge %s: %w", path, err)
		}

		// Attribute provenance for this file. operators/plain-key merges
		// and overrides surface as changed-or-new keys; null-reverts with
		// no registered default surface as removed keys. For YAML, capture
		// the per-key source line (Phase 3.1); other formats report kind+path.
		var lineMap map[string]int
		if formats[i] == formatYAML {
			lineMap = yamlLineMap(data)
		}
		after := k.All()
		for key, val := range after {
			if prev, ok := before[key]; !ok || !reflect.DeepEqual(prev, val) {
				src := ConfigSource{Kind: formats[i].String(), Path: path}
				if ln, ok := lineMap[key]; ok {
					src.Line = ln
				}
				sources[key] = src
			}
		}
		for key := range before {
			if _, ok := after[key]; !ok {
				delete(sources, key)
			}
		}
		// A null-revert that landed on a registered default is sourced
		// from the default, not this file (overrides the diff above,
		// which sees the value change).
		for key := range nullReverts {
			if _, ok := after[key]; ok {
				sources[key] = ConfigSource{Kind: sourceKindDefault}
			}
		}
	}

	// Env layer (ADR-010 §4: defaults < files < env). Phase 3.1 wires the
	// `NUCLEUS_`-prefixed environment variables — the same provider and
	// `__`→`.` transform `app.LoadConfig` uses — into this path so the fluent
	// builder honours the documented precedence (previously env never applied
	// here) and so env-sourced keys carry real `[env:NAME]` provenance. Only
	// schema-recognised keys are applied: env is an ambient, shared namespace,
	// so an unrecognised `NUCLEUS_`-prefixed variable is ignored rather than
	// treated as an authored mistake (files are strictly validated; env is
	// not). The CLI flags and programmatic-override layers of §4 remain
	// outside this path.
	if err := applyEnvLayer(k, sources, schemaKeys); err != nil {
		return nil, nil, err
	}

	return k, sources, nil
}

// yamlLineMap parses raw YAML bytes into a node tree and returns a map from
// each dotted key to the 1-based line its key token appears on (Phase 3.1
// file:line provenance). Only called for formatYAML files. It mirrors koanf's
// flattening — nested mappings become `parent.child`, and a list/scalar value
// is recorded under its key — so the keys line up with the merged koanf's
// `All()`. Intermediate mapping keys (e.g. `databases`) are recorded too but
// simply go unused since only leaf keys appear in the effective config.
//
// It is best-effort: the koanf YAML parser separately validates the bytes, so
// a parse error or a non-mapping document root yields an empty or nil map.
// Callers treat both identically — a missing key reads back as line 0
// (omitted). Known limitations, acceptable for provenance: keys reached only
// through anchors/aliases or YAML merge keys (`<<`) carry no line, and a key
// produced by an `_append`/`_remove` operator is attributed to the file but
// without a line (the operator key is stripped before the merge, so the base
// key has no direct token).
func yamlLineMap(data []byte) map[string]int {
	var root yamlnode.Node
	if err := yamlnode.Unmarshal(data, &root); err != nil || len(root.Content) == 0 {
		return nil
	}
	out := make(map[string]int)
	walkYAMLNode("", root.Content[0], out)
	return out
}

// walkYAMLNode recursively records key→line for every mapping entry under
// node, prefixing nested keys with their dotted path. Content entries from
// yamlnode.Unmarshal are never nil, so no per-entry nil guard is needed.
func walkYAMLNode(prefix string, node *yamlnode.Node, out map[string]int) {
	if node == nil || node.Kind != yamlnode.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		// `<<` is a YAML merge-key directive, not a config key — skip it so
		// it never pollutes the line map.
		if keyNode.Value == "<<" {
			continue
		}
		dotted := keyNode.Value
		if prefix != "" {
			dotted = prefix + "." + keyNode.Value
		}
		out[dotted] = keyNode.Line
		if valNode.Kind == yamlnode.MappingNode {
			walkYAMLNode(dotted, valNode, out)
		}
	}
}

// applyEnvLayer merges `NUCLEUS_`-prefixed environment variables onto the
// running koanf and attributes each applied key to the originating variable.
// Env values are strings (as the OS delivers them); koanf coerces them at
// Unmarshal time, identically to `app.LoadConfig` — a non-coercible value
// (e.g. `NUCLEUS_PORT=abc` against the int `port`) surfaces as an Unmarshal
// error from loadFromFiles, not here.
//
// Keys that do not match the schema (`app.ContractConfigKeyPatterns()`) are
// skipped so a stray `NUCLEUS_`-prefixed variable neither pollutes the typed
// config nor the effective snapshot. Unlike the file layer, an unrecognised
// env key is NOT an error: env is a shared ambient namespace, so an unrelated
// `NUCLEUS_`-prefixed variable (or an operator typo) is ignored rather than
// failing the boot.
//
// Non-nullable security keys (ADR-010 §14, e.g. `jwt_secret`) reject an empty
// env value the same way the file layer rejects `null`: an empty
// `NUCLEUS_JWT_SECRET=` would otherwise silently overwrite a file-set secret
// and disable signing. Env strings can never be nil, so only the empty-string
// degenerate case needs guarding here.
func applyEnvLayer(k *koanf.Koanf, sources map[string]ConfigSource, schemaKeys []string) error {
	// The transform callback fires once per matching variable during
	// envK.Load, in os.Environ() order, recording envVarByKey[dottedKey] in
	// lockstep with the value koanf stores. If two variables collapse to the
	// same dotted key (e.g. NUCLEUS_PORT and NUCLEUS_port on a case-sensitive
	// OS), the last one in that order wins for BOTH the value and the recorded
	// Path, so provenance always names the variable whose value won.
	envVarByKey := make(map[string]string)
	envK := koanf.New(".")
	if err := envK.Load(env.Provider(envVarPrefix, ".", func(s string) string {
		key := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, envVarPrefix), "__", "."))
		envVarByKey[key] = s
		return key
	}), nil); err != nil {
		return fmt.Errorf("nucleus: load environment overrides: %w", err)
	}

	patterns := compileKeyPatterns(schemaKeys)
	for key, val := range envK.All() {
		if !keyMatchesAny(key, patterns) {
			continue // unrecognised NUCLEUS_-prefixed variable — ignore
		}
		if isNonNullableSecurityKey(key) {
			if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
				return fmt.Errorf("%w: %q via %s is empty — set a value or unset the variable", ErrSecurityKeyNotNullable, key, envVarByKey[key])
			}
		}
		if err := k.Set(key, val); err != nil {
			return fmt.Errorf("nucleus: apply env override %s: %w", envVarByKey[key], err)
		}
		// Env is the highest-precedence layer applied in this path, so it owns
		// the provenance for every key it sets — even when the value equals
		// what a file already held, the operator set it via the environment.
		sources[key] = ConfigSource{Kind: sourceKindEnv, Path: envVarByKey[key]}
	}
	return nil
}

// processOperatorsAndNull walks fileK and applies the ADR-010 §3
// merge-time operators against the running result k. Operator and
// null keys are deleted from fileK so the subsequent strict-schema
// check sees only "real" keys and so koanf.Merge does not later
// overwrite the operator-derived value with the operator's raw
// representation. The defaultsK koanf provides the source-of-truth
// value for null-reverts (ADR-010 §3: "null unsets and reverts to
// default").
func processOperatorsAndNull(k, fileK, defaultsK *koanf.Koanf, path string) error {
	// Snapshot the keys first — we mutate fileK during the loop, and
	// koanf.All() returns a fresh map but iterating a freshly-mutated
	// koanf is undefined.
	all := fileK.All()
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys) // deterministic order keeps error messages stable

	for _, key := range keys {
		val := all[key]

		// null handling: ADR-010 §3 says null reverts to default,
		// except for keys in the non-nullable security set where
		// null is a boot error.
		if val == nil {
			if isNonNullableSecurityKey(key) {
				return fmt.Errorf("%w: %q in %s — set an explicit value or remove the key", ErrSecurityKeyNotNullable, key, path)
			}
			// Revert-to-default: set k[key] back to the value from
			// the framework's struct defaults (or unset if no default
			// is registered for the key). Then strip the null entry
			// from fileK so the subsequent Merge does not re-apply
			// it.
			if defaultsK.Exists(key) {
				if err := k.Set(key, defaultsK.Get(key)); err != nil {
					return fmt.Errorf("nucleus: %s: revert %s to default: %w", path, key, err)
				}
			} else {
				k.Delete(key)
			}
			fileK.Delete(key)
			continue
		}

		// Note on operator precedence: keys are processed in sorted
		// order (see the sort above), so for the same base key in
		// the same file, `_append` runs before `_remove` (alphabetic
		// order: "a" < "r"). Within a single file this is rarely
		// meaningful; across files each operator applies to the
		// running result the previous file produced.
		switch {
		case strings.HasSuffix(key, operatorSuffixAppend):
			baseKey := strings.TrimSuffix(key, operatorSuffixAppend)
			if baseKey == "" {
				return fmt.Errorf("nucleus: %s: operator key %q has no base key name", path, key)
			}
			merged, err := applyAppend(k.Get(baseKey), val)
			if err != nil {
				return fmt.Errorf("nucleus: %s: %s: %w", path, key, err)
			}
			if err := k.Set(baseKey, merged); err != nil {
				return fmt.Errorf("nucleus: %s: set %s: %w", path, baseKey, err)
			}
			fileK.Delete(key)

		case strings.HasSuffix(key, operatorSuffixRemove):
			baseKey := strings.TrimSuffix(key, operatorSuffixRemove)
			if baseKey == "" {
				return fmt.Errorf("nucleus: %s: operator key %q has no base key name", path, key)
			}
			filtered, err := applyRemove(k.Get(baseKey), val)
			if err != nil {
				return fmt.Errorf("nucleus: %s: %s: %w", path, key, err)
			}
			if err := k.Set(baseKey, filtered); err != nil {
				return fmt.Errorf("nucleus: %s: set %s: %w", path, baseKey, err)
			}
			fileK.Delete(key)

		default:
			// Plain key: leave it in fileK so the subsequent Merge
			// applies it under the standard deep-merge / replace
			// rules. No work to do here.
		}
	}
	return nil
}

// applyAppend returns existing + newVals where both are coerced to
// `[]any`. Append-on-nil yields the new value as a fresh list. The
// helper never errors today; the signature retains an `error` return
// so a future strict-type-check (e.g. refusing to append to a map)
// can land without touching call sites.
func applyAppend(existing, newVals any) ([]any, error) {
	existingList := coerceToList(existing)
	newList := coerceToList(newVals)
	out := make([]any, 0, len(existingList)+len(newList))
	out = append(out, existingList...)
	out = append(out, newList...)
	return out, nil
}

// applyRemove returns existing minus newVals. Equality is by
// JSON-marshalled byte comparison, which gives a deterministic
// representation regardless of map-key insertion order — important
// for struct-typed list elements like `jwt_keys[]` that koanf
// surfaces as `map[string]any`. For elements that cannot be JSON-
// marshalled (rare in practice — config values are scalars or
// nested maps) the loader falls back to `fmt.Sprint`. Removing an
// entry that does not exist is a no-op (so removes are idempotent
// across multiple file loads). The `error` return mirrors
// `applyAppend` for symmetry / future-proofing.
func applyRemove(existing, newVals any) ([]any, error) {
	existingList := coerceToList(existing)
	rmList := coerceToList(newVals)
	rmSet := make(map[string]struct{}, len(rmList))
	for _, v := range rmList {
		rmSet[canonicalKey(v)] = struct{}{}
	}
	out := make([]any, 0, len(existingList))
	for _, v := range existingList {
		if _, drop := rmSet[canonicalKey(v)]; drop {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// canonicalKey returns a deterministic string representation of v
// suitable for set-membership comparison. JSON marshalling sorts
// map keys alphabetically, so two equivalent map-typed entries
// produce identical keys regardless of insertion order. For values
// that fail to marshal (uncommon — config values are JSON-friendly
// by construction since they come from YAML/TOML/JSON parsers),
// `fmt.Sprint` is the fallback; it is non-deterministic for maps
// but correctly handles the scalar / list types most config lists
// carry.
func canonicalKey(v any) string {
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

// coerceToList accepts a nil / []any / single-element value and
// returns a `[]any`. Scalars are wrapped in a one-element list — this
// matches user intent for a YAML like `cors_origins_append: https://x`
// (single string rather than a list). A nil input is treated as the
// empty list, which is what callers want when the base key has no
// existing value yet.
func coerceToList(v any) []any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	default:
		return []any{t}
	}
}

// checkMixedFormats inspects the format slice and, if more than one
// distinct format appears, either rejects (strict) or emits a
// `WARN`-level slog event (default). The intent is to make
// mixed-format usage visible without breaking it for callers who
// genuinely need to ride the migration window between two formats.
func checkMixedFormats(paths []string, formats []configFormat, strict bool) error {
	seen := make(map[configFormat]struct{}, len(formats))
	for _, f := range formats {
		seen[f] = struct{}{}
	}
	if len(seen) <= 1 {
		return nil
	}

	formatStrings := make([]string, 0, len(seen))
	for f := range seen {
		formatStrings = append(formatStrings, f.String())
	}
	sort.Strings(formatStrings)

	if strict {
		return fmt.Errorf("%w: %v across paths %v — drop AppBuilder.WithConfigStrict(true) or unify the formats", ErrMixedConfigFormats, formatStrings, paths)
	}
	slog.Default().Warn("nucleus: configuration files mix formats; this is permitted but discouraged — consider AppBuilder.WithConfigStrict(true) or unifying",
		"formats", formatStrings,
		"paths", paths,
	)
	return nil
}

// readFileWithCap reads up to capBytes+1 bytes from path. When the
// file is larger than capBytes (the +1 is the overshoot signalling),
// it returns ErrConfigFileTooLarge wrapped with the path. Stat is not
// used as the only check because some filesystems (procfs, FUSE) lie
// about file size; reading is the source of truth.
func readFileWithCap(path string, capBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("nucleus: open %s: %w", path, err)
	}
	defer f.Close()

	limited := io.LimitReader(f, capBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("nucleus: read %s: %w", path, err)
	}
	if int64(len(data)) > capBytes {
		return nil, fmt.Errorf("%w (path=%q, cap=%d bytes)", ErrConfigFileTooLarge, path, capBytes)
	}
	return data, nil
}

// defaultsForConfig returns the same defaults app.LoadConfig uses,
// reached through the public app.DefaultConfig accessor so this
// package does not need to import pkg/app's internals.
func defaultsForConfig() app.Config {
	return app.DefaultConfig()
}

// unknownKeys returns the leaf keys present in the file-koanf's
// flattened map that do NOT appear in any schemaKey prefix. The
// `app.ContractConfigKeyPatterns()` set is the canonical schema
// surface — it enumerates the koanf-bindable keys
// `pkg/app.Config` and its nested structs expose.
//
// A key matches the schema if any schemaKey is either equal to the
// key or is a pattern whose wildcard segments (`*`, `<alias>`,
// `<site>`, `<tenant>`, …) the key's corresponding segments fill.
// Slice-typed schema slots are written with a trailing `[]` in
// `app.ContractConfigKeyPatterns()`; that suffix is stripped during
// pattern compilation since koanf flattens a slice value under the
// key itself, not under `key[]`.
func unknownKeys(loaded map[string]any, schemaKeys []string) []string {
	patterns := compileKeyPatterns(schemaKeys)
	var unknown []string
	for k := range loaded {
		if !keyMatchesAny(k, patterns) {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// compiledKeyPattern is the segment-by-segment shape used by
// keyMatchesAny. Wildcard segments (`*`, `<…>`) are matched against
// any single segment in the key under test; literal segments must
// match byte-for-byte.
type compiledKeyPattern []string

func compileKeyPatterns(patterns []string) []compiledKeyPattern {
	out := make([]compiledKeyPattern, 0, len(patterns))
	for _, p := range patterns {
		// Strip the `[]` slice marker on the last segment — koanf
		// keeps slice values under the bare key, not under `key[]`.
		p = strings.TrimSuffix(p, "[]")
		out = append(out, strings.Split(p, "."))
	}
	return out
}

// keyMatchesAny reports whether key matches at least one of the
// supplied patterns. Matching is segment-by-segment; a pattern
// segment is a wildcard when it is `*` or any string of the form
// `<…>` (the placeholder convention used by
// `app.ContractConfigKeyPatterns()`).
func keyMatchesAny(key string, patterns []compiledKeyPattern) bool {
	segments := strings.Split(key, ".")
	for _, pat := range patterns {
		if len(pat) != len(segments) {
			continue
		}
		match := true
		for i, p := range pat {
			if isWildcardSegment(p) {
				continue
			}
			if p != segments[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// isWildcardSegment reports whether a pattern segment matches any
// key segment. Two forms are recognised: the literal `*` (used by
// hand-built test patterns and by the operator-stripping path) and
// the `<…>` placeholder convention used by
// `app.ContractConfigKeyPatterns()` (e.g. `databases.<alias>.url`).
func isWildcardSegment(s string) bool {
	return s == "*" || (strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">"))
}

// formatUnknownKeys produces an ErrUnknownConfigKeys-wrapped error
// listing every unknown key with a did-you-mean hint when a close
// match exists in the schema (within a Levenshtein-style edit
// distance of 3 on the deepest-segment basis). The originating file
// path is folded into the error preamble so multi-file loads point
// the operator at the right place ("unknown configuration key(s) in
// foo.yaml:\n  - key" reads naturally).
func formatUnknownKeys(unknown, schemaKeys []string, path string) error {
	preamble := ErrUnknownConfigKeys.Error()
	if path != "" {
		preamble += " in " + path
	}
	var b strings.Builder
	b.WriteString(preamble)
	b.WriteString(":")
	for _, k := range unknown {
		b.WriteString("\n  - ")
		b.WriteString(k)
		if hint := didYouMean(k, schemaKeys); hint != "" {
			b.WriteString(" (did you mean ")
			b.WriteString(hint)
			b.WriteString("?)")
		}
	}
	// Wrap the sentinel so errors.Is(err, ErrUnknownConfigKeys)
	// still works, but render the preamble via the assembled string
	// above so the path annotation reads naturally.
	return fmt.Errorf("%s [%w]", b.String(), ErrUnknownConfigKeys)
}

// didYouMean returns the closest schema key to `unknown` within an
// edit-distance threshold of 3 on the final segment, or the empty
// string when no schema key is close enough. The intent is to catch
// typos like `loging.level` → `log_level` without producing noisy
// false-positive hints.
func didYouMean(unknown string, schemaKeys []string) string {
	uTail := lastSegment(unknown)
	if uTail == "" {
		return ""
	}
	best := ""
	bestDist := 4 // accept distance ≤3; reject 4+
	for _, k := range schemaKeys {
		sTail := lastSegment(k)
		if sTail == "" {
			continue
		}
		d := levenshtein(uTail, sTail)
		if d < bestDist {
			bestDist = d
			best = k
		}
	}
	return best
}

func lastSegment(k string) string {
	// Strip trailing `[]` (slice marker) before comparison so a typo
	// like `log_redact_extra_key` suggests `log_redact_extra_keys[]`
	// without the bracket noise getting in the way.
	k = strings.TrimSuffix(k, "[]")
	if i := strings.LastIndex(k, "."); i >= 0 {
		return k[i+1:]
	}
	return k
}

// levenshtein computes the edit distance between two ASCII strings.
// Simple O(n*m) DP — config keys are short (rarely >30 chars), so
// the allocation cost is negligible compared with the readability
// win of a textbook implementation.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = del
			if ins < curr[j] {
				curr[j] = ins
			}
			if sub < curr[j] {
				curr[j] = sub
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
