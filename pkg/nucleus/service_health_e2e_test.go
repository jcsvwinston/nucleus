// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression test for the ServiceRegistration.Health wiring the v1.6.2
// godoc promised (declared but unused — QCD-FW-3's fourth papercut,
// deferred to this arc): a registered service's Health func must surface
// as a `service:<name>` check in /healthz, and a failing one must flip
// the endpoint to 503.
package nucleus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestRun_ServiceHealthWiredIntoHealthz(t *testing.T) {
	port := freeLocalPort(t)

	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "svc-health.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:  cfg,
			Options: []app.Option{app.WithoutDefaults()},
			Services: []ServiceRegistration{
				{
					Name: "indexer",
					Run: func(ctx context.Context) error {
						<-ctx.Done()
						return ctx.Err()
					},
					Health: func(context.Context) error {
						return errors.New("index 3 segments behind")
					},
				},
			},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("server did not come up within 10s")
		}
		resp, err := client.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		select {
		case err := <-runDone:
			t.Fatalf("Run exited during startup: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}

	resp, err := client.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz: %v", err)
	}

	var svcCheck *struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	for i := range body.Checks {
		if body.Checks[i].Name == "service:indexer" {
			svcCheck = &body.Checks[i]
			break
		}
	}
	if svcCheck == nil {
		names := make([]string, 0, len(body.Checks))
		for _, c := range body.Checks {
			names = append(names, c.Name)
		}
		t.Fatalf("service Health not wired into /healthz: no 'service:indexer' check (got %v)", names)
	}
	if svcCheck.Status != "unhealthy" || !strings.Contains(svcCheck.Message, "segments behind") {
		t.Errorf("service check must carry the Health error: %+v", svcCheck)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a failing service Health must flip /healthz to 503, got %d (status=%s)", resp.StatusCode, body.Status)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run after SIGTERM: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not shut down within 15s of SIGTERM")
	}
}
