package cli

import (
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
