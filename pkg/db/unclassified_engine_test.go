// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

// recordingHandler keeps every record logged through it, so a test can count
// the warnings New emitted and read their attributes.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler            { return h }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

// warnings returns the attributes of every WARN record, keyed by name.
func (h *recordingHandler) warnings() []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []map[string]string
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		attrs := map[string]string{"msg": r.Message}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		out = append(out, attrs)
	}
	return out
}

func newRecordingLogger() (*slog.Logger, *recordingHandler) {
	h := &recordingHandler{}
	return slog.New(h), h
}

// forgetWarned lets a test that runs under -count=N warn again: the
// once-per-engine memory is process-wide by design.
func forgetWarned(t *testing.T, engines ...string) {
	t.Helper()
	forget := func() {
		for _, e := range engines {
			warnedUnclassified.Delete(e)
		}
	}
	forget()
	t.Cleanup(forget)
}

// withClassifiers stands in for a registry holding exactly engines, for the
// duration of the test.
func withClassifiers(t *testing.T, engines ...string) {
	t.Helper()
	prev := hasClassifier
	hasClassifier = func(e string) bool {
		for _, have := range engines {
			if have == e {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { hasClassifier = prev })
}

// fakeSQLDriver is a database/sql driver that opens without a server: enough
// for New to open and ping, which is all the warning depends on.
type fakeSQLDriver struct{}

func (fakeSQLDriver) Open(string) (sqldriver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

var errFakeConn = errors.New("fake driver: no statements")

func (fakeConn) Prepare(string) (sqldriver.Stmt, error) { return nil, errFakeConn }
func (fakeConn) Close() error                           { return nil }
func (fakeConn) Begin() (sqldriver.Tx, error)           { return nil, errFakeConn }

// wrappingSQLDriver delegates to another driver, the shape of the test kit's
// per-test transactional driver and of any instrumenting wrapper: it is
// registered under a name of its own and hands back the inner driver's errors.
type wrappingSQLDriver struct{ inner sqldriver.Driver }

func (w wrappingSQLDriver) Open(dsn string) (sqldriver.Conn, error) { return w.inner.Open(dsn) }

const (
	// fakeDriverName is a database/sql driver an application could import
	// directly — the case the warning exists for.
	fakeDriverName = "fake-direct-import"
	// fakeEngine and fakeClassifiedEngine are engine names no real driver
	// uses, the first left without a classifier and the second given one.
	fakeEngine           = "fake-unclassified-engine"
	fakeClassifiedEngine = "fake-classified-engine"
	// wrappedSQLiteName wraps the SQLite driver the test binary links.
	wrappedSQLiteName = "fake-wrapped-sqlite"
)

var registerFakesOnce sync.Once

// registerFakes registers the fake drivers and the one fake classifier once
// per process, so the tests also pass under -count=N.
func registerFakes(t *testing.T) {
	t.Helper()
	registerFakesOnce.Do(func() {
		sql.Register(fakeDriverName, fakeSQLDriver{})
		sql.Register(fakeEngine, fakeSQLDriver{})
		sql.Register(fakeClassifiedEngine, fakeSQLDriver{})
		driver.MustRegisterUniqueViolation(fakeClassifiedEngine, func(error) bool { return false })

		probe, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			panic(err)
		}
		sql.Register(wrappedSQLiteName, wrappingSQLDriver{inner: probe.Driver()})
		_ = probe.Close()
	})
}

// TestNew_WarnsOnceForAnEngineWithoutClassifier is the case IsUniqueViolation
// documents: the application imports a driver itself instead of the nucleus
// module, never registers a classifier, and opens a database on it. The
// database opens — the warning is a log line, not a failure — and the log
// says, once, which engine is unclassified and what to import.
func TestNew_WarnsOnceForAnEngineWithoutClassifier(t *testing.T) {
	registerFakes(t)
	withClassifiers(t) // a binary that registered no classifier at all
	forgetWarned(t, "mysql")
	logger, rec := newRecordingLogger()

	cfg := Config{
		DatabaseURL: "mysql://app:secret@127.0.0.1:1/app",
		DriverName:  fakeDriverName,
	}
	for i := 0; i < 2; i++ { // a second alias on the same engine
		d, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("New must open the database and only warn; got %v", err)
		}
		_ = d.Close()
	}

	warns := rec.warnings()
	if len(warns) != 1 {
		t.Fatalf("want exactly one WARN for the unclassified engine, got %d: %v", len(warns), warns)
	}
	w := warns[0]
	if w["engine"] != "mysql" {
		t.Errorf("engine = %q, want %q", w["engine"], "mysql")
	}
	if w["driver"] != fakeDriverName {
		t.Errorf("driver = %q, want %q", w["driver"], fakeDriverName)
	}
	for _, want := range []string{
		`import _ "github.com/jcsvwinston/nucleus/drivers/mysql"`,
		"nucleus add mysql",
		`driver.RegisterUniqueViolation("mysql", fn)`,
	} {
		if !strings.Contains(w["fix"], want) {
			t.Errorf("fix %q does not name %q", w["fix"], want)
		}
	}
	if !strings.Contains(w["msg"], "IsUniqueViolation") {
		t.Errorf("message %q does not name db.IsUniqueViolation", w["msg"])
	}
}

// TestWarnUnclassifiedEngine_FakeEngine drives the check with an engine name
// no real driver uses: a database/sql driver is registered under it and no
// classifier is, so the warning names it — once — and tells the caller to
// register one, since there is no nucleus module to import for it.
func TestWarnUnclassifiedEngine_FakeEngine(t *testing.T) {
	registerFakes(t)
	forgetWarned(t, fakeEngine)
	logger, rec := newRecordingLogger()

	if driver.HasEngine(fakeEngine) {
		t.Fatalf("precondition: %q must have no classifier", fakeEngine)
	}
	warnUnclassifiedEngine(logger, fakeEngine, "")
	warnUnclassifiedEngine(logger, fakeEngine, "")

	warns := rec.warnings()
	if len(warns) != 1 {
		t.Fatalf("want exactly one WARN, got %d: %v", len(warns), warns)
	}
	if warns[0]["engine"] != fakeEngine {
		t.Errorf("engine = %q, want %q", warns[0]["engine"], fakeEngine)
	}
	if _, set := warns[0]["driver"]; set {
		t.Errorf("no replacement driver was configured, yet the WARN names one: %v", warns[0])
	}
	if want := `driver.RegisterUniqueViolation("` + fakeEngine + `", fn)`; !strings.Contains(warns[0]["fix"], want) {
		t.Errorf("fix %q does not name %q", warns[0]["fix"], want)
	}
	if strings.Contains(warns[0]["fix"], "import _") {
		t.Errorf("fix %q points at a module for an engine nucleus does not publish", warns[0]["fix"])
	}
}

// TestWarnUnclassifiedEngine_SilentWhenClassified pins the other half: a
// registered classifier — under the engine, or under the driver that replaces
// it — and PostgreSQL, which needs none, produce no warning.
func TestWarnUnclassifiedEngine_SilentWhenClassified(t *testing.T) {
	registerFakes(t)
	forgetWarned(t, fakeClassifiedEngine, fakeEngine, "pgx")
	logger, rec := newRecordingLogger()

	if !driver.HasEngine(fakeClassifiedEngine) {
		t.Fatalf("precondition: %q must have a classifier", fakeClassifiedEngine)
	}
	warnUnclassifiedEngine(logger, fakeClassifiedEngine, "")
	// A replacement driver its importer classified under its own name.
	warnUnclassifiedEngine(logger, fakeEngine, fakeClassifiedEngine)

	// PostgreSQL is read through the SQLState() method of any driver.
	withClassifiers(t)
	warnUnclassifiedEngine(logger, "pgx", "")
	warnUnclassifiedEngine(logger, "pgx", "postgres") // lib/pq

	if warns := rec.warnings(); len(warns) != 0 {
		t.Fatalf("want no WARN when the engine is classified, got %v", warns)
	}
}

// TestNew_NoWarningForClassifiedSQLite opens SQLite the two ways the
// framework's own users do — the stock driver, and a driver that wraps it
// under another name, as the test kit's per-test transactional database
// does — and expects silence: the binary registers SQLite's classifier, and a
// wrapper hands back the errors of the driver it wraps.
func TestNew_NoWarningForClassifiedSQLite(t *testing.T) {
	registerFakes(t)
	if !driver.HasEngine("sqlite") {
		t.Fatal("precondition: the test binary registers SQLite's classifier")
	}
	forgetWarned(t, "sqlite")
	logger, rec := newRecordingLogger()

	for _, cfg := range []Config{
		{DatabaseURL: "sqlite://:memory:"},
		{DatabaseURL: "sqlite://:memory:", DriverName: wrappedSQLiteName},
	} {
		d, err := New(cfg, logger)
		if err != nil {
			t.Fatalf("New(%+v): %v", cfg, err)
		}
		_ = d.Close()
	}
	if warns := rec.warnings(); len(warns) != 0 {
		t.Fatalf("want no WARN for a classified engine, got %v", warns)
	}
}

// TestOfficialDriverModules_RegisterWhatNewChecks keeps the five driver
// modules silent: the name each one registers its classifier under must be
// the name New checks for that engine's URL, or an application importing the
// module would be warned about a gap it does not have. It reads the
// registration from each module's source, since the root module cannot import
// them.
func TestOfficialDriverModules_RegisterWhatNewChecks(t *testing.T) {
	registerCall := regexp.MustCompile(`RegisterUniqueViolation\("([^"]+)"`)
	names := knownproviders.DBDriverNames()
	if len(names) != 5 {
		t.Fatalf("expected the five published driver modules, got %v", names)
	}
	for _, key := range names {
		p, _ := knownproviders.DBDriver(key)
		t.Run(p.Name, func(t *testing.T) {
			url := strings.TrimSpace(strings.TrimPrefix(p.Selects, "databases.default.url:"))
			engine, _, err := resolveDriver(url)
			if err != nil {
				t.Fatalf("resolve %q: %v", url, err)
			}
			if engine != p.Key {
				t.Fatalf("%s resolves to driver %q, but the module is catalogued under %q", url, engine, p.Key)
			}

			dir := filepath.Join("..", "..", strings.TrimPrefix(p.Module, knownproviders.RepoModule+"/"))
			files, err := filepath.Glob(filepath.Join(dir, "*.go"))
			if err != nil || len(files) == 0 {
				t.Fatalf("no Go source in %s (err %v)", dir, err)
			}
			var registered []string
			for _, f := range files {
				if strings.HasSuffix(f, "_test.go") {
					continue
				}
				src, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range registerCall.FindAllSubmatch(src, -1) {
					registered = append(registered, string(m[1]))
				}
			}

			withClassifiers(t, registered...)
			if !classifierCovers(engine, "") {
				t.Errorf("%s registers classifiers under %v, but New checks %q for %s — an application importing it would be warned", p.Module, registered, engine, url)
			}
		})
	}
}
