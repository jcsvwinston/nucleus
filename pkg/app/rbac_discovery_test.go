package app

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestRBACPolicyPath_DiscoversScaffoldedName covers R5 (ADR-013): an app that
// relies on auto-discovery finds the rbac_policy.csv name emitted by the mvc
// scaffold without having to set rbac_policy_file.
func TestRBACPolicyPath_DiscoversScaffoldedName(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "rbac_policy.csv")

	if got := rbacPolicyPath(nil, &Config{}); got != "rbac_policy.csv" {
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

	if got := rbacPolicyPath(nil, &Config{}); got != "config/rbac_policy.csv" {
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

	if got := rbacPolicyPath(nil, &Config{RBACPolicyFile: "custom.csv"}); got != "custom.csv" {
		t.Fatalf("rbacPolicyPath = %q, want custom.csv (explicit)", got)
	}
}

// TestRBACPolicyPath_DeprecatedAliasStillResolves confirms the deprecated
// admin_rbac_policy_file alias is still honoured when rbac_policy_file is empty.
func TestRBACPolicyPath_DeprecatedAliasStillResolves(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "legacy.csv")

	if got := rbacPolicyPath(nil, &Config{AdminRBACPolicyFile: "legacy.csv"}); got != "legacy.csv" {
		t.Fatalf("rbacPolicyPath = %q, want legacy.csv (deprecated alias)", got)
	}
}

// TestRBACPolicyPath_CanonicalKeyWinsOverDeprecatedAlias confirms rbac_policy_file
// takes precedence over the deprecated admin_rbac_policy_file alias.
func TestRBACPolicyPath_CanonicalKeyWinsOverDeprecatedAlias(t *testing.T) {
	t.Chdir(t.TempDir())
	writePolicy(t, "canonical.csv")
	writePolicy(t, "legacy.csv")

	cfg := &Config{RBACPolicyFile: "canonical.csv", AdminRBACPolicyFile: "legacy.csv"}
	if got := rbacPolicyPath(nil, cfg); got != "canonical.csv" {
		t.Fatalf("rbacPolicyPath = %q, want canonical.csv (canonical key wins)", got)
	}
}

// TestRBACPolicyPath_MissingExplicitReturnsEmpty confirms an explicit path that
// does not exist on disk yields "" rather than the dangling path.
func TestRBACPolicyPath_MissingExplicitReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := rbacPolicyPath(nil, &Config{RBACPolicyFile: "nope.csv"}); got != "" {
		t.Fatalf("rbacPolicyPath for a missing explicit file = %q, want empty", got)
	}
}

// TestRBACPolicyPath_NoneReturnsEmpty confirms an empty working directory with no
// policy and no explicit path yields "".
func TestRBACPolicyPath_NoneReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := rbacPolicyPath(nil, &Config{}); got != "" {
		t.Fatalf("rbacPolicyPath in an empty dir = %q, want empty", got)
	}
}

// TestResolveRBACPolicyFile_DeprecatedAliasEmitsWarn confirms that resolving
// the deprecated admin_rbac_policy_file alias emits a one-time deprecation WARN
// pointing operators at the canonical rbac_policy_file key.
func TestResolveRBACPolicyFile_DeprecatedAliasEmitsWarn(t *testing.T) {
	// The deprecation warning is guarded by a process-wide sync.Once; reset it
	// so this test deterministically observes the emission.
	rbacPolicyFileDeprecationOnce = sync.Once{}
	t.Cleanup(func() { rbacPolicyFileDeprecationOnce = sync.Once{} })

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	got := resolveRBACPolicyFile(logger, &Config{AdminRBACPolicyFile: "legacy.csv"})
	if got != "legacy.csv" {
		t.Fatalf("resolveRBACPolicyFile = %q, want legacy.csv", got)
	}
	out := buf.String()
	if !strings.Contains(out, "admin_rbac_policy_file is deprecated") || !strings.Contains(out, "rbac_policy_file") {
		t.Fatalf("expected deprecation WARN mentioning both keys, got: %s", out)
	}
}

// TestResolveRBACPolicyFile_CanonicalKeyEmitsNoWarn confirms the canonical key
// resolves silently (no deprecation noise).
func TestResolveRBACPolicyFile_CanonicalKeyEmitsNoWarn(t *testing.T) {
	rbacPolicyFileDeprecationOnce = sync.Once{}
	t.Cleanup(func() { rbacPolicyFileDeprecationOnce = sync.Once{} })

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	got := resolveRBACPolicyFile(logger, &Config{RBACPolicyFile: "canonical.csv"})
	if got != "canonical.csv" {
		t.Fatalf("resolveRBACPolicyFile = %q, want canonical.csv", got)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Fatalf("canonical key must not emit a deprecation WARN, got: %s", buf.String())
	}
}

func writePolicy(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("p, admin, /, GET\n"), 0o644); err != nil {
		t.Fatalf("write policy %s: %v", path, err)
	}
}
