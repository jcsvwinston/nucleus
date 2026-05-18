package nucleus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempConfig writes content to a temp file with the given
// extension and returns the path. Cleanup happens via t.TempDir().
func writeTempConfig(t *testing.T, ext, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config"+ext)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadFromFile_HappyPathYAML(t *testing.T) {
	t.Parallel()

	yamlBody := `
host: 0.0.0.0
port: 9090
log_level: warn
`
	path := writeTempConfig(t, ".yaml", yamlBody)
	cfg, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host: got %q want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port: got %d want %d", cfg.Port, 9090)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "warn")
	}
}

func TestLoadFromFile_PreservesDefaultsForUnsetKeys(t *testing.T) {
	t.Parallel()

	// Body sets only Port; every other field should come from
	// app.DefaultConfig() (struct defaults applied first).
	path := writeTempConfig(t, ".yaml", "port: 1234\n")
	cfg, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}
	if cfg.Port != 1234 {
		t.Errorf("Port: got %d want 1234", cfg.Port)
	}
	// LogLevel is part of app.DefaultConfig and must survive the load.
	if cfg.LogLevel == "" {
		t.Error("LogLevel was reset to zero value; defaults not applied")
	}
}

func TestLoadFromFile_RejectsUnsupportedExtension(t *testing.T) {
	t.Parallel()

	path := writeTempConfig(t, ".ini", "[server]\nport = 80\n")
	_, err := loadFromFile(path)
	if err == nil {
		t.Fatal("expected an error for .ini extension")
	}
	if !errors.Is(err, ErrUnsupportedConfigFormat) {
		t.Errorf("want ErrUnsupportedConfigFormat, got %v", err)
	}
}

func TestLoadFromFile_TOMLHappyPath(t *testing.T) {
	t.Parallel()

	tomlBody := `host = "127.0.0.1"
port = 8181
log_level = "info"
`
	path := writeTempConfig(t, ".toml", tomlBody)
	cfg, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile(.toml): %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host: got %q want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != 8181 {
		t.Errorf("Port: got %d want %d", cfg.Port, 8181)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "info")
	}
}

func TestLoadFromFile_JSONHappyPath(t *testing.T) {
	t.Parallel()

	jsonBody := `{
  "host": "0.0.0.0",
  "port": 4444,
  "log_level": "debug"
}
`
	path := writeTempConfig(t, ".json", jsonBody)
	cfg, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile(.json): %v", err)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host: got %q want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != 4444 {
		t.Errorf("Port: got %d want %d", cfg.Port, 4444)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadFromFile_FileTooLarge(t *testing.T) {
	t.Parallel()

	// Build a YAML body larger than MaxConfigFileBytes. Use a single
	// long scalar to avoid producing valid YAML by accident — the
	// cap is enforced BEFORE the parser ever runs.
	big := strings.Repeat("# padding line that takes some bytes per line\n", (MaxConfigFileBytes/40)+1)
	path := writeTempConfig(t, ".yaml", big)
	_, err := loadFromFile(path)
	if !errors.Is(err, ErrConfigFileTooLarge) {
		t.Fatalf("want ErrConfigFileTooLarge, got %v", err)
	}
	if !strings.Contains(err.Error(), "cap=") {
		t.Errorf("error should mention the cap, got %q", err.Error())
	}
}

func TestLoadFromFile_FileAtCapBoundaryIsAccepted(t *testing.T) {
	t.Parallel()

	// Exactly at the cap should succeed. Use a body sized so the
	// final file is at or just below MaxConfigFileBytes. The body
	// must remain parseable, so we keep one valid key + padding
	// comment lines.
	header := "port: 8080\n"
	want := MaxConfigFileBytes
	pad := want - len(header)
	if pad < 0 {
		t.Skip("MaxConfigFileBytes is smaller than the header; skipping boundary test")
	}
	body := header + strings.Repeat("#", pad)
	if len(body) > MaxConfigFileBytes {
		body = body[:MaxConfigFileBytes]
	}
	path := writeTempConfig(t, ".yaml", body)
	cfg, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("expected boundary-sized file to load, got %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: got %d want 8080", cfg.Port)
	}
}

func TestLoadFromFile_StrictUnknownKey(t *testing.T) {
	t.Parallel()

	// `prot` is a likely typo for `port`. Strict mode rejects it.
	path := writeTempConfig(t, ".yaml", "prot: 80\n")
	_, err := loadFromFile(path)
	if !errors.Is(err, ErrUnknownConfigKeys) {
		t.Fatalf("want ErrUnknownConfigKeys, got %v", err)
	}
}

func TestLoadFromFile_DidYouMeanHint(t *testing.T) {
	t.Parallel()

	// `loging_level` is one insertion away from `log_level`. The
	// hint should surface.
	path := writeTempConfig(t, ".yaml", "loging_level: warn\n")
	_, err := loadFromFile(path)
	if !errors.Is(err, ErrUnknownConfigKeys) {
		t.Fatalf("want ErrUnknownConfigKeys, got %v", err)
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error should include a did-you-mean hint, got %q", err.Error())
	}
}

func TestLoadFromFile_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := loadFromFile("/nonexistent/path/nucleus.yaml")
	if err == nil {
		t.Fatal("expected an error for missing file")
	}
	if errors.Is(err, ErrUnknownConfigKeys) || errors.Is(err, ErrConfigFileTooLarge) {
		t.Errorf("missing-file error should not be wrapped as a config-content error, got %v", err)
	}
}

func TestLoadFromFile_MalformedYAML(t *testing.T) {
	t.Parallel()

	path := writeTempConfig(t, ".yaml", "port: : bad\n  - mixed: types\n")
	_, err := loadFromFile(path)
	if err == nil {
		t.Fatal("expected a parse error for malformed YAML")
	}
}

func TestLoadFromFile_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := loadFromFile("")
	if err == nil {
		t.Fatal("expected an error for empty path")
	}
}

func TestAppBuilder_FromConfigFile_Happy(t *testing.T) {
	t.Parallel()

	path := writeTempConfig(t, ".yaml", "port: 7777\n")
	a, err := New().FromConfigFile(path).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.Port != 7777 {
		t.Errorf("Port: got %d want 7777", a.Port)
	}
	if a.Modules == nil {
		t.Error("Modules map should be non-nil after Build")
	}
}

func TestAppBuilder_FromConfigFile_PreservesPriorMount(t *testing.T) {
	t.Parallel()

	// Mount BEFORE FromConfigFile and confirm the file load does not
	// drop the registered module.
	mod := Module[struct{}]{Name: "articles", Prefix: "/articles"}.Build()
	path := writeTempConfig(t, ".yaml", "port: 7777\n")
	a, err := New().Mount(mod).FromConfigFile(path).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := a.Modules["articles"]; !ok {
		t.Error("Modules registered before FromConfigFile were dropped by the loader")
	}
}

func TestLevenshtein_Basics(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"port", "port", 0},
		{"prot", "port", 2},  // two transpositions (substitution-based distance)
		{"port", "ports", 1}, // one insertion
		{"port", "", 4},
		{"", "port", 4},
	} {
		got := levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("levenshtein(%q, %q): got %d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestKeyMatchesAny_Wildcards(t *testing.T) {
	t.Parallel()

	patterns := compileKeyPatterns([]string{
		"port",
		"databases.*.url",
		"jwt_keys.*.kid",
	})
	cases := []struct {
		key  string
		want bool
	}{
		{"port", true},
		{"databases.default.url", true},
		{"databases.analytics.url", true},
		{"databases.default.user", false}, // *.user not in patterns
		{"jwt_keys.signing.kid", true},
		{"jwt_keys.signing.algorithm", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		got := keyMatchesAny(tc.key, patterns)
		if got != tc.want {
			t.Errorf("keyMatchesAny(%q): got %v want %v", tc.key, got, tc.want)
		}
	}
}

// TestKeyMatchesAny_PlaceholderWildcards verifies the Phase 2b fix to
// the wildcard matcher: `app.ContractConfigKeyPatterns()` returns
// patterns with `<alias>` / `<site>` / `<tenant>` placeholders rather
// than `*`. Phase 2a only recognised `*`, which silently broke any
// real-world config that set `databases.<some>.url` style keys —
// they'd fail strict-schema with "unknown configuration key".
func TestKeyMatchesAny_PlaceholderWildcards(t *testing.T) {
	t.Parallel()

	patterns := compileKeyPatterns([]string{
		"databases.<alias>.url",
		"multisite.sites.<site>.hosts[]",
		"multitenant.tenants.<tenant>.database",
	})
	cases := []struct {
		key  string
		want bool
	}{
		{"databases.default.url", true},
		{"databases.reporting.url", true},
		{"multisite.sites.www.hosts", true}, // []-suffix stripped during compile
		{"multitenant.tenants.acme.database", true},
		{"databases.default.user", false},
	}
	for _, tc := range cases {
		got := keyMatchesAny(tc.key, patterns)
		if got != tc.want {
			t.Errorf("keyMatchesAny(%q): got %v want %v", tc.key, got, tc.want)
		}
	}
}

func TestLoadFromFile_DatabaseAliasNoLongerFalseUnknown(t *testing.T) {
	t.Parallel()

	// Regression test for the Phase 2a wildcard bug. Previously the
	// matcher treated `<alias>` as a literal segment, so a real config
	// with `databases.default.url` failed strict-schema validation.
	yamlBody := "databases:\n  default:\n    url: \"sqlite://:memory:\"\n"
	path := writeTempConfig(t, ".yaml", yamlBody)
	cfg, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("loadFromFile: %v", err)
	}
	if cfg.Databases["default"].URL != "sqlite://:memory:" {
		t.Errorf("databases.default.url: got %q want %q", cfg.Databases["default"].URL, "sqlite://:memory:")
	}
}

// --- Phase 2b: multi-file merge ---

func TestLoadFromFiles_LastFileWins_Scalars(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", "port: 8080\nhost: 0.0.0.0\nlog_level: info\n")
	overlay := writeTempConfig(t, ".yaml", "port: 9090\nlog_level: warn\n")

	cfg, err := loadFromFiles([]string{base, overlay}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port: got %d want 9090 (overlay should win)", cfg.Port)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "warn")
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host: got %q want %q (should survive from base)", cfg.Host, "0.0.0.0")
	}
}

func TestLoadFromFiles_DeepMergeMaps(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", `
databases:
  default:
    url: sqlite://base.db
    max_open: 10
`)
	overlay := writeTempConfig(t, ".yaml", `
databases:
  default:
    max_open: 50
  reporting:
    url: sqlite://reporting.db
`)
	cfg, err := loadFromFiles([]string{base, overlay}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	if cfg.Databases["default"].URL != "sqlite://base.db" {
		t.Errorf("databases.default.url: got %q want sqlite://base.db (deep-merge should preserve)", cfg.Databases["default"].URL)
	}
	if cfg.Databases["default"].MaxOpen != 50 {
		t.Errorf("databases.default.max_open: got %d want 50 (overlay should win)", cfg.Databases["default"].MaxOpen)
	}
	if cfg.Databases["reporting"].URL != "sqlite://reporting.db" {
		t.Errorf("databases.reporting.url: got %q want sqlite://reporting.db (overlay should add)", cfg.Databases["reporting"].URL)
	}
}

// --- Phase 2b: _append / _remove operators ---

func TestLoadFromFiles_AppendOperator(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", `
log_redact_extra_keys:
  - alpha
  - beta
`)
	overlay := writeTempConfig(t, ".yaml", `
log_redact_extra_keys_append:
  - gamma
`)
	cfg, err := loadFromFiles([]string{base, overlay}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	got := cfg.LogRedactExtraKeys
	want := []string{"alpha", "beta", "gamma"}
	if !equalStringSlice(got, want) {
		t.Errorf("log_redact_extra_keys: got %v want %v", got, want)
	}
}

func TestLoadFromFiles_RemoveOperator(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", `
log_redact_extra_keys:
  - alpha
  - beta
  - gamma
`)
	overlay := writeTempConfig(t, ".yaml", `
log_redact_extra_keys_remove:
  - beta
`)
	cfg, err := loadFromFiles([]string{base, overlay}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	got := cfg.LogRedactExtraKeys
	want := []string{"alpha", "gamma"}
	if !equalStringSlice(got, want) {
		t.Errorf("log_redact_extra_keys: got %v want %v", got, want)
	}
}

func TestLoadFromFiles_AppendThenRemove(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", `
log_redact_extra_keys:
  - alpha
`)
	mid := writeTempConfig(t, ".yaml", `
log_redact_extra_keys_append:
  - beta
  - gamma
`)
	overlay := writeTempConfig(t, ".yaml", `
log_redact_extra_keys_remove:
  - alpha
`)
	cfg, err := loadFromFiles([]string{base, mid, overlay}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	got := cfg.LogRedactExtraKeys
	want := []string{"beta", "gamma"}
	if !equalStringSlice(got, want) {
		t.Errorf("log_redact_extra_keys: got %v want %v", got, want)
	}
}

func TestLoadFromFiles_AppendDoesNotTripStrictSchema(t *testing.T) {
	t.Parallel()

	// The operator key `log_redact_extra_keys_append` is not in the
	// schema patterns set, so a naive strict-schema check would reject
	// it. The merge engine must strip operators BEFORE the strict
	// check.
	path := writeTempConfig(t, ".yaml", `
log_redact_extra_keys_append:
  - new_key
`)
	_, err := loadFromFile(path)
	if err != nil {
		t.Fatalf("operator-only file should load cleanly; got %v", err)
	}
}

// --- Phase 2b: null handling ---

func TestLoadFromFiles_NullRevertsToDefault(t *testing.T) {
	t.Parallel()

	// Base sets log_level to warn; overlay nulls it. Result should
	// revert to the framework default (whatever app.DefaultConfig
	// returns).
	defaultLogLevel := defaultsForConfig().LogLevel
	base := writeTempConfig(t, ".yaml", "log_level: warn\n")
	overlay := writeTempConfig(t, ".yaml", "log_level: null\n")
	cfg, err := loadFromFiles([]string{base, overlay}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Errorf("log_level: got %q want %q (null should revert to default)", cfg.LogLevel, defaultLogLevel)
	}
}

func TestLoadFromFiles_NullOnSecurityKeyIsRejected(t *testing.T) {
	t.Parallel()

	path := writeTempConfig(t, ".yaml", "jwt_secret: null\n")
	_, err := loadFromFile(path)
	if !errors.Is(err, ErrSecurityKeyNotNullable) {
		t.Fatalf("want ErrSecurityKeyNotNullable, got %v", err)
	}
	if !strings.Contains(err.Error(), "jwt_secret") {
		t.Errorf("error should name the offending key, got %q", err.Error())
	}
}

func TestIsNonNullableSecurityKey_ActiveSet(t *testing.T) {
	t.Parallel()

	// The currently-active key:
	if !isNonNullableSecurityKey("jwt_secret") {
		t.Error("isNonNullableSecurityKey(\"jwt_secret\") = false, want true")
	}
	// Sanity: a non-security key returns false.
	if isNonNullableSecurityKey("port") {
		t.Error("isNonNullableSecurityKey(\"port\") = true, want false")
	}
	// ADR-010 §14's prescriptive forward-compat names are
	// deliberately NOT in the active set today — see the godoc on
	// `defaultNonNullableSecurityKeys`. The PR that wires each
	// subsystem in adds the actual `koanf:"..."` tag to the set.
	for _, key := range []string{"cors.origins", "auth.providers", "authz.policy_path", "session.secret"} {
		if isNonNullableSecurityKey(key) {
			t.Errorf("isNonNullableSecurityKey(%q) = true; forward-compat keys should be added when their subsystem lands, not preemptively", key)
		}
	}
}

// --- Phase 2b: mixed-format detection ---

func TestLoadFromFiles_MixedFormatsDefaultAllowed(t *testing.T) {
	t.Parallel()

	// Without WithConfigStrict, mixed formats emit a WARN but do not
	// fail the load. The integration test verifies the load succeeds;
	// the WARN goes to slog.Default() which is the framework's
	// observability surface (Phase 2c will route this through the
	// router's logger).
	yamlPath := writeTempConfig(t, ".yaml", "port: 8080\n")
	tomlPath := writeTempConfig(t, ".toml", "host = \"127.0.0.1\"\n")
	cfg, err := loadFromFiles([]string{yamlPath, tomlPath}, configLoadOptions{strict: false})
	if err != nil {
		t.Fatalf("mixed-format load with strict=false should succeed, got %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: got %d want 8080", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host: got %q want %q", cfg.Host, "127.0.0.1")
	}
}

func TestLoadFromFiles_MixedFormatsStrictRejects(t *testing.T) {
	t.Parallel()

	yamlPath := writeTempConfig(t, ".yaml", "port: 8080\n")
	tomlPath := writeTempConfig(t, ".toml", "host = \"127.0.0.1\"\n")
	_, err := loadFromFiles([]string{yamlPath, tomlPath}, configLoadOptions{strict: true})
	if !errors.Is(err, ErrMixedConfigFormats) {
		t.Fatalf("want ErrMixedConfigFormats, got %v", err)
	}
}

func TestAppBuilder_WithConfigStrict_PropagatesToFromConfigFile(t *testing.T) {
	t.Parallel()

	yamlPath := writeTempConfig(t, ".yaml", "port: 8080\n")
	jsonPath := writeTempConfig(t, ".json", "{\"host\": \"127.0.0.1\"}\n")

	// Without strict — load should succeed.
	if _, err := New().FromConfigFile(yamlPath, jsonPath).Build(); err != nil {
		t.Fatalf("non-strict mixed-format load: unexpected error %v", err)
	}

	// With strict — load should reject.
	b := New().WithConfigStrict(true).FromConfigFile(yamlPath, jsonPath)
	if !errors.Is(b.Err(), ErrMixedConfigFormats) {
		t.Errorf("strict mixed-format load: want ErrMixedConfigFormats, got %v", b.Err())
	}
}

func TestAppBuilder_WithConfigStrict_AfterFromConfigFileIsRejected(t *testing.T) {
	t.Parallel()

	// Misorder guard (architect-reviewer Phase 2b): calling
	// WithConfigStrict AFTER FromConfigFile is silently ineffective
	// without a guard, because the strict flag is only consulted at
	// load time. The builder records a deferred error so the
	// misordered chain fails loud at Build().
	yamlPath := writeTempConfig(t, ".yaml", "port: 8080\n")
	b := New().FromConfigFile(yamlPath).WithConfigStrict(true)
	if b.Err() == nil {
		t.Fatal("expected misordered WithConfigStrict to record an error")
	}
	if !strings.Contains(b.Err().Error(), "before FromConfigFile") {
		t.Errorf("guard error should hint at the correct ordering, got %q", b.Err().Error())
	}
}

// --- Phase 2b: format-detection edge cases ---

func TestLoadFromFiles_UnknownExtensionRejected(t *testing.T) {
	t.Parallel()

	path := writeTempConfig(t, ".xml", "<config><port>80</port></config>\n")
	_, err := loadFromFiles([]string{path}, configLoadOptions{})
	if !errors.Is(err, ErrUnsupportedConfigFormat) {
		t.Errorf("want ErrUnsupportedConfigFormat, got %v", err)
	}
}

func TestLoadFromFiles_EmptyPathInList(t *testing.T) {
	t.Parallel()

	good := writeTempConfig(t, ".yaml", "port: 80\n")
	_, err := loadFromFiles([]string{good, ""}, configLoadOptions{})
	if err == nil {
		t.Fatal("expected error for empty path in list")
	}
	if !strings.Contains(err.Error(), "path[1]") {
		t.Errorf("error should identify the offending path index, got %q", err.Error())
	}
}

// equalStringSlice is a small helper that returns true when both
// slices contain the same elements in the same order. Used by the
// _append / _remove tests; kept package-local so test imports stay
// clean (no reflect.DeepEqual indirection for a one-line predicate).
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
