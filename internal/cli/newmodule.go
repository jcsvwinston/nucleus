// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/cli/scaffold"
)

// moduleNamePattern is the name nucleus.CheckModule accepts: the
// modules.<name> configuration key, the NUCLEUS_MODULES__<NAME>__
// variables and the route prefix all have to be able to carry it.
var moduleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// moduleTestImports are the names the generated test imports beside the
// module's own package; a package of the same name would shadow one.
var moduleTestImports = map[string]bool{"nucleus": true, "nucleustest": true, "context": true, "http": true, "testing": true}

// moduleTemplateFlagsUnused rejects the flags that shape an application —
// a module repository has no main.go and no nucleus.yml for them to write.
func moduleTemplateFlagsUnused(fs *flag.FlagSet) error {
	var misplaced []string
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "with", "db", "port":
			misplaced = append(misplaced, "--"+f.Name)
		}
	})
	if len(misplaced) == 0 {
		return nil
	}
	return fmt.Errorf("%s does not apply to --template module: a module has no main.go or nucleus.yml — the application that mounts it chooses its database, its port and its catalog entries", strings.Join(misplaced, ", "))
}

// runNewModule writes a module repository: its own go.mod, the module
// (a route, the policy row that opens it, typed configuration, a value it
// provides), a test that holds it to nucleustest.CheckModule, a README and
// a CI workflow that runs the tests. It is what a module published on its
// own starts from, the way `generate module` starts a slice inside one
// application.
func runNewModule(projectName, outDir, modulePath string, force, offline bool, stdout, stderr io.Writer) error {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	moduleName := toSnakeCase(projectName)
	if !moduleNamePattern.MatchString(moduleName) {
		return fmt.Errorf("%q cannot name a module: a module name is lowercase letters, digits and underscores, starting with a letter — it is the modules.<name> configuration key and the route prefix", projectName)
	}
	pkg := strings.ReplaceAll(moduleName, "_", "")
	if token.IsKeyword(pkg) || pkg == "main" || moduleTestImports[pkg] {
		return fmt.Errorf("%q cannot name the module's Go package (%s): it is a Go keyword, main, or a package the generated test imports — choose another name", projectName, pkg)
	}

	projectDir := filepath.Join(outDir, projectName)
	if info, err := os.Stat(projectDir); err == nil && !info.IsDir() {
		return fmt.Errorf("target path exists and is not a directory: %s", projectDir)
	} else if err == nil && !force {
		return fmt.Errorf("project directory already exists: %s (use --force to overwrite scaffold files)", projectDir)
	}
	if err := ensureDir(projectDir); err != nil {
		return err
	}

	module := strings.TrimSpace(modulePath)
	if module == "" {
		module = defaultModulePath(projectName)
	}

	// The module's test boots an application on a temporary SQLite
	// database, so the test binary needs the SQLite driver module, at the
	// version released with this CLI.
	sqlite := scaffoldDatabases()["sqlite"]
	goVersion, toolchain := resolveGoDirectives()
	files, err := scaffold.Render("module", scaffold.TemplateData{
		Module:           module,
		ProjectName:      projectName,
		FrameworkVersion: resolveFrameworkVersion(),
		Template:         "module",
		GoVersion:        goVersion,
		Toolchain:        toolchain,
		Database:         sqlite.Name,
		DriverModule:     sqlite.Driver.Module,
		DriverTarget:     sqlite.Driver.Target(),
		ModuleName:       moduleName,
		PackageName:      pkg,
	})
	if err != nil {
		return err
	}
	for _, f := range files {
		target := filepath.Join(projectDir, filepath.FromSlash(f.RelPath))
		if err := writeFileIfNotExists(target, strings.TrimSpace(f.Body)+"\n", force); err != nil {
			return err
		}
	}

	handBack := "go get " + sqlite.Driver.Target() + " && go mod tidy"
	if !offline {
		escape := fmt.Sprintf("the scaffold is written in %s; run `%s` there when the network is back, or re-run with --offline --force", projectDir, handBack)
		fmt.Fprintf(stdout, "go get %s\n", sqlite.Driver.Target())
		if err := goGet(projectDir, sqlite.Driver.Target(), stdout, stderr); err != nil {
			return fmt.Errorf("go get %s: %w (%s)", sqlite.Driver.Target(), err, escape)
		}
		fmt.Fprintln(stdout, "go mod tidy")
		if err := goModTidy(projectDir, stdout, stderr); err != nil {
			return fmt.Errorf("go mod tidy: %w (%s)", err, escape)
		}
	}

	fmt.Fprintf(stdout, "Module scaffold created: %s (template: module, module name: %s, package: %s)\n", projectDir, moduleName, pkg)
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "A module with its own go.mod: a route under /%s, the policy row that opens it, typed\n", moduleName)
	fmt.Fprintf(stdout, "configuration (modules.%s) and a value it provides to other modules; a test that holds\n", moduleName)
	fmt.Fprintf(stdout, "it to nucleustest.CheckModule; a README; and a CI workflow that runs go test.\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "Next steps:\n")
	fmt.Fprintf(stdout, "  cd %s\n", projectDir)
	if offline {
		fmt.Fprintf(stdout, "  %s   # skipped by --offline\n", handBack)
	}
	fmt.Fprintf(stdout, "  go test ./...\n")
	fmt.Fprintf(stdout, "\n")
	fmt.Fprintf(stdout, "An application mounts it:\n")
	fmt.Fprintf(stdout, "  go get %s\n", module)
	fmt.Fprintf(stdout, "  nucleus.New().FromConfigFile(\"nucleus.yml\").Mount(%s.Module()).Start()\n", pkg)
	fmt.Fprintf(stdout, "\n")
	printDocsPointers(stdout, "module")
	return nil
}
