// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// What `generate model`, `generate resource` and `startapp` hand the user
// since NU-137: a Quark model and a Quark repository, and no import of
// pkg/model anywhere — the package leaves the public API at v2.0.0
// (DEP-2026-024). --data sql keeps a database/sql repository over the same
// model.
package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertNoPkgModelImport walks every Go file the generators wrote under
// internal/ and fails on an import of pkg/model.
func assertNoPkgModelImport(t *testing.T, projectDir string) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(projectDir, "internal"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if strings.Contains(readFile(t, p), `nucleus/pkg/model"`) {
			t.Errorf("%s imports pkg/model; the generators must not hand out the deprecated package (NU-137)", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// assertQuarkModel pins the shape of the generated model: Quark's tags,
// the four conventional columns and TableName pinned to the table.
func assertQuarkModel(t *testing.T, path, pascal, table string) {
	t.Helper()
	src := readFile(t, path)
	for _, want := range []string{
		"type " + pascal + " struct {",
		"`db:\"id\" pk:\"true\" json:\"id\"`",
		"`db:\"name\" quark:\"not_null\" json:\"name\"`",
		"`db:\"created_at\" json:\"created_at\"`",
		"`db:\"updated_at\" json:\"updated_at\"`",
		"DeletedAt *time.Time `db:\"deleted_at\" json:\"-\"`",
		"func (" + pascal + ") TableName() string { return \"" + table + "\" }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("%s is missing %q:\n%s", path, want, src)
		}
	}
	for _, reject := range []string{"model.BaseModel", "pkg/model", "column:", "validate:"} {
		if strings.Contains(src, reject) {
			t.Errorf("%s still carries %q:\n%s", path, reject, src)
		}
	}
}

func TestGenerateModelWritesAQuarkModel(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "qmodel")
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"model", "Category", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate model: %v\nstderr: %s", err, stderr.String())
	}
	assertQuarkModel(t, filepath.Join(projectDir, "internal", "models", "category.go"), "Category", "categories")
	if !strings.Contains(stdout.String(), "Model scaffold created:") {
		t.Errorf("unexpected output:\n%s", stdout.String())
	}
}

func TestGenerateResourceWritesQuarkModelAndRepository(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "qres")
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"resource", "widget", "--out", projectDir, "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate resource: %v\nstderr: %s", err, stderr.String())
	}
	assertQuarkModel(t, filepath.Join(projectDir, "internal", "models", "widget.go"), "Widget", "widgets")
	assertNoPkgModelImport(t, projectDir)

	repo := readFile(t, filepath.Join(projectDir, "internal", "repositories", "widget_repository.go"))
	for _, want := range []string{
		`"example.com/qres/internal/models"`,
		`"github.com/jcsvwinston/quark"`,
		`_ "github.com/jcsvwinston/quark/drivers/sqlite"`,
		"type WidgetRecord = models.Widget",
		`quark.NewWithDB("sqlite", db)`,
		"quark.For[WidgetRecord]",
		"func NewWidgetRepository(db *sql.DB) (*WidgetRepository, error)",
	} {
		if !strings.Contains(repo, want) {
			t.Errorf("quark repository is missing %q:\n%s", want, repo)
		}
	}
	if strings.Contains(repo, "QueryContext") || strings.Contains(repo, "LastInsertId") {
		t.Errorf("quark repository must not fall back to hand-written SQL:\n%s", repo)
	}
	module := readFile(t, filepath.Join(projectDir, "internal", "modules", "widget_module.go"))
	if !strings.Contains(module, "repository, err := repositories.NewWidgetRepository(db)") || !strings.Contains(module, "quark client over the managed pool") {
		t.Errorf("module must handle the quark client derivation error:\n%s", module)
	}
	// --offline hands back the tidy that resolves Quark and its driver.
	out := stdout.String()
	if !strings.Contains(out, "repository (quark):") || !strings.Contains(out, "go mod tidy   # skipped by --offline") || !strings.Contains(out, "github.com/jcsvwinston/quark/drivers/sqlite") {
		t.Errorf("the generator must name the quark repository and hand back the tidy, got:\n%s", out)
	}
}

func TestGenerateResourceDataSQLKeepsDatabaseSQLOverTheQuarkModel(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "sqlres")
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"resource", "gizmo", "--out", projectDir, "--data", "sql"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate resource --data sql: %v\nstderr: %s", err, stderr.String())
	}
	assertQuarkModel(t, filepath.Join(projectDir, "internal", "models", "gizmo.go"), "Gizmo", "gizmos")
	assertNoPkgModelImport(t, projectDir)
	repo := readFile(t, filepath.Join(projectDir, "internal", "repositories", "gizmo_repository.go"))
	for _, want := range []string{`"example.com/sqlres/internal/models"`, "type GizmoRecord = models.Gizmo", "QueryContext", "func NewGizmoRepository(db *sql.DB) *GizmoRepository"} {
		if !strings.Contains(repo, want) {
			t.Errorf("sql repository is missing %q:\n%s", want, repo)
		}
	}
	if strings.Contains(repo, "jcsvwinston/quark") {
		t.Errorf("--data sql must not import Quark:\n%s", repo)
	}
	module := readFile(t, filepath.Join(projectDir, "internal", "modules", "gizmo_module.go"))
	if !strings.Contains(module, "service = services.NewGizmoService(repositories.NewGizmoRepository(db))") {
		t.Errorf("sql module must construct the repository in one line:\n%s", module)
	}
	// Nothing new to resolve: no tidy, no hand-back.
	if strings.Contains(stdout.String(), "go mod tidy") {
		t.Errorf("--data sql imports nothing the project lacks; no tidy expected, got:\n%s", stdout.String())
	}
}

// The Quark resource ends with the tidy that resolves Quark, once, like
// `generate module`; --offline skips it; a failing tidy leaves the files
// in place and names the escape.
func TestGenerateResourceTidiesForQuarkUnlessOffline(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "tidyres")
	calls := stubScaffoldNetwork(t)
	var stdout, stderr bytes.Buffer
	if err := runGenerate([]string{"resource", "note", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate: %v\nstderr: %s", err, stderr.String())
	}
	if want := []string{"go mod tidy in tidyres"}; strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("generate resource must run go mod tidy in the project once, ran %q", *calls)
	}
	*calls = nil
	stdout.Reset()
	if err := runGenerate([]string{"resource", "tag", "--out", projectDir, "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate --offline: %v\nstderr: %s", err, stderr.String())
	}
	if len(*calls) != 0 || !strings.Contains(stdout.String(), "go mod tidy   # skipped by --offline") {
		t.Errorf("--offline must not run go mod tidy and must hand it back, ran %q, got:\n%s", *calls, stdout.String())
	}
	goModTidy = func(string, io.Writer, io.Writer) error { return os.ErrDeadlineExceeded }
	err := runGenerate([]string{"resource", "label", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "go mod tidy") || !strings.Contains(err.Error(), "--offline") {
		t.Errorf("a failing tidy must name the command and the --offline escape, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, "internal", "modules", "label_module.go")); statErr != nil {
		t.Errorf("the resource must stay in place when only the tidy failed: %v", statErr)
	}
}

func TestStartAppWritesQuarkModelAndRepository(t *testing.T) {
	projectDir := scaffoldOfflineProject(t, "qapp2")
	calls := stubScaffoldNetwork(t)
	var stdout, stderr bytes.Buffer
	if err := runStartApp([]string{"billing", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runStartApp: %v\nstderr: %s", err, stderr.String())
	}
	assertQuarkModel(t, filepath.Join(projectDir, "internal", "models", "billing.go"), "Billing", "billings")
	assertNoPkgModelImport(t, projectDir)
	repo := readFile(t, filepath.Join(projectDir, "internal", "repositories", "billing_repository.go"))
	for _, want := range []string{`"github.com/jcsvwinston/quark"`, "type BillingRecord = models.Billing", "quark.For[BillingRecord]"} {
		if !strings.Contains(repo, want) {
			t.Errorf("startapp quark repository is missing %q:\n%s", want, repo)
		}
	}
	module := readFile(t, filepath.Join(projectDir, "internal", "modules", "billing_module.go"))
	if !strings.Contains(module, "repository, err := repositories.NewBillingRepository(db)") {
		t.Errorf("startapp module must handle the quark client derivation error:\n%s", module)
	}
	if want := []string{"go mod tidy in qapp2"}; strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("startapp must run go mod tidy in the project once, ran %q", *calls)
	}

	// --data sql: database/sql repository over the same model, no tidy.
	*calls = nil
	stdout.Reset()
	if err := runStartApp([]string{"invoice", "--out", projectDir, "--data", "sql"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runStartApp --data sql: %v\nstderr: %s", err, stderr.String())
	}
	assertQuarkModel(t, filepath.Join(projectDir, "internal", "models", "invoice.go"), "Invoice", "invoices")
	repo = readFile(t, filepath.Join(projectDir, "internal", "repositories", "invoice_repository.go"))
	if !strings.Contains(repo, "type InvoiceRecord = models.Invoice") || !strings.Contains(repo, "QueryContext") || strings.Contains(repo, "jcsvwinston/quark") {
		t.Errorf("startapp --data sql must write a database/sql repository over the model:\n%s", repo)
	}
	if len(*calls) != 0 {
		t.Errorf("--data sql imports nothing the project lacks; no tidy expected, ran %q", *calls)
	}
	// --offline on the default hands the tidy back.
	stdout.Reset()
	if err := runStartApp([]string{"receipt", "--out", projectDir, "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runStartApp --offline: %v\nstderr: %s", err, stderr.String())
	}
	if len(*calls) != 0 || !strings.Contains(stdout.String(), "go mod tidy   # skipped by --offline") || !strings.Contains(stdout.String(), "github.com/jcsvwinston/quark/drivers/sqlite") {
		t.Errorf("--offline must hand back the tidy that resolves quark and its driver, ran %q, got:\n%s", *calls, stdout.String())
	}
	if err := runStartApp([]string{"ledger", "--out", projectDir, "--data", "gorm"}, strings.NewReader(""), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "unsupported --data") {
		t.Errorf("startapp --data gorm: want an unsupported-value error, got %v", err)
	}
}
