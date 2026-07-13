package authz

// Regression guard for NU-P1-4: `metrics_public: false` must be able to
// keep /metrics out of the anonymous bootstrap allow-list while the rest
// of the framework-owned routes keep responding.

import (
	"io"
	"log/slog"
	"testing"
)

func newSeedTestEnforcer(t *testing.T) *Enforcer {
	t.Helper()
	e, err := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestSeedBootstrapAllowList_IncludesMetricsByDefault(t *testing.T) {
	e := newSeedTestEnforcer(t)
	if err := e.SeedBootstrapAllowList(); err != nil {
		t.Fatalf("SeedBootstrapAllowList: %v", err)
	}
	if !e.Can(BootstrapSubject, "/metrics", "GET") {
		t.Fatal("default seeding must allow anonymous /metrics")
	}
}

func TestSeedBootstrapAllowListExcluding_SkipsMetricsOnly(t *testing.T) {
	e := newTestEnforcer(t)
	if err := e.SeedBootstrapAllowListExcluding("/metrics"); err != nil {
		t.Fatalf("SeedBootstrapAllowListExcluding: %v", err)
	}
	if e.Can(BootstrapSubject, "/metrics", "GET") {
		t.Fatal("/metrics must NOT be anonymous when excluded")
	}
	for _, obj := range []string{"/healthz", "/login", "/.well-known/jwks.json"} {
		if !e.Can(BootstrapSubject, obj, "GET") {
			t.Fatalf("%s must remain in the bootstrap allow-list", obj)
		}
	}
}
