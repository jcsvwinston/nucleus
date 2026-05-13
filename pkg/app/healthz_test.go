package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type stubDB struct {
	err error
}

func (s *stubDB) Health(ctx context.Context) error { return s.err }

func TestProbeDB_Healthy(t *testing.T) {
	check := probeDB(context.Background(), "default", &stubDB{})
	if check.Status != "healthy" {
		t.Fatalf("expected healthy, got %q (msg=%q)", check.Status, check.Message)
	}
	if check.Name != "db:default" {
		t.Fatalf("expected name db:default, got %q", check.Name)
	}
}

func TestProbeDB_Unhealthy(t *testing.T) {
	check := probeDB(context.Background(), "shard", &stubDB{err: errors.New("boom")})
	if check.Status != "unhealthy" {
		t.Fatalf("expected unhealthy, got %q", check.Status)
	}
	if check.Message != "boom" {
		t.Fatalf("expected message 'boom', got %q", check.Message)
	}
}

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
