// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strconv"
)

// The generators used to end with "Mount it in main.go:" and a line to
// paste. That line is the whole integration of a module slice, and it was
// the one step the tool left to the person — who then edited two places
// (the import block and the builder chain) by hand, or wrote a sed that
// also matched the chain repeated in main.go's doc comment.
//
// ensureMountCall makes the edit itself, the way `nucleus add` writes the
// blank import: it parses the file, finds the fluent chain rooted at
// nucleus.New() inside func main, and splices `.Mount(<expr>)` into it as
// TEXT, without printing the AST back (go/printer would reflow code it was
// not asked to touch). A main.go without such a chain is refused, not
// guessed at: the caller prints the line to add and stops.

// errNoBuilderChain reports a main.go whose func main holds no fluent chain
// rooted at nucleus.New() — a hand-written composition root (for example
// nucleus.Run(nucleus.App{...})) the editor must not guess at.
var errNoBuilderChain = errors.New("no nucleus.New() builder chain found in func main")

const nucleusPackagePath = "github.com/jcsvwinston/nucleus/pkg/nucleus"

// ensureMountCall adds `Mount(<expr>)` to the nucleus.New() builder chain in
// func main of the file at path and imports importPath for it, unless the
// chain already mounts that expression. It reports whether it wrote
// anything. The Mount call goes right before the chain's terminal call
// (Start, Serve, Build) so the module is registered before the application
// is assembled; a chain without a terminal call gets it appended.
func ensureMountCall(path, importPath, expr string) (bool, error) {
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
	if pkgName == "" {
		return false, fmt.Errorf("%s: %w (the file does not import %s)", path, errNoBuilderChain, nucleusPackagePath)
	}

	mainFn := findFuncMain(f)
	if mainFn == nil || mainFn.Body == nil {
		return false, fmt.Errorf("%s: %w (no func main)", path, errNoBuilderChain)
	}

	chain := findBuilderChain(mainFn.Body, pkgName)
	if chain == nil {
		return false, fmt.Errorf("%s: %w", path, errNoBuilderChain)
	}

	// Idempotence: a Mount call already carrying the same expression text
	// means the module is mounted; touching the file again would register
	// it twice and fail boot on the duplicate module name.
	offsetOf := func(p token.Pos) int { return fset.Position(p).Offset }
	for _, call := range chain {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Mount" {
			continue
		}
		for _, arg := range call.Args {
			if string(src[offsetOf(arg.Pos()):offsetOf(arg.End())]) == expr {
				return false, nil
			}
		}
	}

	// Splice point: before the terminal call when there is one, else after
	// the outermost call of the chain. The chain is ordered innermost
	// (nucleus.New()) to outermost.
	var out []byte
	terminal := terminalCall(chain)
	if terminal != nil {
		sel := terminal.Fun.(*ast.SelectorExpr)
		receiverEnd := offsetOf(sel.X.End())
		selStart := offsetOf(sel.Sel.Pos())
		// Whatever sits between the receiver and the method name — the dot
		// plus the newline and indentation the file already uses — is
		// repeated verbatim so the new line matches the chain's layout.
		joiner := src[receiverEnd:selStart]
		out = append(out, src[:receiverEnd]...)
		out = append(out, joiner...)
		out = append(out, "Mount("+expr+")"...)
		out = append(out, src[receiverEnd:]...)
	} else {
		last := chain[len(chain)-1]
		end := offsetOf(last.End())
		out = append(out, src[:end]...)
		out = append(out, "."+"\n\t\tMount("+expr+")"...)
		out = append(out, src[end:]...)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	if _, err := ensureImport(path, importPath, ""); err != nil {
		return false, err
	}
	return true, nil
}

// nucleusLocalName returns the identifier the file uses for the
// pkg/nucleus import ("nucleus" unless aliased), or "" when it is not
// imported.
func nucleusLocalName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != nucleusPackagePath {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "" && imp.Name.Name != "_" && imp.Name.Name != "." {
			return imp.Name.Name
		}
		return "nucleus"
	}
	return ""
}

func findFuncMain(f *ast.File) *ast.FuncDecl {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "main" {
			return fn
		}
	}
	return nil
}

// findBuilderChain returns the longest method-call chain rooted at
// <pkg>.New() found in body, innermost call first. It walks every
// expression in func main, so the chain can sit inside an `if err := …`
// initialiser, an assignment (`app, err := …Build()`) or a bare statement.
func findBuilderChain(body *ast.BlockStmt, pkgName string) []*ast.CallExpr {
	var best []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		chain := callChain(call)
		if len(chain) == 0 || !isNewCall(chain[0], pkgName) {
			return true
		}
		if len(chain) > len(best) {
			best = chain
		}
		// The inner calls of this chain are also CallExprs the walk will
		// visit; they are shorter chains and lose the comparison above.
		return true
	})
	return best
}

// callChain unrolls a fluent expression `a().b().c()` into [a(), b(), c()].
// A receiver that is not itself a call ends the chain.
func callChain(outer *ast.CallExpr) []*ast.CallExpr {
	var reversed []*ast.CallExpr
	cur := outer
	for cur != nil {
		reversed = append(reversed, cur)
		sel, ok := cur.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		next, ok := sel.X.(*ast.CallExpr)
		if !ok {
			break
		}
		cur = next
	}
	chain := make([]*ast.CallExpr, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		chain = append(chain, reversed[i])
	}
	return chain
}

func isNewCall(call *ast.CallExpr, pkgName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkgName
}

// terminalCall returns the chain's assembling call — Start, Serve or Build —
// or nil when the chain stops at the builder.
func terminalCall(chain []*ast.CallExpr) *ast.CallExpr {
	for _, call := range chain[1:] {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		switch sel.Sel.Name {
		case "Start", "Serve", "Build":
			return call
		}
	}
	return nil
}

// ensureImport adds `import <name> "importPath"` to the file at path
// unless the path is already imported, reporting whether it wrote
// anything. name is "_" for a side-effect import and "" for a plain one.
// Like ensureBlankImport it edits the text, appending to the last
// parenthesised block so the file keeps its own grouping, then lets gofmt
// sort the block.
func ensureImport(path, importPath, name string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == importPath {
			return false, nil
		}
	}

	line := "\t"
	if name != "" {
		line += name + " "
	}
	line += strconv.Quote(importPath) + "\n"
	var out []byte

	var block *ast.GenDecl
	var anchor *ast.ImportSpec
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT || !gd.Lparen.IsValid() {
			continue
		}
		block = gd
		// A plain import of the application's own package belongs in the
		// group that imports pkg/nucleus, not after the side-effect driver
		// imports and their comment; anchor it there when the group exists.
		if name == "" {
			for _, spec := range gd.Specs {
				imp := spec.(*ast.ImportSpec)
				if p, _ := strconv.Unquote(imp.Path.Value); p == nucleusPackagePath {
					anchor = imp
				}
			}
		}
	}
	if anchor != nil {
		end := fset.Position(anchor.End()).Offset
		nl := bytes.IndexByte(src[end:], '\n')
		if nl < 0 {
			return false, fmt.Errorf("%s: cannot find the end of the import line", path)
		}
		at := end + nl + 1
		out = append(out, src[:at]...)
		out = append(out, line...)
		out = append(out, src[at:]...)
	} else if block != nil {
		pos := fset.Position(block.Rparen).Offset
		start := pos
		for start > 0 && (src[start-1] == ' ' || src[start-1] == '\t') {
			start--
		}
		out = append(out, src[:start]...)
		out = append(out, '\n')
		out = append(out, line...)
		out = append(out, src[start:]...)
	} else {
		pkgEnd := fset.Position(f.Name.End()).Offset
		nl := bytes.IndexByte(src[pkgEnd:], '\n')
		if nl < 0 {
			return false, fmt.Errorf("%s: cannot find the end of the package clause", path)
		}
		at := pkgEnd + nl + 1
		out = append(out, src[:at]...)
		out = append(out, []byte("\nimport (\n"+line+")\n")...)
		out = append(out, src[at:]...)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	if gofmt, err := exec.LookPath("gofmt"); err == nil {
		_ = exec.Command(gofmt, "-w", path).Run()
	}
	return true, nil
}
