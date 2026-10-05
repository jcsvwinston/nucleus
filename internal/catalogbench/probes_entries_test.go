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
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
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

// coreRun is what a core entry's wiring check reads: the running
// application's port, the directory it runs in (its SQLite database is
// there), and what it has logged so far.
type coreRun struct {
	port   int
	dir    string
	output func() string
}

// coreEntry is how a core entry is measured once `nucleus add` knows it.
type coreEntry struct {
	name string
	// edit is what the person does to the configuration `nucleus add`
	// wrote: the values only they know (the identity provider's address).
	// nil boots the file as the command left it.
	edit func(t *testing.T, e *env, config string) string
	// code is the application code no entry can write — the job a queue
	// runs — added to the project before it is built. nil adds nothing.
	code func(t *testing.T, dir string)
	// check is the wiring check: the capability doing its job in the
	// running application. An entry the command learns gets one in the same
	// session (see docs/catalog-bench.md); without it the probe cannot
	// record present.
	check func(t *testing.T, e *env, run coreRun) (bool, string)
}

// probeCoreEntry measures an entry whose code is already in the framework,
// so there is nothing to `go get`: the catalog's job is the wiring. When
// `nucleus add <name>` accepts the name, the probe adds it to the starter —
// the real command, which writes the import, the chain call and the
// configuration block — builds, boots the project with the configuration
// the command wrote, and runs the entry's wiring check against the running
// application (present when it passes). Until then the probe wires the
// capability BY HAND, the way an author does today, and partial means "it
// works, and the catalog does not do it for you".
func probeCoreEntry(t *testing.T, e *env, c coreEntry, byHand func(t *testing.T, e *env) bool) verdict {
	r := e.dryRun(c.name)
	if r.code == 0 {
		t.Logf("nucleus add %s is accepted:\n%s", c.name, firstLines(r.stdout, 8))
		return addedEntryVerdict(t, e, c)
	}
	t.Logf("nucleus add %s: %s", c.name, firstLines(r.stderr, 1))
	if byHand(t, e) {
		return partial
	}
	return absent
}

// addedEntryVerdict is the measurement once `nucleus add` knows a name: the
// real command on a copy of the starter, a build, a boot with the
// configuration the command wrote, and the check against the running
// application.
func addedEntryVerdict(t *testing.T, e *env, c coreEntry) verdict {
	e.scaffold(t)
	v := e.added(t, c.name)
	if v.add.code != 0 {
		t.Logf("nucleus add %s exited %d:\n%s", c.name, v.add.code, firstLines(v.add.all(), 8))
		return absent
	}
	t.Logf("nucleus add %s on the starter:\n%s", c.name, firstLines(v.add.stdout, 10))

	dir := v.dir
	if c.code != nil {
		dir = t.TempDir()
		must(t, copyProject(v.dir, dir))
		c.code(t, dir)
	}
	if log, err := goRun(dir, "build", "-o", exeName("app"), "."); err != nil {
		skipIfOffline(t, log)
		t.Logf("the project nucleus add %s left does not build:\n%s", c.name, log)
		return absent
	}
	raw, err := os.ReadFile(filepath.Join(dir, "nucleus.yml"))
	must(t, err)
	config := string(raw)
	if c.edit != nil {
		config = c.edit(t, e, config)
	}

	var ok bool
	evidence := "the probe has no wiring check for this entry"
	runDir := t.TempDir()
	b := bootIn(t, runDir, filepath.Join(dir, exeName("app")), config, nil, func(port int, output func() string) {
		if c.check != nil {
			ok, evidence = c.check(t, e, coreRun{port: port, dir: runDir, output: output})
		}
	})
	if !b.listening {
		t.Logf("the starter with %s does not start: %v\n%s", c.name, b.exitErr, firstLines(b.output, 10))
		return partial
	}
	if !ok {
		t.Logf("the starter boots with %s added, but the capability is not wired: %s", c.name, evidence)
		return partial
	}
	t.Logf("wired: %s", evidence)
	return present
}

const oidcConfig = "public_base_url: http://127.0.0.1:8080\nauth_federated:\n  - name: corp\n    provider: oidc\n" +
	"auth:\n  corp:\n    issuer: http://127.0.0.1:1/\n    client_id: bench\n"

// oidcPlaceholderIssuer is the issuer the oidc recipe writes; the person
// replaces it with their identity provider's, and so does the probe.
const oidcPlaceholderIssuer = "issuer: https://idp.example.com/"

// oidcSignIn is EN-01's wiring check: a sign-in, end to end, against the
// bench's stand-in identity provider. The start route has to send the
// browser to the provider with the callback the operator registers; the
// callback has to complete the flow — the code exchanged with the PKCE
// verifier, the id_token verified against the published key and the nonce —
// and answer with the identity.
func oidcSignIn(t *testing.T, e *env, run coreRun) (bool, string) {
	idp := e.idp()
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := fmt.Sprintf("http://127.0.0.1:%d", run.port)

	resp, err := browser.Get(base + "/auth/corp/start")
	if err != nil {
		return false, err.Error()
	}
	_ = resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || loc == nil || !strings.HasPrefix(loc.String(), idp.srv.URL+"/authorize") {
		return false, fmt.Sprintf("GET /auth/corp/start answered %d to %q, not a redirect to the identity provider", resp.StatusCode, resp.Header.Get("Location"))
	}
	q := loc.Query()
	callback := q.Get("redirect_uri")
	idp.expect("bench-code", q.Get("nonce"), q.Get("code_challenge"), q.Get("client_id"))

	resp, err = browser.Get(base + "/auth/corp/callback?code=bench-code")
	if err != nil {
		return false, err.Error()
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"username":"bench-user"`) || !idp.wasExchanged() {
		return false, fmt.Sprintf("GET /auth/corp/callback answered %d: %s", resp.StatusCode, firstLines(string(body), 2))
	}
	return true, fmt.Sprintf("GET /auth/corp/start redirected to the identity provider (redirect_uri %s); the callback exchanged the code "+
		"with its PKCE verifier, verified the id_token and answered 200 for bench-user", callback)
}

func probeEntryOIDC(t *testing.T, e *env) verdict {
	c := coreEntry{
		name: "oidc",
		// What the person does after `nucleus add oidc`: point the issuer
		// at their identity provider.
		edit: func(t *testing.T, e *env, config string) string {
			if !strings.Contains(config, oidcPlaceholderIssuer) {
				t.Logf("nucleus.yml carries no %q to replace; booting it as written", oidcPlaceholderIssuer)
				return config
			}
			return strings.Replace(config, oidcPlaceholderIssuer, "issuer: "+e.idp().srv.URL, 1)
		},
		check: oidcSignIn,
	}
	return probeCoreEntry(t, e, c, func(t *testing.T, e *env) bool {
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

// apiKeysAuthenticate is EN-03's wiring check: a key issued with the real
// `nucleus apikey create`, into the running application's own database, is
// accepted, a forged one is refused, and a request without one still
// passes — authentication, not a wall.
func apiKeysAuthenticate(t *testing.T, e *env, run coreRun) (bool, string) {
	// The CLI resolves sqlite://app.db against its own working directory;
	// the application runs in run.dir, so the CLI is pointed at the same
	// file by its absolute path.
	raw, err := os.ReadFile(filepath.Join(run.dir, "nucleus.yml"))
	if err != nil {
		return false, err.Error()
	}
	cliConfig := filepath.Join(t.TempDir(), "nucleus.yml")
	must(t, os.WriteFile(cliConfig, []byte(strings.Replace(string(raw), "url: sqlite://app.db", "url: sqlite://"+filepath.Join(run.dir, "app.db"), 1)), 0o644))
	r := e.cli("apikey", "create", "--config", cliConfig, "--name", "catalogbench", "--owner", "bench-program")
	key := strings.TrimSpace(r.stdout)
	if r.code != 0 || !strings.HasPrefix(key, apikeys.Prefix+"_") {
		return false, fmt.Sprintf("nucleus apikey create exited %d: %s", r.code, firstLines(r.all(), 3))
	}
	status := func(presented string) int {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/openapi.json", run.port), nil)
		if presented != "" {
			req.Header.Set(apikeys.HeaderName, presented)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	valid, forged, none := status(key), status(apikeys.Prefix+"_bogus_bogus"), status("")
	evidence := fmt.Sprintf("a key from `nucleus apikey create` answered %d, a forged one %d, no key %d", valid, forged, none)
	return valid == http.StatusOK && forged == http.StatusUnauthorized && none == http.StatusOK, evidence
}

func probeEntryAPIKeys(t *testing.T, e *env) verdict {
	return probeCoreEntry(t, e, coreEntry{name: "apikeys", check: apiKeysAuthenticate}, func(t *testing.T, e *env) bool {
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
	check := func(t *testing.T, _ *env, run coreRun) (bool, string) {
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d%s", run.port, accounts.RouteRegister), "application/json",
			strings.NewReader(`{"email":"bench@example.test","username":"bench","password":"correct horse battery staple"}`))
		if err != nil {
			return false, err.Error()
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusAccepted, fmt.Sprintf("POST %s answered %d", accounts.RouteRegister, resp.StatusCode)
	}
	return probeCoreEntry(t, e, coreEntry{name: "accounts", check: check}, func(t *testing.T, e *env) bool {
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

// benchJobsModule is the code a person writes once the queue is there: a
// module that registers a job. No catalog entry can write it — the job is
// the application's — so the probe adds it to the project `nucleus add
// sql-queue` left, the way the person would.
const benchJobsModule = `package main

import (
	"context"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func benchJobs() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "benchjobs",
		Jobs: func(j nucleus.JobRegistry, _ struct{}) {
			_ = j.Register("tick", nucleus.JobSpec{Every: 200 * time.Millisecond, Handler: func(context.Context) error { return nil }})
		},
	}.Build()
}
`

func addBenchJobs(t *testing.T, dir string) {
	must(t, os.WriteFile(filepath.Join(dir, "benchjobs.go"), []byte(benchJobsModule), 0o644))
	mainGo := filepath.Join(dir, "main.go")
	src, err := os.ReadFile(mainGo)
	must(t, err)
	edited := strings.Replace(string(src), "\t\tStart(); err != nil", "\t\tMount(benchJobs()).\n\t\tStart(); err != nil", 1)
	if edited == string(src) {
		t.Fatalf("the starter's main.go has no Start() to mount the job module before:\n%s", src)
	}
	must(t, os.WriteFile(mainGo, []byte(edited), 0o644))
}

// queueRuns is EN-05's wiring check: the job a module registered runs on the
// durable queue — its runs are rows of the queue table in the application's
// own database, finished — and the application says which provider it
// scheduled them on. On the in-process queue the job would run too, and the
// table would not exist: the rows are what tell the two apart.
func queueRuns(t *testing.T, _ *env, run coreRun) (bool, string) {
	path := filepath.Join(run.dir, "app.db")
	deadline := time.Now().Add(15 * time.Second)
	var done int
	var lastErr error
	for time.Now().Before(deadline) {
		done, lastErr = countDoneJobs(path)
		if lastErr == nil && done > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	scheduled := strings.Contains(run.output(), `msg="nucleus: module jobs scheduled" provider=sql`)
	evidence := fmt.Sprintf("finished runs in nucleus_jobs: %d (err=%v); the boot log names provider=sql: %v", done, lastErr, scheduled)
	return done > 0 && scheduled, evidence
}

func countDoneJobs(path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	var n int
	err = db.QueryRow("SELECT COUNT(*) FROM nucleus_jobs WHERE status = 'done'").Scan(&n)
	return n, err
}

func probeEntrySQLQueue(t *testing.T, e *env) verdict {
	return probeCoreEntry(t, e, coreEntry{name: "sql-queue", code: addBenchJobs, check: queueRuns}, func(t *testing.T, e *env) bool {
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
	// No wiring check yet: the session that teaches `nucleus add` this name
	// writes it (a frame delivered over the route the recipe mounts).
	return probeCoreEntry(t, e, coreEntry{name: "websockets"}, func(t *testing.T, e *env) bool {
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
			return addedEntryVerdict(t, e, coreEntry{name: n})
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

// probeEntrySentry measures EN-09 on the real path once `nucleus add sentry`
// knows the name: the command on the starter (the module fetched and
// imported, the http_interceptors block written), the DSN the person sets —
// here the stand-in Sentry's — routes that fail the way an application's
// do, and the wiring check: a handler's error and a panic arrive as events.
func probeEntrySentry(t *testing.T, e *env) verdict {
	if r := e.dryRun("sentry"); r.code == 0 {
		t.Logf("nucleus add sentry is accepted:\n%s", firstLines(r.stdout, 6))
		return addedEntryVerdict(t, e, coreEntry{
			name: "sentry",
			edit: func(t *testing.T, e *env, config string) string {
				if !strings.Contains(config, sentryEmptyDSN) {
					t.Logf("nucleus.yml carries no %q to replace; booting it as written", sentryEmptyDSN)
					return config
				}
				return strings.Replace(config, sentryEmptyDSN, "dsn: "+e.sentry().dsn(), 1)
			},
			code:  addBenchFailures,
			check: sentryReports,
		})
	}
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
