package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runOpenAPI(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("openapi", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "openapi")

	outPath := fs.String("out", "openapi.json", "Output path for the exported OpenAPI JSON document, or - for stdout")
	projectDir := fs.String("project", ".", "Project root that contains go.mod and internal/contracts")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) != 0 {
		return usageError("openapi")
	}

	root, err := filepath.Abs(strings.TrimSpace(*projectDir))
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	modulePath, hasModule, err := detectModulePath(root)
	if err != nil {
		return err
	}
	if !hasModule {
		return fmt.Errorf("openapi export requires a Go module in %s", root)
	}
	if err := requireContractsAggregator(root); err != nil {
		return err
	}

	exporterDir, err := os.MkdirTemp(root, ".nucleus-openapi-*")
	if err != nil {
		return fmt.Errorf("create exporter workspace: %w", err)
	}
	defer os.RemoveAll(exporterDir)

	exporterMainPath := filepath.Join(exporterDir, "main.go")
	exporterMainBody := fmt.Sprintf(openAPIExporterTemplate, modulePath)
	if err := os.WriteFile(exporterMainPath, []byte(exporterMainBody), 0644); err != nil {
		return fmt.Errorf("write exporter entrypoint: %w", err)
	}

	exportRel, err := filepath.Rel(root, exporterDir)
	if err != nil {
		return fmt.Errorf("resolve exporter path: %w", err)
	}

	cmd := exec.Command("go", "run", "./"+filepath.ToSlash(exportRel))
	cmd.Dir = root

	var body bytes.Buffer
	var cmdErr bytes.Buffer
	cmd.Stdout = &body
	cmd.Stderr = &cmdErr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(cmdErr.String())
		if msg == "" {
			return fmt.Errorf("export openapi document: %w", err)
		}
		return fmt.Errorf("export openapi document: %w: %s", err, msg)
	}
	if !json.Valid(body.Bytes()) {
		return fmt.Errorf("openapi export produced invalid JSON")
	}

	if strings.TrimSpace(*outPath) == "-" {
		if _, err := stdout.Write(body.Bytes()); err != nil {
			return fmt.Errorf("write openapi stdout: %w", err)
		}
		if body.Len() > 0 && body.Bytes()[body.Len()-1] != '\n' {
			_, _ = io.WriteString(stdout, "\n")
		}
		return nil
	}

	targetPath := strings.TrimSpace(*outPath)
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(root, targetPath)
	}
	if err := ensureDir(filepath.Dir(targetPath)); err != nil {
		return err
	}
	if err := os.WriteFile(targetPath, body.Bytes(), 0644); err != nil {
		return fmt.Errorf("write openapi document %s: %w", targetPath, err)
	}

	fmt.Fprintf(stdout, "OpenAPI document exported: %s\n", targetPath)
	return nil
}

// contractsPackageRelPath is the package the exporter imports
// (<module>/internal/contracts): generate resource and startapp create it
// as internal/contracts/contracts.go, and every scaffolded contract registers
// into it. The exporter only calls contracts.NewDocument(), so the check is
// on the package, not on that file name — a project that keeps its
// aggregator under registry.go or document.go is as exportable as the
// scaffolded one.
const contractsPackageRelPath = "internal/contracts"

// requireContractsAggregator fails BEFORE the exporter is compiled when the
// project has no internal/contracts package. The exporter imports that
// package, so on a fresh `nucleus new` scaffold the failure used to be the
// raw stderr of `go run`: "no required module provides package
// example.com/myapp/internal/contracts; to add it: go get
// example.com/myapp/internal/contracts" — an instruction that cannot work,
// for a package that lives in the project itself. The recipe here names the
// commands that create the aggregator.
//
// "Package present" means the directory holds at least one non-test .go
// file: that is what `go build` needs to resolve the import. A directory
// with only _test.go files (or none) is not importable and gets the recipe.
func requireContractsAggregator(root string) error {
	dir := filepath.Join(root, filepath.FromSlash(contractsPackageRelPath))
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return fmt.Errorf("scan %s: %w", dir, err)
	}
	for _, match := range matches {
		if strings.HasSuffix(match, "_test.go") {
			continue
		}
		info, err := os.Stat(match)
		if err != nil {
			return fmt.Errorf("stat %s: %w", match, err)
		}
		if info.Mode().IsRegular() {
			return nil
		}
	}
	return fmt.Errorf("no %s package in %s: the OpenAPI document is built from that package and this project has none yet.\n"+
		"Run `nucleus generate resource <Name>` (or `nucleus startapp <name>`) to create the contracts aggregator with a first contract, then export again",
		contractsPackageRelPath, root)
}

const openAPIExporterTemplate = `package main

import (
	"log"
	"os"

	"%[1]s/internal/contracts"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

func main() {
	doc := contracts.NewDocument()
	if err := openapi.WriteJSON(os.Stdout, doc); err != nil {
		log.Fatal(err)
	}
}
`
