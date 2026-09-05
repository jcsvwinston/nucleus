// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ensureMountCall edits the one file every project has — the composition
// root — so its behaviour on the shapes people actually write is the
// contract: the scaffold's Start() chain, the Build()+nucleus.Run shape,
// a chain on a single line, a chain already mounting the module, and a
// main.go with no chain at all, which it must refuse rather than guess.
func TestEnsureMountCall(t *testing.T) {
	const importPath = "example.com/myapp/internal/notes"
	const expr = "notes.Module()"

	scaffoldMain := `// Command myapp is the entry point.
//
//	nucleus.New().
//	    FromConfigFile("nucleus.yml").
//	    Mount(notes.Module()).   // your module
//	    Start()
package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)

func main() {
	if err := nucleus.New().
		FromConfigFile("nucleus.yml").
		Start(); err != nil {
		log.Fatalf("myapp: %v", err)
	}
}
`

	buildMain := `package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func main() {
	app, err := nucleus.New().
		FromConfigFile("nucleus.yml").
		WithoutDefaults().
		Build()
	if err != nil {
		log.Fatal(err)
	}
	if err := nucleus.Run(app); err != nil {
		log.Fatal(err)
	}
}
`

	oneLineMain := `package main

import "github.com/jcsvwinston/nucleus/pkg/nucleus"

func main() {
	_ = nucleus.New().FromConfigFile("nucleus.yml").Start()
}
`

	aliasedMain := `package main

import (
	"log"

	nuc "github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func main() {
	b := nuc.New().
		FromConfigFile("nucleus.yml")
	if err := b.Start(); err != nil {
		log.Fatal(err)
	}
}
`

	cases := []struct {
		name  string
		src   string
		want  []string
		added bool
	}{
		{
			name: "scaffold Start() chain: Mount goes before Start, on its own line",
			src:  scaffoldMain,
			want: []string{
				"FromConfigFile(\"nucleus.yml\").\n\t\tMount(notes.Module()).\n\t\tStart()",
				"\"example.com/myapp/internal/notes\"",
			},
			added: true,
		},
		{
			name:  "Build() chain: Mount goes before Build",
			src:   buildMain,
			want:  []string{"WithoutDefaults().\n\t\tMount(notes.Module()).\n\t\tBuild()", "\"example.com/myapp/internal/notes\""},
			added: true,
		},
		{
			name:  "single-line chain keeps its layout",
			src:   oneLineMain,
			want:  []string{"nucleus.New().FromConfigFile(\"nucleus.yml\").Mount(notes.Module()).Start()"},
			added: true,
		},
		{
			name:  "aliased import and a chain without a terminal call: Mount is appended",
			src:   aliasedMain,
			want:  []string{"FromConfigFile(\"nucleus.yml\").\n\t\tMount(notes.Module())", "b.Start()"},
			added: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(path, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			added, err := ensureMountCall(path, importPath, expr)
			if err != nil {
				t.Fatalf("ensureMountCall: %v", err)
			}
			if added != c.added {
				t.Errorf("added = %v, want %v", added, c.added)
			}
			got := readFile(t, path)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("result is missing %q:\n%s", w, got)
				}
			}
			// The doc comment repeats the chain on purpose (the trap a sed
			// falls into): the CODE must mount exactly once.
			if n := strings.Count(stripComments(got), "Mount("+expr+")"); n != 1 {
				t.Errorf("Mount(%s) appears %d times in code, want exactly 1:\n%s", expr, n, got)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.AllErrors); err != nil {
				t.Errorf("the edited file is not valid Go: %v\n%s", err, got)
			}

			// Idempotence: a second run changes nothing.
			again, err := ensureMountCall(path, importPath, expr)
			if err != nil {
				t.Fatalf("second ensureMountCall: %v", err)
			}
			if again {
				t.Errorf("second run reported an edit; the file should already carry the mount")
			}
			if after := readFile(t, path); after != got {
				t.Errorf("second run changed the file:\n--- first\n%s\n--- second\n%s", got, after)
			}
		})
	}
}

// A composition root with no builder chain — the direct-struct shape, or
// a main that delegates to another function — is refused with
// errNoBuilderChain and left byte-for-byte alone. Guessing where a Mount
// call belongs in code the tool does not understand is how a helper
// becomes a hazard.
func TestEnsureMountCallRefusesWithoutBuilderChain(t *testing.T) {
	cases := map[string]string{
		"direct struct": `package main

import "github.com/jcsvwinston/nucleus/pkg/nucleus"

func main() {
	_ = nucleus.Run(nucleus.App{})
}
`,
		"delegated main": `package main

import "github.com/jcsvwinston/nucleus/pkg/nucleus"

func build() error { return nucleus.New().Start() }

func main() { _ = build() }
`,
		"no nucleus import": `package main

func main() {}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			added, err := ensureMountCall(path, "example.com/x/internal/notes", "notes.Module()")
			if !errors.Is(err, errNoBuilderChain) {
				t.Fatalf("want errNoBuilderChain, got added=%v err=%v", added, err)
			}
			if got := readFile(t, path); got != src {
				t.Errorf("a refused edit must leave the file untouched:\n%s", got)
			}
		})
	}
}
