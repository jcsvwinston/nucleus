// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Behavioral contract of the in-process test kit (DX-22): the application
// starts and stops inside the test process — no go build, no exec.Command,
// no hand-rolled /healthz polling — and MintToken issues bearer tokens the
// application's own JWT material validates. Two consecutive boots in the
// same process pin the "parable" half: a kit that cannot shut the first
// instance down fails the second Start on a leaked port or a hung run loop.
package nucleustest

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func pingApp(t *testing.T) nucleus.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.JWTSecret = strings.Repeat("nucleustest-secret-", 3)
	cfg.JWTIssuer = "nucleustest"
	cfg.JWTExpiry = time.Hour

	m := nucleus.Module[struct{}]{
		Name: "ping",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/ping", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"pong": "true"})
			})
		},
	}.Build()

	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		// The kit's contract under test is lifecycle + minting, not RBAC:
		// open authz keeps /ping reachable without policy rows.
		Options: []app.Option{app.WithOpenAuthz()},
	}
}

func TestStartServesStopsAndRestarts(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	first := StartApp(t, pingApp(t))

	resp, err := first.Client().Get(first.URL("/ping"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "pong") {
		t.Fatalf("GET /ping: want 200 with pong, got %d body=%s", resp.StatusCode, body)
	}

	// Stop must terminate the run loop and release the socket...
	first.Stop()
	if _, err := first.Client().Get(first.URL("/healthz")); err == nil {
		t.Fatal("first instance still answers after Stop")
	}

	// ...so a second in-process boot works — the restart pattern E2E suites
	// use to prove persistence.
	second := StartApp(t, pingApp(t))
	resp2, err := second.Client().Get(second.URL("/ping"))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /ping on second in-process boot: want 200, got %d", resp2.StatusCode)
	}
}

func TestMintTokenRoundTripsThroughTheAppsJWTMaterial(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	a := pingApp(t)
	srv := StartApp(t, a)

	token := srv.MintToken("user-1", "tester", "admin")
	if token == "" {
		t.Fatal("MintToken returned an empty token")
	}

	manager := auth.NewJWTManager(a.Config.JWTSecret, a.Config.JWTExpiry, a.Config.JWTIssuer)
	claims, err := manager.Validate(token)
	if err != nil {
		t.Fatalf("the app's own JWT material rejects the minted token: %v", err)
	}
	if claims.Username != "tester" || claims.Role != "admin" {
		t.Fatalf("claims round-trip: got username=%q role=%q", claims.Username, claims.Role)
	}
}
