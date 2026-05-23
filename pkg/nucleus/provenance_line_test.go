package nucleus

import (
	"os"
	"testing"
)

// writeTempFile writes body to a temp file named filename (extension drives
// the loader's format detection) and returns its path.
func writeTempFile(t *testing.T, filename, body string) string {
	t.Helper()
	path := t.TempDir() + "/" + filename
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	return path
}

// TestLoadEffective_YAMLLineProvenance checks that top-level YAML keys carry
// their 1-based source line (Phase 3.1).
func TestLoadEffective_YAMLLineProvenance(t *testing.T) {
	unsetEnv(t, "NUCLEUS_PORT")
	unsetEnv(t, "NUCLEUS_LOG_LEVEL")

	// port on line 1, log_level on line 2.
	path := writeTempFile(t, "nucleus.yaml", "port: 9001\nlog_level: debug\n")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	byKey := effectiveByKey(t, ec)

	port := byKey["port"]
	if port.Source.Kind != "yaml" || port.Source.Path != path || port.Source.Line != 1 {
		t.Errorf("port source = %+v; want yaml:%s:1", port.Source, path)
	}
	logLevel := byKey["log_level"]
	if logLevel.Source.Line != 2 {
		t.Errorf("log_level source line = %d; want 2 (source=%+v)", logLevel.Source.Line, logLevel.Source)
	}
}

// TestLoadEffective_NestedYAMLLine checks that a nested key reports the line
// of its leaf token.
func TestLoadEffective_NestedYAMLLine(t *testing.T) {
	// databases (1) / default (2) / url (3).
	body := "databases:\n  default:\n    url: sqlite://x.db\n"
	path := writeTempFile(t, "nucleus.yaml", body)

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	got, ok := effectiveByKey(t, ec)["databases.default.url"]
	if !ok {
		t.Fatal("expected databases.default.url")
	}
	if got.Source.Kind != "yaml" || got.Source.Line != 3 {
		t.Errorf("databases.default.url source = %+v; want yaml line 3", got.Source)
	}
}

// TestLoadEffective_TOMLNoLine confirms TOML files report kind+path with no
// line (positions only available via go-toml's unstable API — out of scope).
func TestLoadEffective_TOMLNoLine(t *testing.T) {
	unsetEnv(t, "NUCLEUS_PORT")
	path := writeTempFile(t, "nucleus.toml", "port = 9002\n")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	port := effectiveByKey(t, ec)["port"]
	if port.Source.Kind != "toml" || port.Source.Path != path {
		t.Errorf("port source = %+v; want toml:%s", port.Source, path)
	}
	if port.Source.Line != 0 {
		t.Errorf("toml source must not report a line; got %d", port.Source.Line)
	}
}

// TestLoadEffective_JSONNoLine confirms JSON files report kind+path with no
// line (no standard line API).
func TestLoadEffective_JSONNoLine(t *testing.T) {
	unsetEnv(t, "NUCLEUS_PORT")
	path := writeTempFile(t, "nucleus.json", "{\"port\": 9003}\n")

	ec, err := LoadEffective([]string{path})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	port := effectiveByKey(t, ec)["port"]
	if port.Source.Kind != "json" || port.Source.Line != 0 {
		t.Errorf("json source = %+v; want json with no line", port.Source)
	}
}

// TestYAMLLineMap_Direct exercises the walker on a small document including a
// list value (recorded under its key) and a malformed input (best-effort nil).
func TestYAMLLineMap_Direct(t *testing.T) {
	m := yamlLineMap([]byte("port: 1\ncors_origins:\n  - https://a\n  - https://b\n"))
	if m["port"] != 1 {
		t.Errorf("port line = %d; want 1", m["port"])
	}
	if m["cors_origins"] != 2 {
		t.Errorf("cors_origins line = %d; want 2", m["cors_origins"])
	}
	if got := yamlLineMap([]byte(": : : not yaml")); got != nil {
		t.Errorf("malformed YAML should yield nil line map, got %v", got)
	}
}
