// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
)

// The scaffold serves the application's OpenAPI document
// (WithOpenAPIDocument in the nucleus.New() chain), and the generators that
// write a hand-written contract (generate resource, startapp) used to leave
// it out of that document: the application served the routes derived from
// the router, `nucleus openapi` exported the same, and internal/contracts —
// the schemas the generator had just written — appeared in neither. The
// contract joins the served document as its base, written into main.go the
// way --mount writes a Mount call.

// errNoOpenAPIDocument reports a composition root whose builder chain does
// not serve the derived document: there is no call to pass the base to.
var errNoOpenAPIDocument = errors.New("the nucleus.New() builder chain does not call WithOpenAPIDocument")

// ensureOpenAPIBase passes `<contracts>.NewDocument()` as the base of the
// WithOpenAPIDocument call in func main of the file at path, importing
// importPath for it. It reports whether it wrote anything; a call that
// already names the contracts package, or already carries a base the
// person chose, is left as it is.
func ensureOpenAPIBase(path, importPath string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	pkgName := nucleusLocalName(f)
	mainFn := findFuncMain(f)
	if pkgName == "" || mainFn == nil || mainFn.Body == nil {
		return false, fmt.Errorf("%s: %w", path, errNoBuilderChain)
	}
	chain := findBuilderChain(mainFn.Body, pkgName)
	if chain == nil {
		return false, fmt.Errorf("%s: %w", path, errNoBuilderChain)
	}
	var call *ast.CallExpr
	for _, c := range chain {
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithOpenAPIDocument" {
			call = c
		}
	}
	if call == nil {
		return false, fmt.Errorf("%s: %w", path, errNoOpenAPIDocument)
	}
	name := packageNameOfPath(importPath)
	offsetOf := func(p token.Pos) int { return fset.Position(p).Offset }
	if len(call.Args) != 1 {
		// No pattern (it would not compile) or a base already there: what
		// the person wrote stays, including a base that is not ours.
		return false, nil
	}
	for _, arg := range call.Args {
		if strings.Contains(string(src[offsetOf(arg.Pos()):offsetOf(arg.End())]), name+".") {
			return false, nil
		}
	}
	if err := checkImportNameFree(f, importPath); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}

	rparen := offsetOf(call.Rparen)
	var out []byte
	out = append(out, src[:rparen]...)
	out = append(out, ", "+name+".NewDocument()"...)
	out = append(out, src[rparen:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	if _, err := ensureImport(path, importPath, ""); err != nil {
		return false, err
	}
	return true, nil
}

// wireContractsDocument is the step generate resource and startapp run
// after writing the contracts aggregator: the contract becomes the base of
// the document the application serves. A project without a scaffolded
// composition root (no main.go, a hand-written chain, a chain that serves
// no document) gets the line to add instead.
func wireContractsDocument(outDir, modulePath string, stdout io.Writer) {
	if modulePath == "" {
		return
	}
	importPath := modulePath + "/internal/contracts"
	hint := func() {
		fmt.Fprintf(stdout, "Serve the contract in the application's OpenAPI document:  WithOpenAPIDocument(\"/openapi.json\", contracts.NewDocument())  (import %q)\n", importPath)
	}
	mainPath, err := pickImportFile(outDir)
	if err != nil || mainPath == "" || pkgNameOf(mainPath) != "main" {
		hint()
		return
	}
	added, err := ensureOpenAPIBase(mainPath, importPath)
	switch {
	case err != nil:
		hint()
	case added:
		fmt.Fprintf(stdout, "The contract is the base of the served OpenAPI document (%s):  WithOpenAPIDocument(\"/openapi.json\", contracts.NewDocument())\n", rel(outDir, mainPath))
	}
}
