// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package drivergraph_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// engines maps each driver module under drivers/ to the module of the engine
// it links. A driver module's package graph must hold its own engine — which
// also proves the listing is not empty — and none of the other four.
var engines = map[string]string{
	"mssql":    "github.com/microsoft/go-mssqldb",
	"mysql":    "github.com/go-sql-driver/mysql",
	"oracle":   "github.com/sijms/go-ora/v2",
	"postgres": "github.com/jackc/pgx/v5",
	"sqlite":   "modernc.org/sqlite",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func goCmd(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GOFLAGS="}, env...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		for _, s := range []string{"dial tcp", "no such host", "proxy.golang.org", "GOPROXY=off", "i/o timeout"} {
			if strings.Contains(string(out), s) {
				t.Skipf("the module proxy is unreachable (%q); nothing was measured:\n%s", s, out)
			}
		}
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func owner(pkg string) (string, bool) {
	for name, mod := range engines {
		if pkg == mod || strings.HasPrefix(pkg, mod+"/") {
			return name, true
		}
	}
	return "", false
}

// Each driver module links its own engine and no other (NU-8). Measured on
// the module as an application imports it, against THIS tree's framework:
// a workspace holds the root and the driver module, so a regression in either
// — an engine imported next to the driver, or a root package the driver
// imports growing an engine — shows up here, in `go test ./...`, rather than
// as a heavier binary someone measures months later.
func TestEachDriverModuleLinksOnlyItsEngine(t *testing.T) {
	root := repoRoot(t)
	work := t.TempDir()
	dirs := []string{root}
	for name := range engines {
		dirs = append(dirs, filepath.Join(root, "drivers", name))
	}
	sort.Strings(dirs)
	goCmd(t, work, []string{"GOWORK=off"}, append([]string{"work", "init"}, dirs...)...)
	env := []string{"GOWORK=" + filepath.Join(work, "go.work")}

	for name := range engines {
		t.Run(name, func(t *testing.T) {
			mod := "github.com/jcsvwinston/nucleus/drivers/" + name
			out := goCmd(t, work, env, "list", "-deps", mod)
			linked := map[string][]string{}
			for _, pkg := range strings.Fields(out) {
				if engine, ok := owner(pkg); ok {
					linked[engine] = append(linked[engine], pkg)
				}
			}
			if len(linked[name]) == 0 {
				t.Fatalf("%s does not link its own engine %s; the listing measured nothing:\n%s", mod, engines[name], out)
			}
			for engine, pkgs := range linked {
				if engine == name {
					continue
				}
				t.Errorf("%s links the %s engine (%s): %s\n\n"+
					"An application that imports this module pays for every engine it links. The\n"+
					"module registers its own driver and classifier with its own engine's types only.",
					mod, engine, engines[engine], strings.Join(pkgs, ", "))
			}
		})
	}
}
