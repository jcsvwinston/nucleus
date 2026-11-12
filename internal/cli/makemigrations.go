// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

// `nucleus makemigrations <name>` renders the up/down pair from the
// project's Quark models (DX-N3n2). Nucleus does not read the structs
// itself: the migration a model implies is Quark's business, and its CLI
// prints the DDL as data — `quark migrate create <name> --from-models <dir>
// --dialect <d> --emit json`, one JSON document on stdout — so the two
// binaries share a contract (its `schema` field), not a Go type. This file
// finds that CLI, runs it from the project, and writes what it printed in
// the framework's own migration format.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const (
	// quarkCLIEnv pins the quark CLI makemigrations runs: a path to the
	// executable, or a name resolved on PATH.
	quarkCLIEnv = "NUCLEUS_QUARK_CLI"
	// quarkCLIModule is the module `go run` fetches when no quark CLI is
	// on PATH.
	quarkCLIModule = "github.com/jcsvwinston/quark/cmd/quark"
	// quarkCLIMinVersion is the first release of cmd/quark whose `migrate
	// create` has --emit json. An older CLI is reported, not worked around.
	quarkCLIMinVersion = "v1.3.0"
	// ddlDocumentSchema is the version of the JSON contract this reader
	// understands (quark's commands.DDLDocumentSchema). Quark bumps it only
	// on an incompatible change, so a document with another value is one
	// this nucleus cannot read, and says so.
	ddlDocumentSchema = 1
	// projectModelsDir is where the generators write Quark models and where
	// makemigrations looks for them, relative to the project root.
	projectModelsDir = "internal/models"
)

// quarkCLI is how makemigrations invokes the quark CLI: an executable and
// the arguments that precede quark's own (`run <module>@latest` for go).
type quarkCLI struct {
	bin  string
	args []string
	// how says where the CLI came from, for messages.
	how string
}

// String is the command line as the user would type it.
func (c quarkCLI) String() string {
	return strings.Join(append([]string{c.bin}, c.args...), " ")
}

// resolveQuarkCLI finds the quark CLI: NUCLEUS_QUARK_CLI when set (a path,
// or a name on PATH), else `quark` on PATH, else `go run` of the published
// module — which downloads it, so the caller says so before running.
func resolveQuarkCLI(getenv func(string) string) (quarkCLI, error) {
	if pinned := strings.TrimSpace(getenv(quarkCLIEnv)); pinned != "" {
		if strings.ContainsAny(pinned, `/\`) {
			abs, err := filepath.Abs(pinned)
			if err != nil {
				return quarkCLI{}, fmt.Errorf("%s=%q: %w", quarkCLIEnv, pinned, err)
			}
			if st, err := os.Stat(abs); err != nil || st.IsDir() {
				return quarkCLI{}, fmt.Errorf("%s=%q is not an executable file", quarkCLIEnv, pinned)
			}
			return quarkCLI{bin: abs, how: quarkCLIEnv}, nil
		}
		path, err := exec.LookPath(pinned)
		if err != nil {
			return quarkCLI{}, fmt.Errorf("%s=%q is not on PATH: %w", quarkCLIEnv, pinned, err)
		}
		return quarkCLI{bin: path, how: quarkCLIEnv}, nil
	}
	if path, err := exec.LookPath("quark"); err == nil {
		return quarkCLI{bin: path, how: "PATH"}, nil
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return quarkCLI{}, fmt.Errorf("no quark CLI: put `quark` on PATH (go install %s@latest) or point %s at one; without the go tool it cannot be fetched either", quarkCLIModule, quarkCLIEnv)
	}
	return quarkCLI{bin: goBin, args: []string{"run", quarkCLIModule + "@latest"}, how: "go run"}, nil
}

// ddlDocument is the JSON `quark migrate create --emit json` prints: the
// statements as data. Field names are quark's contract (DDLDocument in
// cmd/quark/commands); a reader of schema 1 keeps decoding fields a later
// CLI adds, and stops at a schema it does not know.
type ddlDocument struct {
	Schema  int    `json:"schema"`
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Dialect string `json:"dialect"`
	Models  []struct {
		Name  string `json:"name"`
		Table string `json:"table"`
	} `json:"models"`
	Up   []string `json:"up"`
	Down []string `json:"down"`
}

// errQuarkCLITooOld is returned when the quark CLI found predates --emit
// json. It names the recipe rather than falling back to the empty pair:
// a pair that silently ignores the models is the failure mode this
// command exists to remove.
type errQuarkCLITooOld struct {
	cli    quarkCLI
	stderr string
}

func (e *errQuarkCLITooOld) Error() string {
	return fmt.Sprintf("the quark CLI at %s (from %s) does not know --emit json; makemigrations needs cmd/quark %s or later: go install %s@latest, or point %s at one that has it (quark said: %s)",
		e.cli.String(), e.cli.how, quarkCLIMinVersion, quarkCLIModule, quarkCLIEnv, strings.TrimSpace(e.stderr))
}

// runQuarkCLI runs the resolved CLI with args and returns its stdout and
// stderr. A variable so tests can stand in for the binary without PATH.
var runQuarkCLI = func(cli quarkCLI, dir string, args []string) (stdout, stderr []byte, err error) {
	cmd := exec.Command(cli.bin, append(append([]string{}, cli.args...), args...)...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return out.Bytes(), errBuf.Bytes(), err
}

// renderModelsDDL asks the quark CLI for the DDL of the models in modelsDir
// (absolute: quark resolves a relative path against its own working
// directory, QK-100) for dialect, which is nucleus's engine name —
// postgresql, mysql, sqlite, mssql, oracle — a spelling quark accepts.
func renderModelsDDL(cli quarkCLI, modelsDir, dialect, name string) (ddlDocument, error) {
	args := []string{"migrate", "create", name, "--from-models", modelsDir, "--dialect", dialect, "--emit", "json"}
	stdout, stderr, err := runQuarkCLI(cli, modelsDir, args)
	if err != nil {
		text := strings.TrimSpace(string(stderr))
		if strings.Contains(text, "unknown flag: --emit") || strings.Contains(text, "unknown flag: --from-models") {
			return ddlDocument{}, &errQuarkCLITooOld{cli: cli, stderr: text}
		}
		if text == "" {
			text = strings.TrimSpace(string(stdout))
		}
		return ddlDocument{}, fmt.Errorf("quark migrate create --from-models %s --dialect %s --emit json (via %s): %w\n%s", modelsDir, dialect, cli.String(), err, text)
	}
	var doc ddlDocument
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &doc); err != nil {
		return ddlDocument{}, fmt.Errorf("the quark CLI at %s did not print the DDL document --emit json promises (%w); its stdout was:\n%s", cli.String(), err, strings.TrimSpace(string(stdout)))
	}
	if doc.Tool != "quark" {
		return ddlDocument{}, fmt.Errorf("the document %s printed names tool %q, not quark", cli.String(), doc.Tool)
	}
	if doc.Schema != ddlDocumentSchema {
		return ddlDocument{}, fmt.Errorf("the quark CLI at %s (%s) prints DDL document schema %d and this nucleus reads schema %d: upgrade nucleus, or pin an older cmd/quark with %s", cli.String(), doc.Version, doc.Schema, ddlDocumentSchema, quarkCLIEnv)
	}
	return doc, nil
}

// modelsPackageHasQuarkModels reports whether dir holds a Go package with at
// least one exported struct carrying a `db` tag — what quark's loader takes
// as a model. It parses without type-checking, so it costs nothing and
// needs no module graph: it only decides whether the quark CLI is worth
// running, and the CLI is the one that reads the models for real.
func modelsPackageHasQuarkModels(dir string) (bool, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		return false, fmt.Errorf("reading the models in %s: %w", dir, err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					for _, field := range st.Fields.List {
						if field.Tag == nil {
							continue
						}
						raw, err := strconv.Unquote(field.Tag.Value)
						if err != nil {
							continue
						}
						if _, ok := reflect.StructTag(raw).Lookup("db"); ok {
							return true, nil
						}
					}
				}
			}
		}
	}
	return false, nil
}

// renderMigrationPair writes the up and down bodies from doc for system:
// a header that says where the SQL came from — the models, the quark that
// rendered it, the dialect — and the statements in execution order, each
// terminated the way pkg/db.ExecScript splits that engine's scripts (`;`,
// or a `/` line on Oracle, which go-ora would refuse as part of a
// statement). modelsRel is how the user named the models directory.
func renderMigrationPair(doc ddlDocument, system, modelsRel string) (up, down string) {
	names := make([]string, 0, len(doc.Models))
	for _, m := range doc.Models {
		names = append(names, m.Name+" -> "+m.Table)
	}
	sort.Strings(names)
	header := fmt.Sprintf("-- Rendered by nucleus makemigrations from %s with quark %s for %s.\n-- Models: %s.\n-- Review before nucleus migrate up: the file is not regenerated when a model changes.\n",
		filepath.ToSlash(modelsRel), doc.Version, system, strings.Join(names, ", "))
	return header + renderStatements(doc.Up, system), header + renderStatements(doc.Down, system)
}

func renderStatements(stmts []string, system string) string {
	if len(stmts) == 0 {
		return "-- Nothing to do.\n"
	}
	terminator := ";\n"
	if system == "oracle" {
		terminator = "\n/\n"
	}
	var b strings.Builder
	for i, stmt := range stmts {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimRight(stmt, " \t\n;"))
		b.WriteString(terminator)
	}
	return b.String()
}

// errNoQuarkModels is returned by renderModelsMigration when the directory
// exists but no exported struct in it carries a db tag; the caller writes
// the empty pair and says why.
var errNoQuarkModels = errors.New("no Quark models")

// renderModelsMigration is the model-driven half of `migrate create`: it
// resolves the quark CLI, renders the DDL for the models in modelsDir and
// returns the pair's bodies. notice receives what the user should know
// before the files are written (a download about to happen).
func renderModelsMigration(modelsDir, system, name string, getenv func(string) string, notice func(string)) (doc ddlDocument, up, down string, err error) {
	abs, err := filepath.Abs(modelsDir)
	if err != nil {
		return doc, "", "", fmt.Errorf("--from-models %s: %w", modelsDir, err)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return doc, "", "", fmt.Errorf("--from-models %s: no such directory (the generators write Quark models to %s)", modelsDir, projectModelsDir)
	}
	has, err := modelsPackageHasQuarkModels(abs)
	if err != nil {
		return doc, "", "", err
	}
	if !has {
		return doc, "", "", errNoQuarkModels
	}
	cli, err := resolveQuarkCLI(getenv)
	if err != nil {
		return doc, "", "", err
	}
	if cli.how == "go run" {
		notice(fmt.Sprintf("No quark CLI on PATH: running `%s` (downloads it; go install %s@latest to keep one)", cli.String(), quarkCLIModule))
	}
	doc, err = renderModelsDDL(cli, abs, system, name)
	if err != nil {
		return doc, "", "", err
	}
	up, down = renderMigrationPair(doc, system, modelsDir)
	return doc, up, down, nil
}
