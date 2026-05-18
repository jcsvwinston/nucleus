// Package nucleus — config.go implements the configuration loader
// surfaced by `AppBuilder.FromConfigFile`. ADR-010 §2 names this as
// Phase 2 work. Phase 2a (PR #73) shipped the single-file YAML loader
// with the 1 MiB size cap, schema strict-unknown-fields validation,
// and did-you-mean hints. Phase 2b (this file) layers on top:
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
// The package-level `Run(App)` and the direct-struct surface never
// traverse this loader — only the builder-chain `FromConfigFile` does.
//
// What's still deferred:
//
//   - Phase 2c — `WithUnknownFields("warn")` opt-out from strict
//     schema validation and the `NUCLEUS_ENV=production` strict
//     override.
//   - Phase 2d — module migration namespacing in `pkg/db/migrate.go`.
//   - Phase 3 — `/_/config` endpoint and `nucleus config print
//     --effective` (which require per-key source tracking the loader
//     does not yet capture).
package nucleus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
	jsonparser "github.com/knadh/koanf/parsers/json"
	tomlparser "github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
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

// loadFromFile is the single-file convenience wrapper around
// loadFromFiles. It is retained as the entry point used by the
// existing Phase 2a tests; multi-file callers go through
// loadFromFiles directly via AppBuilder.FromConfigFile.
func loadFromFile(path string) (*app.Config, error) {
	return loadFromFiles([]string{path}, configLoadOptions{})
}

// configLoadOptions carries the toggles the AppBuilder threads into
// the loader. Currently a single flag — `WithConfigStrict(true)` —
// but the struct gives Phase 2c room to grow without churning the
// loadFromFiles signature.
type configLoadOptions struct {
	strict bool
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
	if len(paths) == 0 {
		return nil, errors.New("nucleus: FromConfigFile requires at least one path")
	}

	// Format detection up front: catch unknown extensions and mixed
	// formats before any file is opened.
	formats := make([]configFormat, len(paths))
	for i, p := range paths {
		if p == "" {
			return nil, fmt.Errorf("nucleus: FromConfigFile path[%d] is empty", i)
		}
		formats[i] = detectFormat(p)
		if formats[i] == formatUnknown {
			return nil, fmt.Errorf("%w: extension of %q is not one of .yaml/.yml/.toml/.json", ErrUnsupportedConfigFormat, p)
		}
	}
	if err := checkMixedFormats(paths, formats, opts.strict); err != nil {
		return nil, err
	}

	// Initialise the running koanf with struct defaults. Every file's
	// cleaned content is deep-merged onto this — last-file-wins for
	// scalars, deep-merge for maps, replace-by-default for lists.
	k := koanf.New(".")
	if err := k.Load(structs.Provider(defaultsForConfig(), "koanf"), nil); err != nil {
		return nil, fmt.Errorf("nucleus: load defaults: %w", err)
	}

	// Keep a separate read-only koanf of just the defaults so that
	// the null operator ("revert to default") can pull the original
	// default value regardless of what intermediate files set the
	// key to.
	defaultsK := koanf.New(".")
	if err := defaultsK.Load(structs.Provider(defaultsForConfig(), "koanf"), nil); err != nil {
		return nil, fmt.Errorf("nucleus: snapshot defaults: %w", err)
	}

	schemaKeys := app.ContractConfigKeyPatterns()
	for i, path := range paths {
		data, err := readFileWithCap(path, MaxConfigFileBytes)
		if err != nil {
			return nil, err
		}

		parser, ok := parserFor(formats[i])
		if !ok {
			// Defensive — format was validated above.
			return nil, fmt.Errorf("%w: no parser registered for %s", ErrUnsupportedConfigFormat, formats[i])
		}

		fileK := koanf.New(".")
		if err := fileK.Load(rawbytes.Provider(data), parser); err != nil {
			return nil, fmt.Errorf("nucleus: parse %s: %w", path, err)
		}

		// Apply _append / _remove operators against the running result,
		// handle null security keys, and strip operator / null keys
		// from fileK so the subsequent strict-schema check sees only
		// real keys and so the Merge does not overwrite the operator's
		// result.
		if err := processOperatorsAndNull(k, fileK, defaultsK, path); err != nil {
			return nil, err
		}

		// Layer 2 strict schema, same as Phase 2a — after operators
		// have been stripped so a key like `cors_origins_append` does
		// not trip the unknown-keys check.
		if unknown := unknownKeys(fileK.All(), schemaKeys); len(unknown) > 0 {
			return nil, formatUnknownKeys(unknown, schemaKeys, path)
		}

		// Deep-merge the file's cleaned content into the running
		// result. koanf.Merge deep-merges nested maps and overwrites
		// scalars — exactly the ADR-010 §3 semantics for plain keys.
		if err := k.Merge(fileK); err != nil {
			return nil, fmt.Errorf("nucleus: merge %s: %w", path, err)
		}
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
