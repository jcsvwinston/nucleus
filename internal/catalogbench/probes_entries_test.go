// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/realtime"
)

// An entry control asks one question of one catalog entry: on a fresh
// `nucleus new --template api --offline` starter, does `nucleus add <name>`
// leave a project that builds, boots, and does the job the entry is for?
//
// "Does the job" is read from the application, not from the project: a
// storage provider logs that it was initialised (and talks to the bench's
// stand-in endpoint), an exporter serves its metrics or reports itself
// enabled, a directory joins the authentication chain. A project that
// builds and boots while the selected entry does nothing is partial, and the
// probe says why.
//
// Whether the entry is PINNED to the certified set is not asked here — that
// is CAT-03, one control for the whole catalog.

// moduleEntry is a catalog entry that ships as a module of its own: `nucleus
// add` fetches it and writes its blank import, and configuration selects it.
type moduleEntry struct {
	name   string
	module string
	config func(e *env) string
	env    func(e *env) []string
	visit  func(port int) string
	wired  func(output, visited string) (bool, string)
}

func storageWired(provider string) func(string, string) (bool, string) {
	line := `msg="storage provider initialized" provider=` + provider
	return func(output, _ string) (bool, string) {
		if strings.Contains(output, line) {
			return true, "the application logged " + line
		}
		return false, "no " + line + " in the boot log"
	}
}

var moduleEntries = []moduleEntry{
	{
		name: "s3", module: "github.com/jcsvwinston/nucleus/providers/storage-s3",
		config: func(e *env) string {
			return "storage:\n  provider: s3\n  s3:\n    endpoint: " + e.fakeStore().srv.URL +
				"\n    bucket: bench\n    region: us-east-1\n    access_key_id: bench\n    secret_access_key: bench-secret\n    use_path_style: true\n"
		},
		wired: storageWired("s3"),
	},
	{
		name: "gcs", module: "github.com/jcsvwinston/nucleus/providers/storage-gcs",
		config: func(*env) string { return "storage:\n  provider: gcs\n  gcs:\n    bucket: bench\n" },
		env: func(e *env) []string {
			return []string{"STORAGE_EMULATOR_HOST=" + strings.TrimPrefix(e.fakeStore().srv.URL, "http://")}
		},
		wired: storageWired("gcs"),
	},
	{
		name: "azure", module: "github.com/jcsvwinston/nucleus/providers/storage-azure",
		config: func(*env) string {
			return "storage:\n  provider: azure\n  azure:\n    account_name: bench\n    account_key: YmVuY2gtYmVuY2gtYmVuY2g=\n    container: bench\n"
		},
		wired: storageWired("azure"),
	},
	{
		name: "ldap", module: "github.com/jcsvwinston/nucleus/providers/ldap",
		config: func(*env) string {
			return "auth_backends: [ldap]\nauth:\n  ldap:\n    url: ldap://127.0.0.1:1\n    base_dn: dc=bench,dc=test\n"
		},
		wired: func(output, _ string) (bool, string) {
			if strings.Contains(output, "authentication chain ready") && strings.Contains(output, "backends=ldap") {
				return true, "the authentication chain is ready with backends=ldap"
			}
			return false, "the boot log shows no authentication chain with ldap in it"
		},
	},
	{
		name: "otlp", module: "github.com/jcsvwinston/nucleus/exporters/otlp",
		config: func(*env) string { return "otlp_endpoint: http://127.0.0.1:1\n" },
		wired: func(output, _ string) (bool, string) {
			if strings.Contains(output, `msg="otel initialized"`) && strings.Contains(output, "otlp_enabled=true") {
				return true, "telemetry initialised with otlp_enabled=true"
			}
			return false, "the boot log does not show the OTLP exporter enabled"
		},
	},
	{
		name: "prometheus", module: "github.com/jcsvwinston/nucleus/exporters/prometheus",
		// A path other than the default is what tells the framework the
		// operator ASKED for metrics; the default alone only logs a hint.
		config: func(*env) string { return "metrics_path: /bench-metrics\n" },
		visit: func(port int) string {
			status, body := get(port, "/bench-metrics")
			return fmt.Sprintf("%d %s", status, firstLines(body, 3))
		},
		wired: func(_, visited string) (bool, string) {
			if strings.HasPrefix(visited, "200 ") && strings.Contains(visited, "# TYPE") {
				return true, "GET /bench-metrics answered the Prometheus exposition format"
			}
			return false, "GET /bench-metrics answered " + firstLines(visited, 2)
		},
	},
}

func moduleEntryNamed(name string) moduleEntry {
	for _, m := range moduleEntries {
		if m.name == name {
			return m
		}
	}
	panic("no module entry " + name)
}

func (m moduleEntry) envFor(e *env) []string {
	if m.env == nil {
		return nil
	}
	return m.env(e)
}

// starterConfig is the starter's own nucleus.yml followed by a block.
func starterConfig(t *testing.T, p *project, block string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.dir, "nucleus.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(raw), "\n") + "\n" + block
}

func probeModuleEntry(t *testing.T, e *env, name string) verdict {
	m := moduleEntryNamed(name)
	base := e.scaffold(t)
	v := e.built(t, name)
	if v.add.code != 0 {
		t.Logf("nucleus add %s exited %d:\n%s", name, v.add.code, firstLines(v.add.all(), 12))
		return absent
	}
	t.Logf("nucleus add %s:\n%s", name, firstLines(v.add.stdout, 4))
	if v.buildErr != nil {
		t.Logf("the project nucleus add left does not build:\n%s", v.buildLog)
		return absent
	}
	config := starterConfig(t, base, m.config(e))
	var visited string
	b := bootWith(t, v.bin, config, m.envFor(e), func(port int) {
		if m.visit != nil {
			visited = m.visit(port)
		}
	})
	if !b.listening {
		t.Logf("the application does not start with %s selected: %v\n%s", name, b.exitErr, firstLines(b.output, 12))
		return partial
	}
	ok, evidence := m.wired(b.output, visited)
	if !ok {
		t.Logf("the application starts, but nothing shows %s doing its job: %s", name, evidence)
		// The diagnosis: the starter WITHOUT the module, same configuration.
		without := bootWith(t, base.bin, config, m.envFor(e), nil)
		if without.listening {
			t.Logf("and the starter without the module starts the same way: the selection is ignored, not refused")
		} else {
			t.Logf("the starter without the module refuses: %s", firstLines(without.output, 4))
		}
		return partial
	}
	if reqs := e.fakeStore().requests(); len(reqs) > 0 && strings.HasPrefix(m.module, "github.com/jcsvwinston/nucleus/providers/storage-") {
		t.Logf("the stand-in object store was asked: %v", reqs)
	}
	t.Logf("wired: %s", evidence)
	return present
}

func probeEntryS3(t *testing.T, e *env) verdict    { return probeModuleEntry(t, e, "s3") }
func probeEntryGCS(t *testing.T, e *env) verdict   { return probeModuleEntry(t, e, "gcs") }
func probeEntryAzure(t *testing.T, e *env) verdict { return probeModuleEntry(t, e, "azure") }
func probeEntryLDAP(t *testing.T, e *env) verdict  { return probeModuleEntry(t, e, "ldap") }
func probeEntryOTLP(t *testing.T, e *env) verdict  { return probeModuleEntry(t, e, "otlp") }
func probeEntryPrometheus(t *testing.T, e *env) verdict {
	return probeModuleEntry(t, e, "prometheus")
}

// ---- entries that live in the core ------------------------------------------

// coreEntry measures an entry whose code is already in the framework, so
// there is nothing to `go get`: the catalog's job is the wiring. When `nucleus
// add <name>` accepts the name, the probe adds it to the starter, builds,
// boots, and runs check against the application (present when it passes).
// Until then the probe wires the capability BY HAND, the way an author does
// today, and partial means "it works, and the catalog does not do it for
// you".
func probeCoreEntry(t *testing.T, e *env, name, config string,
	check func(t *testing.T, port int) (bool, string),
	byHand func(t *testing.T, e *env) bool) verdict {
	r := e.dryRun(name)
	if r.code == 0 {
		t.Logf("nucleus add %s is accepted:\n%s", name, firstLines(r.stdout, 6))
		return addedEntryVerdict(t, e, name, config, check)
	}
	t.Logf("nucleus add %s: %s", name, firstLines(r.stderr, 1))
	if byHand(t, e) {
		return partial
	}
	return absent
}

// addedEntryVerdict is the measurement once `nucleus add` knows a name:
// the real command on a copy of the starter, a build, a boot, and the check
// against the running application.
func addedEntryVerdict(t *testing.T, e *env, name, config string,
	check func(t *testing.T, port int) (bool, string)) verdict {
	base := e.scaffold(t)
	v := e.built(t, name)
	if v.add.code != 0 || v.buildErr != nil {
		t.Logf("nucleus add %s did not leave a project that builds: %s%s", name, firstLines(v.add.all(), 6), v.buildLog)
		return absent
	}
	var ok bool
	var evidence string
	b := bootWith(t, v.bin, starterConfig(t, base, config), nil, func(port int) {
		if check != nil {
			ok, evidence = check(t, port)
		}
	})
	if !b.listening {
		t.Logf("the starter with %s does not start: %v\n%s", name, b.exitErr, firstLines(b.output, 10))
		return partial
	}
	if check == nil {
		t.Logf("the starter builds and boots with %s added; this probe has no wiring check for it yet — "+
			"grow it before recording present", name)
		return present
	}
	if !ok {
		t.Logf("the starter boots with %s added, but the capability is not wired: %s", name, evidence)
		return partial
	}
	t.Logf("wired: %s", evidence)
	return present
}

const oidcConfig = "public_base_url: http://127.0.0.1:8080\nauth_federated:\n  - name: corp\n    provider: oidc\n" +
	"auth:\n  corp:\n    issuer: http://127.0.0.1:1/\n    client_id: bench\n"

// oidcWired: the federated set is built with the instance, and its sign-in
// route answers (an entry that only builds the provider leaves the route to
// the application, which is the half the catalog is meant to do).
func oidcWired(t *testing.T, port int) (bool, string) {
	status, _ := get(port, "/auth/corp/start")
	return status != 0 && status != http.StatusNotFound, fmt.Sprintf("GET /auth/corp/start answered %d", status)
}

func probeEntryOIDC(t *testing.T, e *env) verdict {
	return probeCoreEntry(t, e, "oidc", oidcConfig, oidcWired, func(t *testing.T, e *env) bool {
		// By hand: the blank import the federated registry needs, written
		// into the starter's main.go, and the configuration.
		base := e.scaffold(t)
		dir := t.TempDir()
		must(t, copyProject(base.dir, dir))
		mainGo := filepath.Join(dir, "main.go")
		src, err := os.ReadFile(mainGo)
		must(t, err)
		driver := `_ "github.com/jcsvwinston/nucleus/drivers/sqlite"`
		edited := strings.Replace(string(src), driver, driver+"\n\t_ \"github.com/jcsvwinston/nucleus/pkg/auth/federated/oidc\"", 1)
		must(t, os.WriteFile(mainGo, []byte(edited), 0o644))
		if log, err := goRun(dir, "build", "-o", exeName("app"), "."); err != nil {
			t.Logf("the starter with the oidc import by hand does not build:\n%s", log)
			return false
		}
		var start int
		b := bootWith(t, filepath.Join(dir, exeName("app")), starterConfig(t, base, oidcConfig), nil, func(port int) {
			start, _ = get(port, "/auth/corp/start")
		})
		if !b.listening || !strings.Contains(b.output, "federated sign-in configured") || !strings.Contains(b.output, "instances=corp") {
			t.Logf("by hand, the starter does not build the federated set:\n%s", firstLines(b.output, 8))
			return false
		}
		t.Logf("by hand (blank import of pkg/auth/federated/oidc + auth_federated): the federated set is built; "+
			"GET /auth/corp/start answers %d — the sign-in routes are the application's to mount", start)
		return true
	})
}

func probeEntryAPIKeys(t *testing.T, e *env) verdict {
	check := func(t *testing.T, port int) (bool, string) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/openapi.json", port), nil)
		req.Header.Set(apikeys.HeaderName, apikeys.Prefix+"_bogus_bogus")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err.Error()
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusUnauthorized, fmt.Sprintf("a request with a forged key answered %d", resp.StatusCode)
	}
	return probeCoreEntry(t, e, "apikeys", "", check, func(t *testing.T, e *env) bool {
		db := memorySQLite(t, "catalogbench_keys")
		store, err := apikeys.NewSQLStore(t.Context(), db, apikeys.SQLStoreConfig{Flavor: apikeys.FlavorSQLite})
		if err != nil {
			t.Logf("apikeys.NewSQLStore: %v", err)
			return false
		}
		_, presented, err := apikeys.Issue(t.Context(), store, apikeys.Key{Name: "bench", OwnerID: "user-1"})
		if err != nil {
			t.Logf("apikeys.Issue: %v", err)
			return false
		}
		h := apikeys.Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		status := func(key string) int {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(apikeys.HeaderName, key)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec.Code
		}
		good, bad := status(presented), status(apikeys.Prefix+"_bogus_bogus")
		t.Logf("by hand (a SQL store, Issue, Middleware around a handler): a valid key answers %d, a forged one %d", good, bad)
		return good == http.StatusNoContent && bad == http.StatusUnauthorized
	})
}

func probeEntryAccounts(t *testing.T, e *env) verdict {
	check := func(t *testing.T, port int) (bool, string) {
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d%s", port, accounts.RouteRegister), "application/json",
			strings.NewReader(`{"email":"bench@example.test","username":"bench","password":"correct horse battery staple"}`))
		if err != nil {
			return false, err.Error()
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusAccepted, fmt.Sprintf("POST %s answered %d", accounts.RouteRegister, resp.StatusCode)
	}
	return probeCoreEntry(t, e, "accounts", "", check, func(t *testing.T, e *env) bool {
		// By hand: the store needs a *sql.DB BEFORE the application is
		// built, because accounts.Module takes a finished *Service — so the
		// author opens a second handle to a database of their own, and
		// hands the service a mailer of their own too (without one,
		// registration answers 500: the flow refuses to pretend it sent
		// the verification mail).
		db := memorySQLite(t, "catalogbench_accounts")
		store, err := accounts.NewSQLStore(t.Context(), db, accounts.SQLStoreConfig{Flavor: accounts.FlavorSQLite})
		if err != nil {
			t.Logf("accounts.NewSQLStore: %v", err)
			return false
		}
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		svc, err := accounts.New(store, nil, &recordingMailer{}, nil, accounts.Config{
			BaseURL: "https://catalogbench.example.test", From: "no-reply@example.test", MFAEncryptionKey: key, Issuer: "Catalogbench",
		}, nil)
		if err != nil {
			t.Logf("accounts.New: %v", err)
			return false
		}
		cfg := inProcessConfig(t)
		srv := nucleustest.StartApp(t, nucleus.App{
			Config:  cfg,
			Options: []app.Option{app.WithOpenAuthz()},
			Modules: map[string]nucleus.ModuleSpec{"accounts": accounts.Module(svc)},
		})
		resp, err := srv.Client().Post(srv.URL(accounts.RouteRegister), "application/json",
			strings.NewReader(`{"email":"bench@example.test","username":"bench","password":"correct horse battery staple"}`))
		if err != nil {
			t.Logf("register: %v", err)
			return false
		}
		_ = resp.Body.Close()
		t.Logf("by hand (its own SQL store, accounts.New, Mount(accounts.Module(svc))): POST %s answers %d", accounts.RouteRegister, resp.StatusCode)
		return resp.StatusCode == http.StatusAccepted
	})
}

func probeEntrySQLQueue(t *testing.T, e *env) verdict {
	return probeCoreEntry(t, e, "sql-queue", "jobs_provider: sql\n", nil, func(t *testing.T, e *env) bool {
		// By hand: jobs_provider: sql, and a module that registers a job —
		// the queue does not exist until one does (NF-13).
		var ran atomic.Int32
		cfg := inProcessConfig(t)
		cfg.JobsProvider = "sql"
		srv := nucleustest.StartApp(t, nucleus.App{
			Config:  cfg,
			Options: []app.Option{app.WithOpenAuthz()},
			Modules: map[string]nucleus.ModuleSpec{"benchjobs": nucleus.Module[struct{}]{
				Name: "benchjobs",
				Jobs: func(j nucleus.JobRegistry, _ struct{}) {
					_ = j.Register("tick", nucleus.JobSpec{Every: 100 * time.Millisecond, Handler: func(context.Context) error {
						ran.Add(1)
						return nil
					}})
				},
			}.Build()},
		})
		deadline := time.Now().Add(15 * time.Second)
		for ran.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		var rows int
		err := srv.Runtime().DB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM nucleus_jobs").Scan(&rows)
		t.Logf("by hand (jobs_provider: sql + a module with a job): the job ran %d time(s); the queue table answers err=%v", ran.Load(), err)
		return ran.Load() > 0 && err == nil
	})
}

func probeEntryWebSockets(t *testing.T, e *env) verdict {
	return probeCoreEntry(t, e, "websockets", "", nil, func(t *testing.T, e *env) bool {
		// By hand: a hub the application owns and a route that serves it.
		hub := realtime.New(realtime.Config{Logger: slog.New(slog.DiscardHandler)})
		defer func() { _ = hub.Close() }()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = realtime.ServeWS(w, r, realtime.WSConfig{Hub: hub, Topics: []string{"bench"}, PingInterval: time.Hour})
		}))
		defer srv.Close()
		conn, reader, status, err := dialWS(srv.URL)
		if err != nil {
			t.Logf("by hand: the handshake failed: %v", err)
			return false
		}
		defer func() { _ = conn.Close() }()
		deadline := time.Now().Add(5 * time.Second)
		for hub.Count("bench") == 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		hub.Broadcast(t.Context(), realtime.Message{Topic: "bench", Event: "tick", Data: []byte(`"catalogbench"`)})
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		frame, err := readFrame(reader)
		t.Logf("by hand (realtime.New + realtime.ServeWS on a route): handshake %q, first frame %q (err=%v)", status, frame, err)
		return strings.Contains(status, "101") && strings.Contains(frame, "catalogbench")
	})
}

// ---- entries that do not exist yet ----------------------------------------

// missingEntry is an entry the catalog names and the repository does not
// ship. The probe asks `nucleus add` under the names other ecosystems use,
// then looks for the code anywhere in the repository — a package, a module,
// a dependency on the library such a module would wrap — and logs every
// place it looked, which is the honest form of an absence.
type missingEntry struct {
	names []string // what a person would type
	dirs  []string // where the code would live
	deps  []string // the library a module would wrap
	src   *regexp.Regexp
	under string // where src is searched
}

func probeMissingEntry(t *testing.T, e *env, m missingEntry) verdict {
	for _, n := range m.names {
		if r := e.dryRun(n); r.code == 0 {
			t.Logf("nucleus add %s is accepted:\n%s", n, firstLines(r.stdout, 4))
			return addedEntryVerdict(t, e, n, "", nil)
		}
	}
	t.Logf("nucleus add refuses every name asked: %v", m.names)
	found := false
	if d := existingDirs(t, m.dirs...); len(d) > 0 {
		t.Logf("code exists at %v, and nucleus add does not install it", d)
		found = true
	} else {
		t.Logf("no package or module at any of %v", m.dirs)
	}
	if len(m.deps) > 0 {
		if g := goModsRequiring(t, m.deps...); len(g) > 0 {
			t.Logf("a module of this repository requires %v", g)
			found = true
		} else {
			t.Logf("no go.mod in the repository requires %v", m.deps)
		}
	}
	if m.src != nil {
		if s := sourceMatches(t, m.under, m.src); len(s) > 0 {
			t.Logf("source under %s matches %s: %v", m.under, m.src, s)
			found = true
		} else {
			t.Logf("no source under %s matches %s", m.under, m.src)
		}
	}
	if found {
		return partial
	}
	return absent
}

func probeEntrySAML(t *testing.T, e *env) verdict {
	return probeMissingEntry(t, e, missingEntry{
		names: []string{"saml", "saml2", "auth-saml"},
		dirs:  []string{"pkg/auth/federated/saml", "providers/saml", "providers/auth-saml", "providers/federated-saml"},
		deps:  []string{"github.com/crewjam/saml", "github.com/russellhaering/gosaml2"},
		src:   regexp.MustCompile(`federated\.Register\("saml"`),
		under: ".",
	})
}

func probeEntryRedisCache(t *testing.T, e *env) verdict {
	return probeMissingEntry(t, e, missingEntry{
		names: []string{"redis-cache", "redis", "cache-redis"},
		dirs:  []string{"pkg/cache/redis", "providers/cache-redis", "providers/redis-cache", "providers/redis"},
		// go-redis is already in the core graph (sessions, the asynq queue,
		// the realtime relay), so a dependency check proves nothing here;
		// the question is whether pkg/cache has a backend over it.
		src:   regexp.MustCompile(`(?i)func\s+New(Redis|RedisCache)\b|redis\.(Client|UniversalClient)`),
		under: "pkg/cache",
	})
}

func probeEntryStripe(t *testing.T, e *env) verdict {
	return probeMissingEntry(t, e, missingEntry{
		names: []string{"stripe", "billing", "payments-stripe"},
		dirs:  []string{"providers/stripe", "providers/billing-stripe", "providers/payments-stripe", "pkg/billing", "pkg/payments"},
		deps:  []string{"github.com/stripe/stripe-go"},
		src:   regexp.MustCompile(`subscription\.create|CapabilitySubscription`),
		under: "pkg",
	})
}

func probeEntrySentry(t *testing.T, e *env) verdict {
	seam := sourceMatches(t, "pkg/router/interceptor", regexp.MustCompile(`(?m)^func Register\(`))
	t.Logf("the seam a reporter module would register on — the request-interceptor registry (http_interceptors): %v; "+
		"errors.Reportable is per error type", seam)
	return probeMissingEntry(t, e, missingEntry{
		names: []string{"sentry", "errors-sentry", "error-tracking"},
		dirs:  []string{"providers/sentry", "exporters/sentry", "providers/errors-sentry", "pkg/errortracking"},
		deps:  []string{"github.com/getsentry/sentry-go"},
		src:   regexp.MustCompile(`sentry\.(Init|CaptureException|NewClient)|getsentry`),
		under: ".",
	})
}

// ---- helpers ---------------------------------------------------------------

// inProcessConfig is the configuration of an application a probe boots in
// its own process: a throwaway SQLite database, a signing secret, and only
// the errors in the log.
func inProcessConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.LogLevel = "error"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("catalogbench-probe-secret", 2)
	return cfg
}

// recordingMailer is the Mailer an author hands accounts.New by hand.
type recordingMailer struct{ sent atomic.Int32 }

func (m *recordingMailer) Send(context.Context, mail.Message) error {
	m.sent.Add(1)
	return nil
}

func memorySQLite(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", name, time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// dialWS performs a WebSocket handshake by hand: what is measured is
// whether the SERVER completes the protocol.
func dialWS(rawURL string) (net.Conn, *bufio.Reader, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, "", err
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		return nil, nil, "", err
	}
	var key [16]byte
	_, _ = rand.Read(key[:])
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", u.Host, base64.StdEncoding.EncodeToString(key[:]))
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, nil, "", err
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, nil, "", err
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
	return conn, reader, strings.TrimSpace(status), nil
}

// readFrame reads one unmasked server frame and returns its payload.
func readFrame(r *bufio.Reader) (string, error) {
	head := make([]byte, 2)
	if _, err := ioReadFull(r, head); err != nil {
		return "", err
	}
	n := int(head[1] & 0x7f)
	switch n {
	case 126:
		ext := make([]byte, 2)
		if _, err := ioReadFull(r, ext); err != nil {
			return "", err
		}
		n = int(ext[0])<<8 | int(ext[1])
	case 127:
		return "", fmt.Errorf("frame too large for the probe")
	}
	payload := make([]byte, n)
	if _, err := ioReadFull(r, payload); err != nil {
		return "", err
	}
	return string(payload), nil
}

func ioReadFull(r *bufio.Reader, b []byte) (int, error) {
	read := 0
	for read < len(b) {
		n, err := r.Read(b[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}
