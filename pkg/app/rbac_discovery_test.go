package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRBACPolicyPath_DiscoversScaffoldedName covers R5 (ADR-013): an app that
// relies on auto-discovery finds the rbac_policy.csv name emitted by the mvc
// scaffold without having to set rbac_policy_file.
func TestRBACPolicyPath_DiscoversScaffoldedName(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "rbac_policy.csv")

	if got, _ := rbacPolicyPath(&Config{}); got != "rbac_policy.csv" {
		t.Fatalf("rbacPolicyPath = %q, want rbac_policy.csv (auto-discovered)", got)
	}
}

// TestRBACPolicyPath_DiscoversConfigSubdir confirms the config/ variant of the
// scaffolded name is probed too.
func TestRBACPolicyPath_DiscoversConfigSubdir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("config", 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	writePolicy(t, filepath.Join("config", "rbac_policy.csv"))

	if got, _ := rbacPolicyPath(&Config{}); got != "config/rbac_policy.csv" {
		t.Fatalf("rbacPolicyPath = %q, want config/rbac_policy.csv", got)
	}
}

// TestRBACPolicyPath_ExplicitWins confirms an explicit rbac_policy_file
// takes precedence over auto-discovery and is returned verbatim when it exists.
func TestRBACPolicyPath_ExplicitWins(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "custom.csv")
	// A discoverable default also present, to prove the explicit path wins.
	writePolicy(t, "rbac_policy.csv")

	if got, _ := rbacPolicyPath(&Config{RBACPolicyFile: "custom.csv"}); got != "custom.csv" {
		t.Fatalf("rbacPolicyPath = %q, want custom.csv (explicit)", got)
	}
}

// TestRBACPolicyPath_MissingExplicitFails confirms an explicit path that
// does not exist on disk is an ERROR naming the path (DX-2 corollary) —
// the old "" return booted the app into total default-deny with a WARN
// pointing at the key the operator had already set.
func TestRBACPolicyPath_MissingExplicitFails(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := rbacPolicyPath(&Config{RBACPolicyFile: "nope.csv"})
	if err == nil {
		t.Fatalf("rbacPolicyPath for a missing explicit file must fail, got %q", got)
	}
	if !strings.Contains(err.Error(), "nope.csv") {
		t.Fatalf("the error must name the missing file, got: %v", err)
	}
}

// TestRBACPolicyPath_NoneReturnsEmpty confirms an empty working directory with no
// policy and no explicit path yields "".
func TestRBACPolicyPath_NoneReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got, _ := rbacPolicyPath(&Config{}); got != "" {
		t.Fatalf("rbacPolicyPath in an empty dir = %q, want empty", got)
	}
}

// TestResolveRBACPolicyFile_CanonicalKey confirms the canonical key resolves
// (the deprecated admin_rbac_policy_file alias was removed in v0.12.0,
// DEP-2026-004 — the canonical key is the only source).
func TestResolveRBACPolicyFile_CanonicalKey(t *testing.T) {
	if got := resolveRBACPolicyFile(&Config{RBACPolicyFile: "canonical.csv"}); got != "canonical.csv" {
		t.Fatalf("resolveRBACPolicyFile = %q, want canonical.csv", got)
	}
	if got := resolveRBACPolicyFile(nil); got != "" {
		t.Fatalf("resolveRBACPolicyFile(nil) = %q, want empty", got)
	}
}

func writePolicy(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("p, admin, /, GET\n"), 0o644); err != nil {
		t.Fatalf("write policy %s: %v", path, err)
	}
}
