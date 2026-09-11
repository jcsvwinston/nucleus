// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// env carries what several probes share: one booted application, started at
// most once per run and torn down by the parent test.
//
// The application is a DEFAULT one — no module mounted, nothing wired by
// hand. That is deliberate: what a probe asks it is what an application gets
// for free from the framework, which is exactly the question an "auth as a
// product" arc has to answer. A control the application only has because the
// probe wired it would measure the probe.
type env struct {
	tb   testing.TB
	once sync.Once
	srv  *nucleustest.Server
}

func newEnv(tb testing.TB) *env { return &env{tb: tb} }

func (e *env) server() *nucleustest.Server {
	e.once.Do(func() {
		cfg := app.DefaultConfig()
		cfg.Env = "development"
		// The default database URL is a file relative to the working
		// directory; a temp one keeps the package clean.
		cfg.Databases = nucleustest.TempSQLite(e.tb)
		cfg.JWTSecret = strings.Repeat("authbench-probe-secret", 2)
		e.srv = nucleustest.StartApp(e.tb, nucleus.App{Config: cfg})
	})
	return e.srv
}

// status returns the status code the default application answers for one
// request. It is the measurement behind every "absent" verdict on an HTTP
// surface: a route nobody serves answers 404, and that is what an application
// author gets today.
func (e *env) status(t *testing.T, method, path string, headers map[string]string) int {
	t.Helper()
	srv := e.server()
	req, err := http.NewRequest(method, srv.URL(path), strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// unroutedVerdict turns "none of these paths is served" into a verdict. Any
// answer other than 404 means the framework serves something there and the
// control needs a real probe instead of this one.
func (e *env) unroutedVerdict(t *testing.T, paths ...string) verdict {
	t.Helper()
	for _, p := range paths {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			if code := e.status(t, m, p, nil); code != http.StatusNotFound {
				t.Logf("%s %s answered %d, not 404: the surface exists", m, p, code)
				return partial
			}
		}
	}
	return absent
}

// startRateLimited boots an application whose limiter allows one request per
// window and scopes by role — the tightest per-identity setting the
// configuration offers — with one route to spend it on.
func startRateLimited(t *testing.T) *nucleustest.Server {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("authbench-probe-secret", 2)
	cfg.RateLimitRequests = 1
	cfg.RateLimitBurst = 1
	cfg.RateLimitWindow = time.Hour
	cfg.RateLimitByRole = true

	return nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithOpenAuthz()},
		Modules: map[string]nucleus.ModuleSpec{
			"authbench-probe": nucleus.Module[struct{}]{
				Name: "authbench-probe",
				Routes: func(r nucleus.Router, _ struct{}) {
					r.Get("/authbench-probe/spend", func(c *nucleus.Context) error {
						return c.NoContent()
					})
				},
			}.Build(),
		},
	})
}

// rateLimitedProbe spends one request as the bearer of the given token.
func rateLimitedProbe(t *testing.T, srv *nucleustest.Server, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL("/authbench-probe/spend"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}
