package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// NF-10: doctor's storage check used to answer every remote provider with
// "run a provider-specific health check" — a check that existed nowhere.
// Now `doctor --check storage` IS that check: it builds the real store from
// the effective config and performs one authenticated List. The full run
// (live=false) stays offline and says how to go further.

func s3Config(t *testing.T) *app.Config {
	t.Helper()
	defaults := app.DefaultConfig()
	cfg := &defaults
	cfg.Storage.Provider = "s3"
	cfg.Storage.S3.Bucket = "some-bucket"
	cfg.Storage.S3.Region = "us-east-1"
	return cfg
}

func TestCheckStorageOfflineRunPointsAtLiveProbe(t *testing.T) {
	outcome := checkStorage(s3Config(t), "", false)
	if outcome.status != doctorStatusWarning {
		t.Fatalf("offline s3 check status = %s; want warning", outcome.status)
	}
	if !strings.Contains(outcome.message, "--check storage") {
		t.Fatalf("offline warning does not point at the live probe: %s", outcome.message)
	}
	if strings.Contains(outcome.message, "provider-specific health check") {
		t.Fatalf("offline warning still delegates to a nonexistent external check: %s", outcome.message)
	}
}

func TestCheckStorageLiveProbeReportsRealFailure(t *testing.T) {
	cfg := s3Config(t)
	// An endpoint that cannot even be parsed into a client: the probe must
	// surface the store-construction failure as an error, not a warning.
	cfg.Storage.S3.Endpoint = "http://invalid endpoint with spaces"
	outcome := checkStorage(cfg, "", true)
	if outcome.status != doctorStatusError {
		t.Fatalf("live probe with broken endpoint status = %s (%s); want error", outcome.status, outcome.message)
	}
}

func TestCheckStorageLocalStillOffline(t *testing.T) {
	defaults := app.DefaultConfig()
	cfg := &defaults
	cfg.Storage.Provider = "local"
	cfg.Storage.Local.Path = t.TempDir()
	outcome := checkStorage(cfg, "", true)
	if outcome.status != doctorStatusPass {
		t.Fatalf("local storage check status = %s (%s); want pass", outcome.status, outcome.message)
	}
}

// NU-99 in doctor: a project whose composition root builds the application
// WithoutDefaults() without WithStorage(), and whose configuration declares
// storage, ignores the block at boot — doctor says so, as a warning (the
// application starts) that names the option and the deprecation notice.
func TestCheckStorageReportsABlockTheApplicationIgnores(t *testing.T) {
	const chain = "package main\n\nimport \"github.com/jcsvwinston/nucleus/pkg/nucleus\"\n\n" +
		"// nucleus.New().WithoutDefaults().WithStorage() — a doc comment does not count.\n" +
		"func main() {\n\t_ = nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults()%s.Start()\n}\n"
	for _, tc := range []struct {
		name     string
		main     string
		declared bool
		ignored  bool
	}{
		{name: "WithoutDefaults, no WithStorage, storage declared", main: fmt.Sprintf(chain, ""), declared: true, ignored: true},
		{name: "WithoutDefaults and WithStorage", main: fmt.Sprintf(chain, ".WithStorage()"), declared: true},
		{name: "nothing declared", main: fmt.Sprintf(chain, ""), declared: false},
		{name: "the app.New spelling", declared: true, ignored: true,
			main: "package main\n\nimport \"github.com/jcsvwinston/nucleus/pkg/app\"\n\nfunc main() {\n\tcfg, _ := app.LoadConfig()\n\t_, _ = app.New(cfg, app.WithoutDefaults())\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(tc.main), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := s3Config(t)
			cfg.StorageDeclared = tc.declared
			outcome := checkStorage(cfg, filepath.Join(dir, "nucleus.yml"), false)
			said := strings.Contains(outcome.message, "IGNORED")
			if said != tc.ignored {
				t.Fatalf("doctor reported the block ignored = %v, want %v: %s", said, tc.ignored, outcome.message)
			}
			if !tc.ignored {
				return
			}
			if outcome.status != doctorStatusWarning {
				t.Errorf("status = %s; want warning — the application starts", outcome.status)
			}
			for _, want := range []string{"WithStorage()", "DEP-2026-013", "v2.0.0", "main.go"} {
				if !strings.Contains(outcome.message, want) {
					t.Errorf("the report does not say %q: %s", want, outcome.message)
				}
			}
		})
	}
}

// The notice doctor and the boot log cite exists in the register.
func TestStorageIgnoredNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join(repoRootForTest(t), "docs", "deprecations", "DEP-2026-013-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/DEP-2026-013-*.md: found %v, want exactly one", matches)
	}
}
