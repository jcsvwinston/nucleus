package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestRunConfigPrint_TextWithSources(t *testing.T) {
	cfg := writeConfigFile(t, "nucleus.yaml", "port: 8099\nhost: 192.0.2.7\n")

	var out, errb bytes.Buffer
	if err := runConfigPrint([]string{"--effective", "--config", cfg}, nil, &out, &errb); err != nil {
		t.Fatalf("runConfigPrint: %v (stderr=%s)", err, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "port = 8099 [yaml:"+cfg+"]") {
		t.Errorf("missing port line with file source; got:\n%s", s)
	}
	if !strings.Contains(s, "host = 192.0.2.7 [yaml:"+cfg+"]") {
		t.Errorf("missing host line with file source; got:\n%s", s)
	}
	if !strings.Contains(s, "[default]") {
		t.Errorf("expected at least one default-sourced key; got:\n%s", s)
	}
}

func TestRunConfigPrint_JSON(t *testing.T) {
	cfg := writeConfigFile(t, "nucleus.yaml", "port: 8099\n")

	var out, errb bytes.Buffer
	if err := runConfigPrint([]string{"--config", cfg, "--json"}, nil, &out, &errb); err != nil {
		t.Fatalf("runConfigPrint: %v", err)
	}

	var decoded struct {
		Values []struct {
			Key    string `json:"key"`
			Source struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"source"`
		} `json:"values"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out.String())
	}
	var found bool
	for _, v := range decoded.Values {
		if v.Key == "port" {
			found = true
			if v.Source.Kind != "yaml" || v.Source.Path != cfg {
				t.Errorf("port source: got %+v want {yaml %s}", v.Source, cfg)
			}
		}
	}
	if !found {
		t.Errorf("port key absent from JSON output")
	}
}

func TestRunConfigPrint_RedactsDatabaseURL(t *testing.T) {
	cfg := writeConfigFile(t, "nucleus.yaml", "databases:\n  default:\n    url: postgres://user:topsecret@db/app\n")

	var out, errb bytes.Buffer
	if err := runConfigPrint([]string{"--config", cfg}, nil, &out, &errb); err != nil {
		t.Fatalf("runConfigPrint: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "[REDACTED]") {
		t.Errorf("expected redacted DB URL; got:\n%s", s)
	}
	if strings.Contains(s, "topsecret") {
		t.Errorf("secret leaked in output:\n%s", s)
	}
}

func TestRunConfigPrint_RequiresConfig(t *testing.T) {
	var out, errb bytes.Buffer
	if err := runConfigPrint([]string{"--effective"}, nil, &out, &errb); err == nil {
		t.Fatal("expected an error when no --config path is given")
	}
}

func TestRunConfig_UnknownSubcommand(t *testing.T) {
	var out, errb bytes.Buffer
	if err := runConfig([]string{"bogus"}, nil, &out, &errb); err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if err := runConfig(nil, nil, &out, &errb); err == nil {
		t.Fatal("expected an error when no subcommand is given")
	}
}
