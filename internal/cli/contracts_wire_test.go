// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wireMain = `package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func main() {
	if err := nucleus.New().
		FromConfigFile("nucleus.yml").
		WithOpenAPIDocument("/openapi.json").
		Start(); err != nil {
		log.Fatal(err)
	}
}
`

func writeWireMain(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The generators write internal/contracts and pass it as the base of the
// served document, once: a second resource leaves main.go alone.
func TestEnsureOpenAPIBaseWiresTheContractOnce(t *testing.T) {
	path := writeWireMain(t, wireMain)
	added, err := ensureOpenAPIBase(path, "example.com/app/internal/contracts")
	if err != nil || !added {
		t.Fatalf("first wiring: added=%v err=%v", added, err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `WithOpenAPIDocument("/openapi.json", contracts.NewDocument())`) ||
		!strings.Contains(string(got), `"example.com/app/internal/contracts"`) {
		t.Fatalf("main.go after wiring:\n%s", got)
	}
	added, err = ensureOpenAPIBase(path, "example.com/app/internal/contracts")
	if err != nil || added {
		t.Fatalf("second wiring must be a no-op: added=%v err=%v", added, err)
	}
}

func TestEnsureOpenAPIBaseLeavesAChosenBaseAlone(t *testing.T) {
	src := strings.Replace(wireMain, `WithOpenAPIDocument("/openapi.json")`, `WithOpenAPIDocument("/openapi.json", myDoc)`, 1)
	path := writeWireMain(t, src)
	added, err := ensureOpenAPIBase(path, "example.com/app/internal/contracts")
	if err != nil || added {
		t.Fatalf("a base the person chose must stay: added=%v err=%v", added, err)
	}
}

func TestEnsureOpenAPIBaseNeedsTheCall(t *testing.T) {
	src := strings.Replace(wireMain, "\t\tWithOpenAPIDocument(\"/openapi.json\").\n", "", 1)
	path := writeWireMain(t, src)
	if _, err := ensureOpenAPIBase(path, "example.com/app/internal/contracts"); !errors.Is(err, errNoOpenAPIDocument) {
		t.Fatalf("a chain that serves no document: want errNoOpenAPIDocument, got %v", err)
	}
}

func TestEnsureOpenAPIBaseRefusesANameCollision(t *testing.T) {
	src := strings.Replace(wireMain, "\t\"log\"\n", "\t\"log\"\n\n\t\"example.com/other/contracts\"\n", 1)
	src = strings.Replace(src, "func main() {", "var _ = contracts.X\n\nfunc main() {", 1)
	path := writeWireMain(t, src)
	if _, err := ensureOpenAPIBase(path, "example.com/app/internal/contracts"); !errors.Is(err, errImportNameCollision) {
		t.Fatalf("want errImportNameCollision, got %v", err)
	}
}
