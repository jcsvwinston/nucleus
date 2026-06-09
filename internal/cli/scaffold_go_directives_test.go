package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScaffoldGoDirectivesTrackGoMod is the drift guard for audit CLI-V2-1.
// The go/toolchain directives the scaffolder bakes into generated projects
// (scaffoldGoVersion / scaffoldToolchain, surfaced via resolveGoDirectives)
// MUST mirror the framework's own go.mod. This reads go.mod at test time and
// fails if they diverge, so a future `go` or `toolchain` bump cannot silently
// leave the scaffold pinning a stale version — the exact drift that shipped a
// `toolchain go1.26.3` scaffold while the framework required 1.26.4.
func TestScaffoldGoDirectivesTrackGoMod(t *testing.T) {
	root := repoRootForTest(t)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read framework go.mod: %v", err)
	}

	var goVersion, toolchain string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "go "):
			goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		case strings.HasPrefix(line, "toolchain "):
			toolchain = strings.TrimSpace(strings.TrimPrefix(line, "toolchain "))
		}
	}
	if goVersion == "" {
		t.Fatal("framework go.mod has no `go` directive")
	}

	gv, tc := resolveGoDirectives()
	if gv != goVersion {
		t.Errorf("scaffold `go` directive %q does not match framework go.mod %q — bump scaffoldGoVersion (audit CLI-V2-1)", gv, goVersion)
	}
	if tc != toolchain {
		t.Errorf("scaffold `toolchain` directive %q does not match framework go.mod %q — update scaffoldToolchain", tc, toolchain)
	}
}
