package contracts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	_ "modernc.org/sqlite"
)

// TestEndpointsDocParity_DocumentedEndpointsRespond is the endpoints-side
// companion to TestCLIDocParity_OverviewCommandsExist. It guards the
// website docs against documenting routes the framework does not actually
// expose, by mounting a minimal in-memory app and verifying every
// documented endpoint responds.
//
// Currently covers /healthz (audit discrepancy D3 in
// docs/audits/2026-05-12-enterprise-readiness.md, referenced from
// website/docs/features/observability.md:62 and
// website/docs/getting-started/quickstart.md:35). When new endpoints get
// added to those tables (e.g. /metrics), append them here in lockstep
// with the docs and the implementation.
func TestEndpointsDocParity_DocumentedEndpointsRespond(t *testing.T) {
	documented := []struct {
		path       string
		wantStatus int
		// allowAny accepts any 2xx/3xx if the exact status is hard to
		// pin without a fuller setup (e.g. redirects from /admin to
		// /admin/login). Kept off for /healthz — that one must be 200.
		allowAny bool
	}{
		{path: "/healthz", wantStatus: http.StatusOK},
		{path: "/metrics", wantStatus: http.StatusOK},
	}

	cfg := minimalAppConfig()
	a, err := app.New(cfg, app.WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	}()

	ts := httptest.NewServer(a.Router)
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	for _, ep := range documented {
		t.Run(ep.path, func(t *testing.T) {
			resp, err := client.Get(ts.URL + ep.path)
			if err != nil {
				t.Fatalf("GET %s: %v", ep.path, err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if ep.allowAny {
				if resp.StatusCode >= 400 {
					t.Fatalf("GET %s: documented endpoint returned %d (body: %s)", ep.path, resp.StatusCode, body)
				}
				return
			}

			if resp.StatusCode != ep.wantStatus {
				t.Fatalf("GET %s: want status %d, got %d (body: %s)", ep.path, ep.wantStatus, resp.StatusCode, body)
			}

			// /healthz pins its body shape via pkg/app/healthz_test.go.
			// Smoke-check here that the body is valid JSON and contains
			// the top-level "status" field, so a future refactor cannot
			// drop the JSON contract without flagging.
			if ep.path == "/healthz" {
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("/healthz body is not JSON: %v (raw: %s)", err, body)
				}
				if _, ok := payload["status"]; !ok {
					t.Fatalf("/healthz body missing \"status\" field: %s", body)
				}
			}
		})
	}
}

func minimalAppConfig() *app.Config {
	return &app.Config{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    2 * time.Second,
		IdleTimeout:     5 * time.Second,
		DatabaseDefault: "default",
		Databases: map[string]app.DatabaseConfig{
			"default": {
				URL:         "sqlite://:memory:",
				MaxOpen:     1,
				MaxIdle:     1,
				MaxLifetime: time.Minute,
			},
		},
		LogLevel:    "error",
		LogFormat:   "text",
		MetricsPath: "/metrics",
	}
}
