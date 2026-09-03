// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ensureBlankImport is the half of `nucleus add` that edits someone's source
// file, so it is the half that has to be beyond doubt: a tool that mangles a
// main.go is worse than no tool.
func TestEnsureBlankImport(t *testing.T) {
	const mod = "github.com/jcsvwinston/nucleus/drivers/postgres"

	cases := []struct {
		name  string
		src   string
		want  []string
		added bool
	}{
		{
			name:  "appends to an existing block, keeping the grouping",
			src:   "package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() { fmt.Println(1) }\n",
			want:  []string{"\"fmt\"", "_ \"" + mod + "\""},
			added: true,
		},
		{
			name:  "creates a block when the file has none",
			src:   "package main\n\nfunc main() {}\n",
			want:  []string{"import (", "_ \"" + mod + "\""},
			added: true,
		},
		{
			name:  "converts nothing when a single-line import exists",
			src:   "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(1) }\n",
			want:  []string{"\"fmt\"", "_ \"" + mod + "\""},
			added: true,
		},
		{
			name:  "is idempotent",
			src:   "package main\n\nimport (\n\t_ \"" + mod + "\"\n)\n",
			want:  []string{"_ \"" + mod + "\""},
			added: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(path, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			added, err := ensureBlankImport(path, mod)
			if err != nil {
				t.Fatalf("ensureBlankImport: %v", err)
			}
			if added != c.added {
				t.Errorf("added = %v, want %v", added, c.added)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.want {
				if !strings.Contains(string(got), w) {
					t.Errorf("result is missing %q:\n%s", w, got)
				}
			}
			// Whatever the shape, the result has to still be Go.
			if n := strings.Count(string(got), "_ \""+mod+"\""); n != 1 {
				t.Errorf("the import appears %d times, want exactly 1:\n%s", n, got)
			}
		})
	}
}

// Adding a second module must not create a second import block.
func TestEnsureBlankImport_TwoModulesShareOneBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() { fmt.Println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{
		"github.com/jcsvwinston/nucleus/drivers/postgres",
		"github.com/jcsvwinston/nucleus/providers/storage-s3",
	} {
		if _, err := ensureBlankImport(path, m); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(got), "import ("); n != 1 {
		t.Errorf("expected one import block, got %d:\n%s", n, got)
	}
}

// People write the flag after the module name. The parser has to cope.
func TestSplitFlagsAndArgs(t *testing.T) {
	for _, c := range []struct {
		args      []string
		wantFlags []string
		wantPos   []string
	}{
		{[]string{"postgres", "--dry-run"}, []string{"--dry-run"}, []string{"postgres"}},
		{[]string{"--dry-run", "postgres"}, []string{"--dry-run"}, []string{"postgres"}},
		{[]string{"s3", "--into", "cmd/server/main.go"}, []string{"--into", "cmd/server/main.go"}, []string{"s3"}},
		{[]string{"--into=x.go", "mysql", "s3"}, []string{"--into=x.go"}, []string{"mysql", "s3"}},
	} {
		flags, pos := splitFlagsAndArgs(c.args)
		if strings.Join(flags, " ") != strings.Join(c.wantFlags, " ") {
			t.Errorf("%v: flags = %v, want %v", c.args, flags, c.wantFlags)
		}
		if strings.Join(pos, " ") != strings.Join(c.wantPos, " ") {
			t.Errorf("%v: positional = %v, want %v", c.args, pos, c.wantPos)
		}
	}
}

// The names people type are not the names the framework keys on: nobody
// writes "pgx" or "sqlserver" when they mean postgres or SQL Server.
func TestLookupAddableAcceptsHumanNames(t *testing.T) {
	for name, wantModule := range map[string]string{
		"postgres":   "github.com/jcsvwinston/nucleus/drivers/postgres",
		"postgresql": "github.com/jcsvwinston/nucleus/drivers/postgres",
		"pgx":        "github.com/jcsvwinston/nucleus/drivers/postgres",
		"mssql":      "github.com/jcsvwinston/nucleus/drivers/mssql",
		"sqlserver":  "github.com/jcsvwinston/nucleus/drivers/mssql",
		"SQLite":     "github.com/jcsvwinston/nucleus/drivers/sqlite",
		"s3":         "github.com/jcsvwinston/nucleus/providers/storage-s3",
		"ldap":       "github.com/jcsvwinston/nucleus/providers/ldap",
		// The startup line says `nucleus add prometheus`; the command has
		// to keep that promise (ADR-031).
		"prometheus": "github.com/jcsvwinston/nucleus/exporters/prometheus",
		"otlp":       "github.com/jcsvwinston/nucleus/exporters/otlp",
	} {
		p, ok := lookupAddable(name)
		if !ok {
			t.Errorf("%q was not resolved", name)
			continue
		}
		if p.Module != wantModule {
			t.Errorf("%q → %q, want %q", name, p.Module, wantModule)
		}
	}
	if _, ok := lookupAddable("mongodb"); ok {
		t.Error("a module this project does not publish must not resolve: the error's promise is that `go get` works")
	}
}
