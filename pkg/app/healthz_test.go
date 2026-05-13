package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHealthzResponse_JSONShape pins the response body shape so external
// liveness probes do not silently break when fields are renamed.
func TestHealthzResponse_JSONShape(t *testing.T) {
	body := HealthzResponse{
		Status:    "healthy",
		CheckedAt: "2026-05-13T00:00:00Z",
		Checks: []HealthzCheck{
			{Name: "db:default", Status: "healthy", LatencyMS: 1},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := string(raw)
	for _, want := range []string{
		`"status":"healthy"`,
		`"checked_at":"2026-05-13T00:00:00Z"`,
		`"checks":[`,
		`"name":"db:default"`,
		`"latency_ms":1`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("response body missing %q in %s", want, got)
		}
	}
}

// TestBuildHealthProbes_OnlyConfiguredSubsystems verifies the probe set
// reflects current app state — no probe registered for a subsystem the
// app didn't initialise.
func TestBuildHealthProbes_OnlyConfiguredSubsystems(t *testing.T) {
	// Empty App: no DBs, no Config, no Storage → no probes.
	empty := &App{}
	if got := empty.buildHealthProbes(); len(got) != 0 {
		t.Fatalf("empty app should produce no probes, got %d", len(got))
	}

	// Config carrying a RedisURL but no DBs or Storage → exactly one probe.
	withRedis := &App{Config: &Config{RedisURL: "redis://127.0.0.1:6379"}}
	probes := withRedis.buildHealthProbes()
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe (redis), got %d", len(probes))
	}
	if probes[0].Name() != "redis" {
		t.Fatalf("expected redis probe, got %q", probes[0].Name())
	}

	// Config with empty RedisURL must not register a redis probe.
	withEmptyRedis := &App{Config: &Config{RedisURL: "   "}}
	if got := withEmptyRedis.buildHealthProbes(); len(got) != 0 {
		t.Fatalf("blank RedisURL must not produce a probe, got %d", len(got))
	}
}
