package nucleus

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// writeTempYAML writes body to a temp .yaml file and returns its path.
func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/nucleus.yaml"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// unsetEnv removes name for the duration of the test and restores any prior
// value on cleanup. Use it when a test must assert behaviour with a given
// NUCLEUS_ variable absent, regardless of the ambient shell/CI environment.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	if orig, ok := os.LookupEnv(name); ok {
		t.Cleanup(func() { _ = os.Setenv(name, orig) })
	}
	_ = os.Unsetenv(name)
}

func effectiveByKey(t *testing.T, ec EffectiveConfig) map[string]EffectiveValue {
	t.Helper()
	m := make(map[string]EffectiveValue, len(ec.Values))
	for _, v := range ec.Values {
		m[v.Key] = v
	}
	return m
}

// TestLoadEffective_EnvOverridesFile is the core Phase 3.1 assertion: a
// NUCLEUS_-prefixed env var overrides the file value AND is attributed to the
// env layer with the originating variable name as the Path.
func TestLoadEffective_EnvOverridesFile(t *testing.T) {
	path := writeTempYAML(t, "port: 8080\n")
	t.Setenv("NUCLEUS_PORT", "9090")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	got := effectiveByKey(t, ec)["port"]
	if got.Source.Kind != sourceKindEnv {
		t.Errorf("port source kind = %q; want %q", got.Source.Kind, sourceKindEnv)
	}
	if got.Source.Path != "NUCLEUS_PORT" {
		t.Errorf("port source path = %q; want NUCLEUS_PORT", got.Source.Path)
	}
	// Env value (string) must have replaced the file's value.
	if got.Value != "9090" {
		t.Errorf("port value = %v (%T); want \"9090\"", got.Value, got.Value)
	}
}

// TestLoadEffective_EnvNestedKey covers the double-underscore → dot transform
// for a nested, schema-valid key (databases.<alias>.url).
func TestLoadEffective_EnvNestedKey(t *testing.T) {
	path := writeTempYAML(t, "port: 8080\n")
	t.Setenv("NUCLEUS_DATABASES__ANALYTICS__URL", "sqlite://analytics.db")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	got, ok := effectiveByKey(t, ec)["databases.analytics.url"]
	if !ok {
		t.Fatal("expected databases.analytics.url from env")
	}
	if got.Source.Kind != sourceKindEnv || got.Source.Path != "NUCLEUS_DATABASES__ANALYTICS__URL" {
		t.Errorf("source = %+v; want env:NUCLEUS_DATABASES__ANALYTICS__URL", got.Source)
	}
	// databases.<alias>.url is a redaction target (embeds credentials).
	if !got.Redacted {
		t.Errorf("databases.analytics.url should be redacted")
	}
}

// TestLoadEffective_UnknownEnvVarIgnored verifies an unrecognised
// NUCLEUS_-prefixed variable neither errors nor appears in the snapshot.
func TestLoadEffective_UnknownEnvVarIgnored(t *testing.T) {
	path := writeTempYAML(t, "port: 8080\n")
	t.Setenv("NUCLEUS_TOTALLY_BOGUS", "nope")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective should ignore unknown env vars, got: %v", err)
	}
	// NUCLEUS_TOTALLY_BOGUS has no `__`, so it transforms to the leaf
	// `totally_bogus` — which matches no schema key and must be dropped.
	for _, v := range ec.Values {
		if v.Key == "totally_bogus" {
			t.Fatalf("unknown env var leaked into effective config as %q", v.Key)
		}
	}
}

// TestLoadFromFiles_EnvApplied is the behavioural half: env overrides must
// flow through to the typed *app.Config (string env coerced to int Port),
// closing the ADR §4 precedence gap in the FromConfigFile path.
func TestLoadFromFiles_EnvApplied(t *testing.T) {
	path := writeTempYAML(t, "port: 8080\n")
	t.Setenv("NUCLEUS_PORT", "9090")

	cfg, err := loadFromFiles([]string{path}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFiles: %v", err)
	}
	if cfg.Port != 9090 {
		t.Fatalf("env override did not apply: cfg.Port = %d; want 9090", cfg.Port)
	}
}

// TestLoadEffective_NoEnvKeepsFileProvenance is a guard: with no NUCLEUS_ env
// set, file-sourced keys keep their file provenance (env layer is a no-op).
func TestLoadEffective_NoEnvKeepsFileProvenance(t *testing.T) {
	// Ensure no ambient NUCLEUS_PORT bleeds in from the dev shell / CI.
	unsetEnv(t, "NUCLEUS_PORT")

	path := writeTempYAML(t, "port: 8081\n")
	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	got := effectiveByKey(t, ec)["port"]
	if got.Source.Kind != "yaml" {
		t.Errorf("with no env, port source kind = %q; want yaml", got.Source.Kind)
	}
}

// TestLoadEffective_EnvSecretRedacted guards that an env-sourced secret is
// redacted in the effective view exactly like a file-sourced one — the "env"
// source kind must not bypass redaction. Path carries only the variable NAME.
func TestLoadEffective_EnvSecretRedacted(t *testing.T) {
	path := writeTempYAML(t, "port: 8080\n")
	t.Setenv("NUCLEUS_JWT_SECRET", "super-signing-key-from-env")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	got, ok := effectiveByKey(t, ec)["jwt_secret"]
	if !ok {
		t.Fatal("expected jwt_secret in effective config when set via env")
	}
	if got.Source.Kind != sourceKindEnv || got.Source.Path != "NUCLEUS_JWT_SECRET" {
		t.Errorf("source = %+v; want env:NUCLEUS_JWT_SECRET", got.Source)
	}
	if !got.Redacted || got.Value != observe.RedactionPlaceholder {
		t.Errorf("env-sourced jwt_secret must be redacted; got redacted=%v value=%v", got.Redacted, got.Value)
	}
}

// TestLoadEffective_EmptyEnvSecurityKeyRejected guards the security fix: an
// empty NUCLEUS_JWT_SECRET must not silently overwrite a file-set secret and
// disable signing — it is a boot error, mirroring the file-layer null guard.
func TestLoadEffective_EmptyEnvSecurityKeyRejected(t *testing.T) {
	path := writeTempYAML(t, "jwt_secret: file-set-secret\n")
	t.Setenv("NUCLEUS_JWT_SECRET", "   ") // whitespace-only counts as empty

	_, err := LoadEffective([]string{path})
	if err == nil {
		t.Fatal("expected an error for an empty NUCLEUS_JWT_SECRET, got nil")
	}
	if !errors.Is(err, ErrSecurityKeyNotNullable) {
		t.Errorf("error = %v; want ErrSecurityKeyNotNullable", err)
	}
}

// TestLoadFromFiles_EnvCoercionError verifies a non-coercible env value for a
// typed key surfaces an actionable Unmarshal error rather than a silent drop.
func TestLoadFromFiles_EnvCoercionError(t *testing.T) {
	path := writeTempYAML(t, "port: 8080\n")
	t.Setenv("NUCLEUS_PORT", "not-a-number")

	_, err := loadFromFiles([]string{path}, configLoadOptions{})
	if err == nil {
		t.Fatal("expected an unmarshal error for NUCLEUS_PORT=not-a-number, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error %q should mention the unmarshal failure", err.Error())
	}
}
