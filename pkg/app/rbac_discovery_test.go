package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRBACPolicyPath_DiscoversScaffoldedName covers R5 (ADR-013): an app that
// relies on auto-discovery finds the rbac_policy.csv name emitted by the mvc
// scaffold without having to set admin_rbac_policy_file.
func TestRBACPolicyPath_DiscoversScaffoldedName(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "rbac_policy.csv")

	if got := rbacPolicyPath(&Config{}); got != "rbac_policy.csv" {
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

	if got := rbacPolicyPath(&Config{}); got != "config/rbac_policy.csv" {
		t.Fatalf("rbacPolicyPath = %q, want config/rbac_policy.csv", got)
	}
}

// TestRBACPolicyPath_ExplicitWins confirms an explicit admin_rbac_policy_file
// takes precedence over auto-discovery and is returned verbatim when it exists.
func TestRBACPolicyPath_ExplicitWins(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "custom.csv")
	// A discoverable default also present, to prove the explicit path wins.
	writePolicy(t, "rbac_policy.csv")

	if got := rbacPolicyPath(&Config{AdminRBACPolicyFile: "custom.csv"}); got != "custom.csv" {
		t.Fatalf("rbacPolicyPath = %q, want custom.csv (explicit)", got)
	}
}

// TestRBACPolicyPath_MissingExplicitReturnsEmpty confirms an explicit path that
// does not exist on disk yields "" rather than the dangling path.
func TestRBACPolicyPath_MissingExplicitReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := rbacPolicyPath(&Config{AdminRBACPolicyFile: "nope.csv"}); got != "" {
		t.Fatalf("rbacPolicyPath for a missing explicit file = %q, want empty", got)
	}
}

// TestRBACPolicyPath_NoneReturnsEmpty confirms an empty working directory with no
// policy and no explicit path yields "".
func TestRBACPolicyPath_NoneReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := rbacPolicyPath(&Config{}); got != "" {
		t.Fatalf("rbacPolicyPath in an empty dir = %q, want empty", got)
	}
}

func writePolicy(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("p, admin, /, GET\n"), 0o644); err != nil {
		t.Fatalf("write policy %s: %v", path, err)
	}
}
