// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// env carries what several probes share: one booted application, started at
// most once per run and torn down by the parent test.
//
// The application is a DEFAULT one — no module mounted, nothing wired by
// hand. What a probe asks it is what an application gets for FREE. A control
// that only works because the probe wired it by hand would measure the probe,
// not the framework. Probes that need a non-default application (an outbox
// turned on, a job registered) boot their own and say so.
type env struct {
	tb   testing.TB
	once sync.Once
	srv  *nucleustest.Server
}

func newEnv(tb testing.TB) *env { return &env{tb: tb} }

func (e *env) server() *nucleustest.Server {
	e.once.Do(func() {
		e.srv = nucleustest.StartApp(e.tb, nucleus.App{Config: benchConfig(e.tb)})
	})
	return e.srv
}

// benchConfig is the default application every probe measures against.
//
// The sqlite URL carries `_pragma=busy_timeout` deliberately, and it is worth
// knowing why a bench that measures the framework hands it a pragma: NU-77.
// The framework passes the sqlite DSN through bare, so busy_timeout stays 0
// and two writers on one file fail instead of waiting. OPS-05 measures that
// defect directly, on a connection of its own; every OTHER probe would just
// inherit its flakiness, so they opt out of it here.
func benchConfig(tb testing.TB) app.Config {
	tb.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.JWTSecret = strings.Repeat("jobsbench-probe-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(tb.TempDir(), "jobsbench.db") + "?_pragma=busy_timeout(10000)"},
	}
	return cfg
}

// status returns the status code the default application answers for one
// request. It is the measurement behind every "absent" verdict on an HTTP
// surface: a route nobody serves answers 404, and that is what an application
// author gets today.
func (e *env) status(t *testing.T, method, path string, headers map[string]string) int {
	t.Helper()
	srv := e.server()
	req, err := http.NewRequest(method, srv.URL(path), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
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
		if code := e.status(t, http.MethodGet, p, nil); code != http.StatusNotFound {
			t.Logf("GET %s answered %d, not 404: the surface exists", p, code)
			return partial
		}
	}
	return absent
}

// configRejects reports whether the configuration layer refuses a value for
// one key, and returns the error text so a probe can check that the message
// names what DOES exist. It is how the bench measures a provider that is not
// there: an application author writes the key, boot fails, and the error is
// the whole answer they get.
func configRejects(tb testing.TB, mutate func(*app.Config)) (bool, string) {
	tb.Helper()
	cfg := benchConfig(tb)
	mutate(&cfg)
	if err := app.ValidateSemantics(&cfg); err != nil {
		return true, err.Error()
	}
	return false, ""
}
