// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `nucleus new` used to leave go.mod one require short: the generated
// main.go imports a driver module the go.mod never mentions, so `go mod
// tidy` was a mandatory human step before the first `go run .`. The
// scaffold now resolves the driver itself (go get + go mod tidy) unless
// --offline asks it not to, and --db picks which engine that is.
package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubScaffoldNetwork replaces the two commands that touch the module
// proxy with recorders, so the sequence is proven without a network.
func stubScaffoldNetwork(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	prevGet, prevTidy := goGet, goModTidy
	goGet = func(root, module string, _, _ io.Writer) error {
		calls = append(calls, "go get "+module+" in "+filepath.Base(root))
		return nil
	}
	goModTidy = func(root string, _, _ io.Writer) error {
		calls = append(calls, "go mod tidy in "+filepath.Base(root))
		return nil
	}
	t.Cleanup(func() { goGet, goModTidy = prevGet, prevTidy })
	return &calls
}

func TestRunNewResolvesDriverUnlessOffline(t *testing.T) {
	t.Run("default: go get the driver, then go mod tidy, in the project", func(t *testing.T) {
		calls := stubScaffoldNetwork(t)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"tidyapp", "--out", outDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
		}
		want := []string{
			"go get github.com/jcsvwinston/nucleus/drivers/sqlite in tidyapp",
			"go mod tidy in tidyapp",
		}
		if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
			t.Errorf("scaffold network sequence:\n got %q\nwant %q", *calls, want)
		}
		if strings.Contains(stdout.String(), "skipped by --offline") {
			t.Errorf("the next steps must not hand `go mod tidy` back when the scaffold ran it:\n%s", stdout.String())
		}
		if !strings.Contains(stdout.String(), "nucleus generate module notes --mount") {
			t.Errorf("the next steps must name the generated-module path:\n%s", stdout.String())
		}
	})

	t.Run("--offline: no network, and the two commands are handed back", func(t *testing.T) {
		calls := stubScaffoldNetwork(t)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"airgap", "--out", outDir, "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew --offline: %v\nstderr: %s", err, stderr.String())
		}
		if len(*calls) != 0 {
			t.Errorf("--offline must not run go get or go mod tidy, ran %q", *calls)
		}
		if !strings.Contains(stdout.String(), "go get github.com/jcsvwinston/nucleus/drivers/sqlite && go mod tidy   # skipped by --offline") {
			t.Errorf("--offline must hand the skipped commands back as a next step:\n%s", stdout.String())
		}
	})

	t.Run("a failing go get is reported with the --offline escape", func(t *testing.T) {
		stubScaffoldNetwork(t)
		goGet = func(string, string, io.Writer, io.Writer) error { return os.ErrDeadlineExceeded }
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := runNew([]string{"noproxy", "--out", outDir}, strings.NewReader(""), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "go get github.com/jcsvwinston/nucleus/drivers/sqlite") || !strings.Contains(err.Error(), "--offline") {
			t.Errorf("want the go get failure naming the module and the --offline escape, got %v", err)
		}
	})
}

// --db selects the engine the project starts on: the driver module the
// generated main.go imports (and the scaffold resolves) and the URL
// nucleus.yml begins with. The human spellings `nucleus add` accepts work
// here too, and an unknown engine lists the supported ones.
func TestRunNewDBFlag(t *testing.T) {
	cases := []struct {
		db     string
		module string
		url    string
	}{
		{"sqlite", "github.com/jcsvwinston/nucleus/drivers/sqlite", "sqlite://app.db"},
		{"postgres", "github.com/jcsvwinston/nucleus/drivers/postgres", "postgres://"},
		{"postgresql", "github.com/jcsvwinston/nucleus/drivers/postgres", "postgres://"},
		{"mysql", "github.com/jcsvwinston/nucleus/drivers/mysql", "mysql://"},
		{"mssql", "github.com/jcsvwinston/nucleus/drivers/mssql", "sqlserver://"},
		{"sqlserver", "github.com/jcsvwinston/nucleus/drivers/mssql", "sqlserver://"},
		{"oracle", "github.com/jcsvwinston/nucleus/drivers/oracle", "oracle://"},
	}
	for _, c := range cases {
		t.Run(c.db, func(t *testing.T) {
			calls := stubScaffoldNetwork(t)
			for _, tmpl := range []string{"mvc", "api"} {
				outDir := t.TempDir()
				var stdout, stderr bytes.Buffer
				if err := runNew([]string{"dbapp", "--out", outDir, "--template", tmpl, "--db", c.db}, strings.NewReader(""), &stdout, &stderr); err != nil {
					t.Fatalf("runNew --db %s (%s): %v\nstderr: %s", c.db, tmpl, err, stderr.String())
				}
				projectDir := filepath.Join(outDir, "dbapp")
				mainSrc := stripComments(readFile(t, filepath.Join(projectDir, "main.go")))
				if !strings.Contains(mainSrc, "_ \""+c.module+"\"") {
					t.Errorf("%s: main.go must import %s:\n%s", tmpl, c.module, mainSrc)
				}
				if strings.Contains(mainSrc, "drivers/sqlite") && c.db != "sqlite" {
					t.Errorf("%s: main.go still imports the sqlite driver for --db %s:\n%s", tmpl, c.db, mainSrc)
				}
				cfg := readFile(t, filepath.Join(projectDir, "nucleus.yml"))
				if !strings.Contains(cfg, "url: "+c.url) {
					t.Errorf("%s: nucleus.yml must start on %s:\n%s", tmpl, c.url, cfg)
				}
				if !strings.Contains(stdout.String(), "(template: "+tmpl+", database: "+filepath.Base(c.module)+")") && !strings.Contains(stdout.String(), "(template: "+tmpl+", database: sqlserver)") {
					t.Errorf("%s: the summary must name the engine:\n%s", tmpl, stdout.String())
				}
			}
			if len(*calls) == 0 || !strings.Contains((*calls)[0], "go get "+c.module) {
				t.Errorf("the scaffold must go get the %s driver, ran %q", c.db, *calls)
			}
		})
	}

	t.Run("unknown engine", func(t *testing.T) {
		stubScaffoldNetwork(t)
		var stdout, stderr bytes.Buffer
		err := runNew([]string{"dbapp", "--out", t.TempDir(), "--db", "cockroach"}, strings.NewReader(""), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), `unsupported --db "cockroach"`) || !strings.Contains(err.Error(), "mysql, oracle, postgres, sqlite, sqlserver") {
			t.Errorf("want an error listing the supported engines, got %v", err)
		}
	})
}
