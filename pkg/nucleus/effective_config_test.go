package nucleus

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// findEffective returns the EffectiveValue for key, or fails the test.
func findEffective(t *testing.T, ec EffectiveConfig, key string) EffectiveValue {
	t.Helper()
	for _, v := range ec.Values {
		if v.Key == key {
			return v
		}
	}
	t.Fatalf("effective config has no key %q", key)
	return EffectiveValue{}
}

func TestLoadEffective_SourceAttribution(t *testing.T) {
	t.Parallel()

	// host is set only by base; log_level is overridden by overlay;
	// debug is never set (stays at the struct default). 192.0.2.1 is
	// TEST-NET-1 and will not collide with the default host.
	base := writeTempConfig(t, ".yaml", "host: 192.0.2.1\nlog_level: warn\n")
	overlay := writeTempConfig(t, ".yaml", "log_level: error\n")

	ec, err := LoadEffective([]string{base, overlay})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}

	host := findEffective(t, ec, "host")
	if host.Source.Kind != "yaml" || host.Source.Path != base {
		t.Errorf("host source: got %+v want {yaml %s}", host.Source, base)
	}

	logLevel := findEffective(t, ec, "log_level")
	if logLevel.Source.Kind != "yaml" || logLevel.Source.Path != overlay {
		t.Errorf("log_level source: got %+v want {yaml %s} (overlay wins)", logLevel.Source, overlay)
	}

	debug := findEffective(t, ec, "debug")
	if debug.Source.Kind != sourceKindDefault {
		t.Errorf("debug source: got %+v want kind=%q (untouched default)", debug.Source, sourceKindDefault)
	}
	if debug.Source.Path != "" {
		t.Errorf("debug source path: got %q want empty (defaults have no path)", debug.Source.Path)
	}
}

func TestLoadEffective_NullRevertsToDefaultSource(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", "log_level: warn\n")
	overlay := writeTempConfig(t, ".yaml", "log_level: null\n")

	ec, err := LoadEffective([]string{base, overlay})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}

	logLevel := findEffective(t, ec, "log_level")
	if logLevel.Source.Kind != sourceKindDefault {
		t.Errorf("log_level source after null revert: got %+v want kind=%q", logLevel.Source, sourceKindDefault)
	}
}

func TestLoadEffective_AppendOperatorSource(t *testing.T) {
	t.Parallel()

	base := writeTempConfig(t, ".yaml", "log_redact_extra_keys:\n  - alpha\n  - beta\n")
	overlay := writeTempConfig(t, ".yaml", "log_redact_extra_keys_append:\n  - gamma\n")

	ec, err := LoadEffective([]string{base, overlay})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}

	// The _append operator mutates the running value, so the key is
	// attributed to the file that carried the operator (overlay).
	v := findEffective(t, ec, "log_redact_extra_keys")
	if v.Source.Kind != "yaml" || v.Source.Path != overlay {
		t.Errorf("log_redact_extra_keys source: got %+v want {yaml %s}", v.Source, overlay)
	}
	// And the appended value is the union, not a misattributed drop.
	if list, ok := v.Value.([]any); !ok || len(list) != 3 {
		t.Errorf("log_redact_extra_keys value: got %v want 3 elements [alpha beta gamma]", v.Value)
	}
}

func TestLoadEffective_RedactsFrameworkSecrets(t *testing.T) {
	t.Parallel()

	// Flat compound secret keys whose leaf is not a short atomic name —
	// they must still redact via the canonical observe set (these are the
	// keys the Phase 3a security review flagged).
	cfg := writeTempConfig(t, ".yaml", "jwt_secret: super-signing-key\nsession_redis_url: redis://:pw@localhost:6379\n")

	ec, err := LoadEffective([]string{cfg})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	for _, key := range []string{"jwt_secret", "session_redis_url"} {
		v := findEffective(t, ec, key)
		if !v.Redacted || v.Value != observe.RedactionPlaceholder {
			t.Errorf("%s: expected redacted, got redacted=%v value=%v", key, v.Redacted, v.Value)
		}
	}
}

func TestLoadEffective_RedactsDatabaseURL(t *testing.T) {
	t.Parallel()

	cfg := writeTempConfig(t, ".yaml", "databases:\n  default:\n    url: postgres://user:secret@db.example.com/app\nport: 8099\n")

	ec, err := LoadEffective([]string{cfg})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}

	url := findEffective(t, ec, "databases.default.url")
	if !url.Redacted {
		t.Errorf("databases.default.url: expected redacted")
	}
	if url.Value != observe.RedactionPlaceholder {
		t.Errorf("databases.default.url value: got %v want %q", url.Value, observe.RedactionPlaceholder)
	}

	port := findEffective(t, ec, "port")
	if port.Redacted {
		t.Errorf("port: expected NOT redacted, got value %v", port.Value)
	}
}

func TestLoadEffective_RedactionExtraKeys(t *testing.T) {
	t.Parallel()

	// log_level is benign; redact it only when passed as an extra key,
	// proving the ExtraKeys mechanism extends the canonical set.
	cfg := writeTempConfig(t, ".yaml", "log_level: warn\n")

	plain, err := LoadEffective([]string{cfg})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	if findEffective(t, plain, "log_level").Redacted {
		t.Errorf("log_level should not redact without an extra key")
	}

	extended, err := LoadEffective([]string{cfg}, "log_level")
	if err != nil {
		t.Fatalf("LoadEffective(extra): %v", err)
	}
	if !findEffective(t, extended, "log_level").Redacted {
		t.Errorf("log_level should redact when listed in ExtraKeys")
	}
}

func TestShouldRedactKey(t *testing.T) {
	t.Parallel()

	set := redactionSet(nil)
	cases := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"smtp.password", true},
		{"token", true},
		{"session.secret", true},
		{"databases.default.url", true}, // parent-aware: DB connection string
		{"databases.reporting.url", true},
		{"port", false},
		{"log_level", false},
		{"databases.default.driver", false}, // non-sensitive leaf under databases.*
		{"public_url", false},               // leaf "public_url" is not the canonical "url" rule target
	}
	for _, tc := range cases {
		if got := shouldRedactKey(tc.key, set); got != tc.want {
			t.Errorf("shouldRedactKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
