package nucleus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
)

// billingConfig is a representative module config exercising every layer-5
// concern: a required field, a oneof-validated enum with a default, a numeric
// default, a duration default, and a bool default.
type billingConfig struct {
	StripeKey string        `koanf:"stripe_key" validate:"required"`
	Tier      string        `koanf:"tier" default:"standard" validate:"oneof=standard premium"`
	Retries   int           `koanf:"retries" default:"3"`
	Timeout   time.Duration `koanf:"timeout" default:"30s"`
	Enabled   bool          `koanf:"enabled" default:"true"`
}

func billingModule(cfg billingConfig) ModuleSpec {
	return Module[billingConfig]{Name: "billing", Config: cfg}.Build()
}

func mustBinder(t *testing.T, spec ModuleSpec) moduleConfigBinder {
	t.Helper()
	b, ok := spec.(moduleConfigBinder)
	if !ok {
		t.Fatalf("module spec %T does not implement moduleConfigBinder", spec)
	}
	return b
}

func writeConfig(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestBindModuleConfig_FileOverlayThenDefaults: file values win over the author
// baseline and over default: tags; default: tags fill only the still-zero
// fields; the author-set field the file does not touch is preserved.
func TestBindModuleConfig_FileOverlayThenDefaults(t *testing.T) {
	mk := koanf.New(".")
	if err := mk.Set("tier", "premium"); err != nil {
		t.Fatal(err)
	}
	if err := mk.Set("retries", 5); err != nil {
		t.Fatal(err)
	}

	bound, err := mustBinder(t, billingModule(billingConfig{StripeKey: "sk_live"})).bindConfig(mk)
	if err != nil {
		t.Fatalf("bindConfig: %v", err)
	}
	cfg := bound.Config().(billingConfig)

	if cfg.StripeKey != "sk_live" {
		t.Errorf("StripeKey: author value not preserved, got %q", cfg.StripeKey)
	}
	if cfg.Tier != "premium" {
		t.Errorf("Tier: file value should win, got %q", cfg.Tier)
	}
	if cfg.Retries != 5 {
		t.Errorf("Retries: file value should win over default, got %d", cfg.Retries)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout: default should fill, got %v", cfg.Timeout)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled: default true should fill, got %v", cfg.Enabled)
	}
}

// TestBindModuleConfig_DefaultsOnly: the direct-struct surface (raw == nil)
// applies defaults and validation but no file overlay.
func TestBindModuleConfig_DefaultsOnly(t *testing.T) {
	bound, err := mustBinder(t, billingModule(billingConfig{StripeKey: "sk_live"})).bindConfig(nil)
	if err != nil {
		t.Fatalf("bindConfig(nil): %v", err)
	}
	cfg := bound.Config().(billingConfig)

	if cfg.Tier != "standard" || cfg.Retries != 3 || cfg.Timeout != 30*time.Second || !cfg.Enabled {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

// TestBindModuleConfig_RequiredFails: a missing required field surfaces
// ErrInvalidModuleConfig.
func TestBindModuleConfig_RequiredFails(t *testing.T) {
	_, err := mustBinder(t, billingModule(billingConfig{})).bindConfig(nil)
	if !errors.Is(err, ErrInvalidModuleConfig) {
		t.Fatalf("want ErrInvalidModuleConfig, got %v", err)
	}
}

// TestBindModuleConfig_OneofFails: a file value that violates a oneof rule
// surfaces ErrInvalidModuleConfig (and the default does not mask it).
func TestBindModuleConfig_OneofFails(t *testing.T) {
	mk := koanf.New(".")
	if err := mk.Set("tier", "enterprise"); err != nil {
		t.Fatal(err)
	}
	_, err := mustBinder(t, billingModule(billingConfig{StripeKey: "sk"})).bindConfig(mk)
	if !errors.Is(err, ErrInvalidModuleConfig) {
		t.Fatalf("want ErrInvalidModuleConfig, got %v", err)
	}
}

// TestBindModuleConfigs_BindsAndIgnoresUnmounted: the orchestrator binds mounted
// modules and ignores (does not error on) config for an unmounted module.
func TestBindModuleConfigs_BindsAndIgnoresUnmounted(t *testing.T) {
	mkBilling := koanf.New(".")
	if err := mkBilling.Set("tier", "premium"); err != nil {
		t.Fatal(err)
	}
	mkGhost := koanf.New(".")
	if err := mkGhost.Set("foo", "bar"); err != nil {
		t.Fatal(err)
	}

	a := App{
		Modules:          map[string]ModuleSpec{"billing": billingModule(billingConfig{StripeKey: "sk"})},
		moduleConfigsRaw: map[string]*koanf.Koanf{"billing": mkBilling, "ghost": mkGhost},
	}
	if err := bindModuleConfigs(&a); err != nil {
		t.Fatalf("bindModuleConfigs: %v", err)
	}
	cfg := a.Modules["billing"].Config().(billingConfig)
	if cfg.Tier != "premium" {
		t.Errorf("billing not bound from file, got tier %q", cfg.Tier)
	}
	if _, ok := a.Modules["ghost"]; ok {
		t.Errorf("unmounted module should not have been added to Modules")
	}
}

// TestBindModuleConfigs_NonStructConfigSkipsValidation: a non-struct module
// config (struct{}, map) carries no tags and must not trip the validator.
func TestBindModuleConfigs_NonStructConfigSkipsValidation(t *testing.T) {
	spec := Module[struct{}]{Name: "noop", Config: struct{}{}}.Build()
	bound, err := mustBinder(t, spec).bindConfig(nil)
	if err != nil {
		t.Fatalf("empty struct config should bind cleanly, got %v", err)
	}
	if _, ok := bound.Config().(struct{}); !ok {
		t.Errorf("config type changed: %T", bound.Config())
	}
}

type defaultsAll struct {
	S      string        `default:"hi"`
	B      bool          `default:"true"`
	I      int           `default:"-7"`
	U      uint          `default:"42"`
	F      float64       `default:"1.5"`
	D      time.Duration `default:"2m"`
	Nested struct {
		X string `default:"deep"`
	}
	NoTag  string
	Preset string `default:"x"`
}

// TestApplyDefaults_FillsZerosOnly: every supported scalar kind, time.Duration,
// and a nested struct are filled from default: tags — but only when zero, so a
// preset value is preserved and an untagged field is left alone.
func TestApplyDefaults_FillsZerosOnly(t *testing.T) {
	v := defaultsAll{Preset: "kept"}
	if err := applyDefaults(&v); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	switch {
	case v.S != "hi":
		t.Errorf("S = %q", v.S)
	case v.B != true:
		t.Errorf("B = %v", v.B)
	case v.I != -7:
		t.Errorf("I = %d", v.I)
	case v.U != 42:
		t.Errorf("U = %d", v.U)
	case v.F != 1.5:
		t.Errorf("F = %v", v.F)
	case v.D != 2*time.Minute:
		t.Errorf("D = %v", v.D)
	case v.Nested.X != "deep":
		t.Errorf("Nested.X = %q", v.Nested.X)
	case v.NoTag != "":
		t.Errorf("NoTag should stay empty, got %q", v.NoTag)
	case v.Preset != "kept":
		t.Errorf("Preset should be preserved, got %q", v.Preset)
	}
}

// TestApplyDefaults_NonNilPointerStruct: defaults recurse into a non-nil
// pointer-to-struct field.
func TestApplyDefaults_NonNilPointerStruct(t *testing.T) {
	type sub struct {
		X string `default:"deep"`
	}
	type withPtr struct {
		Sub *sub
	}
	v := withPtr{Sub: &sub{}}
	if err := applyDefaults(&v); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if v.Sub.X != "deep" {
		t.Errorf("pointer-to-struct default not applied, got %q", v.Sub.X)
	}
}

// TestApplyDefaults_UnsupportedKind: a default: tag on a kind the helper cannot
// parse (slice) fails loud rather than silently.
func TestApplyDefaults_UnsupportedKind(t *testing.T) {
	type bad struct {
		Slc []string `default:"x"`
	}
	var v bad
	if err := applyDefaults(&v); err == nil {
		t.Fatal("want error for default: tag on a slice field, got nil")
	}
}

// TestApplyDefaults_InvalidTag: an unparseable default fails loud at boot.
func TestApplyDefaults_InvalidTag(t *testing.T) {
	type bad struct {
		D time.Duration `default:"notaduration"`
	}
	var v bad
	if err := applyDefaults(&v); err == nil {
		t.Fatal("want error for invalid duration default, got nil")
	}
}

// TestApplyDefaults_NonPointerNoop: a non-pointer (or nil pointer) argument is a
// safe no-op.
func TestApplyDefaults_NonPointerNoop(t *testing.T) {
	if err := applyDefaults(defaultsAll{}); err != nil {
		t.Fatalf("non-pointer should no-op, got %v", err)
	}
	var p *defaultsAll
	if err := applyDefaults(p); err != nil {
		t.Fatalf("nil pointer should no-op, got %v", err)
	}
}

// TestLoadFromFilesWithModules_AcceptsModulesNamespace: a config file with a
// modules.* block is no longer rejected as unknown, the app keys still bind, and
// the per-module subtree is extracted rooted at the module name.
func TestLoadFromFilesWithModules_AcceptsModulesNamespace(t *testing.T) {
	body := "port: 9000\nmodules:\n  billing:\n    tier: premium\n    stripe_key: sk_live\n"
	path := writeConfig(t, "cfg.yaml", body)

	cfg, mods, err := loadFromFilesWithModules([]string{path}, configLoadOptions{})
	if err != nil {
		t.Fatalf("loadFromFilesWithModules: %v", err)
	}
	if cfg.Port != 9000 {
		t.Errorf("app key not bound: port = %d", cfg.Port)
	}
	mk := mods["billing"]
	if mk == nil {
		t.Fatalf("billing module subtree not extracted; got %v", mods)
	}
	if got := mk.String("tier"); got != "premium" {
		t.Errorf("module subtree key tier = %q", got)
	}
	if got := mk.String("stripe_key"); got != "sk_live" {
		t.Errorf("module subtree key stripe_key = %q", got)
	}
}

// TestLoadFromFiles_StillRejectsRealUnknownKey: the modules.* exemption must not
// weaken the unknown-key guard for genuinely unrecognised top-level keys.
func TestLoadFromFiles_StillRejectsRealUnknownKey(t *testing.T) {
	body := "port: 9000\nbogus_top_key: x\n"
	path := writeConfig(t, "cfg.yaml", body)

	if _, err := loadFromFiles([]string{path}, configLoadOptions{}); err == nil {
		t.Fatal("want unknown-key rejection for bogus_top_key, got nil")
	}
}

// TestLoadFromFiles_ModulesPlusBogusKeyStillRejects: the modules.* exemption
// must strip ONLY module keys — a bogus top-level key coexisting with a valid
// modules.* block is still rejected.
func TestLoadFromFiles_ModulesPlusBogusKeyStillRejects(t *testing.T) {
	body := "port: 9000\nmodules:\n  billing:\n    tier: premium\nbogus_top_key: x\n"
	path := writeConfig(t, "cfg.yaml", body)

	if _, err := loadFromFiles([]string{path}, configLoadOptions{}); err == nil {
		t.Fatal("want rejection for bogus_top_key alongside a modules.* block, got nil")
	}
}

// TestLoadEffective_ExcludesModulesNamespace: module config (which has no
// redaction contract) must never appear in the effective-config snapshot that
// backs the admin GET /_/config endpoint, even in cleartext.
func TestLoadEffective_ExcludesModulesNamespace(t *testing.T) {
	body := "port: 9000\nmodules:\n  billing:\n    stripe_key: sk_secret\n    tier: premium\n"
	path := writeConfig(t, "cfg.yaml", body)

	eff, err := loadEffective([]string{path}, configLoadOptions{}, nil)
	if err != nil {
		t.Fatalf("loadEffective: %v", err)
	}
	var sawPort bool
	for _, v := range eff.Values {
		if strings.HasPrefix(v.Key, "modules") {
			t.Errorf("effective snapshot leaked module key %q (value %v)", v.Key, v.Value)
		}
		if v.Key == "port" {
			sawPort = true
		}
	}
	if !sawPort {
		t.Error("expected app key 'port' to remain in the effective snapshot")
	}
}

// TestFromConfigFile_ModuleConfigBinding: the builder path accepts a modules.*
// block and, after Build + bindModuleConfigs, the mounted module observes the
// file-bound config.
func TestFromConfigFile_ModuleConfigBinding(t *testing.T) {
	body := "modules:\n  billing:\n    tier: premium\n    stripe_key: sk_live\n    timeout: 45s\n"
	path := writeConfig(t, "cfg.yaml", body)

	a, err := New().Mount(billingModule(billingConfig{})).FromConfigFile(path).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := bindModuleConfigs(&a); err != nil {
		t.Fatalf("bindModuleConfigs: %v", err)
	}
	cfg := a.Modules["billing"].Config().(billingConfig)
	if cfg.Tier != "premium" || cfg.StripeKey != "sk_live" {
		t.Errorf("module config not bound from file: %+v", cfg)
	}
	if cfg.Timeout != 45*time.Second {
		t.Errorf("duration not decoded from file string: %v", cfg.Timeout)
	}
	if cfg.Retries != 3 {
		t.Errorf("default not applied on builder path: retries = %d", cfg.Retries)
	}
}
