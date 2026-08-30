package cli

import (
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
)

func TestDiffConfig(t *testing.T) {
	defaultCfg := app.DefaultConfig()
	current := defaultCfg
	current.Port = 9090
	current.LogFormat = "text"
	current.ReadTimeout = 10 * time.Second

	diffs := diffConfig(defaultCfg, current)

	var changed map[string]settingDiff
	changed = make(map[string]settingDiff)
	for _, item := range diffs {
		if item.Changed {
			changed[item.Key] = item
		}
	}

	if _, ok := changed["port"]; !ok {
		t.Fatalf("expected changed setting for port")
	}
	if _, ok := changed["log_format"]; !ok {
		t.Fatalf("expected changed setting for log_format")
	}
	if _, ok := changed["read_timeout"]; !ok {
		t.Fatalf("expected changed setting for read_timeout")
	}

	if len(changed) < 3 {
		t.Fatalf("expected at least 3 changed settings, got %d", len(changed))
	}
}

// A nil slice in the defaults and an empty slice in the loaded config are
// the same setting. The loader materialises empty collections (e.g.
// auth_federated, jwt_keys), and diffsettings used to print them as
// no-differences: `auth_federated [] -> []` (NC-13).
func TestDiffConfigNilAndEmptySlicesAreEqual(t *testing.T) {
	defaultCfg := app.DefaultConfig()
	current := defaultCfg
	current.AuthFederated = []auth.FederatedInstance{}
	current.JWTKeys = []app.JWTKeySpec{}

	for _, item := range diffConfig(defaultCfg, current) {
		if item.Changed && (item.Key == "auth_federated" || item.Key == "jwt_keys") {
			t.Fatalf("nil vs empty must not be a difference, got %s: %v -> %v", item.Key, item.Default, item.Current)
		}
	}
}

func TestFormatSettingValue(t *testing.T) {
	if got := formatSettingValue(""); got != `""` {
		t.Fatalf("expected empty string formatting, got %q", got)
	}
	if got := formatSettingValue(true); got != "true" {
		t.Fatalf("expected bool formatting, got %q", got)
	}
}
