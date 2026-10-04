package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

func runOpenAPI(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("openapi", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "openapi")

	outPath := fs.String("out", "openapi.json", "Output path for the exported OpenAPI JSON document, or - for stdout")
	projectDir := fs.String("project", ".", "Project root that contains go.mod and internal/contracts")
	from := fs.String("from", "auto", "Where the document comes from: app (boot the application and read the document it derives from its routes and serves), contracts (internal/contracts.NewDocument(), the hand-written contract alone), or auto (app, unless the application serves no document and the project has internal/contracts)")
	mainDir := fs.String("dir", "", "Directory of the application's main package, for --from app (default: the project root)")
	timeout := fs.Duration("timeout", defaultRoutesTimeout, "How long the built application may take to print its document before it is killed (the build is not counted)")
	document := fs.String("document", "", "Read the document from this file instead of the project (a previous export, a document checked into a frontend repository); with --client or --check")
	client := fs.String("client", "", "Write a client generated from the document instead of the document: typescript (one dependency-free .ts file over fetch; --out defaults to client.ts)")
	check := fs.String("check", "", "Compare the document with a baseline (a previous export) and fail on every change that breaks a client written against it; nothing is written unless --out is given as well")

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

	var body []byte
	if path := strings.TrimSpace(*document); path != "" {
		// A document on disk: nothing to build or boot. The output paths
		// stay relative to the working directory, not to a project.
		if body, err = os.ReadFile(path); err != nil {
			return fmt.Errorf("read the document: %w", err)
		}
		if !json.Valid(body) {
			return fmt.Errorf("%s is not JSON", path)
		}
		if root, err = os.Getwd(); err != nil {
			return err
		}
		return finishOpenAPI(fs, body, root, *outPath, *client, *check, stdout, stderr)
	}

	modulePath, hasModule, err := detectModulePath(root)
	if err != nil {
		return err
	}
	if !hasModule {
		return fmt.Errorf("openapi export requires a Go module in %s", root)
	}

	switch source := strings.TrimSpace(*from); source {
	case "contracts":
		body, err = exportContractsDocument(root, modulePath)
	case "app", "auto":
		dir := root
		if strings.TrimSpace(*mainDir) != "" {
			if dir, err = filepath.Abs(*mainDir); err != nil {
				return fmt.Errorf("resolve --dir: %w", err)
			}
		}
		body, err = appDocument(dir, root, modulePath, source == "auto", *timeout, stderr)
	default:
		return fmt.Errorf("--from must be app, contracts or auto, got %q", source)
	}
	if err != nil {
		return err
	}
	return finishOpenAPI(fs, body, root, *outPath, *client, *check, stdout, stderr)
}

// finishOpenAPI is what the command does with the document once it has
// it: check it against a baseline, write a client generated from it, or
// write it.
func finishOpenAPI(fs *flag.FlagSet, body []byte, root, outPath, client, check string, stdout, stderr io.Writer) error {
	outGiven := false
	fs.Visit(func(f *flag.Flag) { outGiven = outGiven || f.Name == "out" })
	if strings.TrimSpace(check) != "" {
		if err := checkAgainstBaseline(body, check, stdout, stderr); err != nil {
			return err
		}
		if !outGiven && strings.TrimSpace(client) == "" {
			return nil
		}
	}
	switch lang := strings.TrimSpace(client); lang {
	case "":
	case "typescript", "ts":
		var doc openapi.Document
		if err := json.Unmarshal(body, &doc); err != nil {
			return fmt.Errorf("the document is not an OpenAPI document: %w", err)
		}
		ts, err := generateTypeScriptClient(&doc)
		if err != nil {
			return err
		}
		target := outPath
		if !outGiven {
			target = "client.ts"
		}
		return writeGeneratedFile([]byte(ts), target, root, "TypeScript client", stdout)
	default:
		return fmt.Errorf("--client must be typescript, got %q", lang)
	}
	return writeOpenAPIOutput(body, outPath, root, stdout)
}

// writeGeneratedFile writes a generated artifact to stdout (-) or to path,
// relative to the project root.
func writeGeneratedFile(body []byte, outPath, root, what string, stdout io.Writer) error {
	if strings.TrimSpace(outPath) == "-" {
		_, err := stdout.Write(body)
		return err
	}
	target := strings.TrimSpace(outPath)
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	if err := ensureDir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return fmt.Errorf("write %s %s: %w", what, target, err)
	}
	fmt.Fprintf(stdout, "%s written: %s\n", what, target)
	return nil
}

// checkAgainstBaseline compares the exported document with a baseline and
// fails naming every change that breaks a client written against the
// baseline (openapi.BreakingChanges). Additions pass.
func checkAgainstBaseline(body []byte, baselinePath string, stdout, stderr io.Writer) error {
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		return fmt.Errorf("read the baseline document: %w", err)
	}
	var prev, next openapi.Document
	if err := json.Unmarshal(raw, &prev); err != nil {
		return fmt.Errorf("the baseline %s is not an OpenAPI document: %w", baselinePath, err)
	}
	if err := json.Unmarshal(body, &next); err != nil {
		return fmt.Errorf("the exported document is not an OpenAPI document: %w", err)
	}
	changes := openapi.BreakingChanges(&prev, &next)
	if len(changes) == 0 {
		fmt.Fprintf(stdout, "OpenAPI document compatible with %s: no change breaks a client written against it\n", baselinePath)
		return nil
	}
	for _, c := range changes {
		fmt.Fprintf(stderr, "  %s\n", c)
	}
	return fmt.Errorf("%d change(s) break a client written against %s (listed above); keep the old shape beside the new one, or replace the baseline deliberately", len(changes), baselinePath)
}

// appDocument reads the document the application derives — the one its
// WithOpenAPIDocument route serves — by booting it the way `nucleus
// routes` does (NUCLEUS_PRINT_ROUTES: every module mounted, nothing
// listening) and taking the document off the same exit. In auto mode a
// project whose application serves no document and that keeps a
// hand-written internal/contracts gets that contract, as before this
// command could read the application, with a note on stderr; so does a
// project on a release that predates the derivation.
func appDocument(dir, root, modulePath string, auto bool, timeout time.Duration, stderr io.Writer) ([]byte, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("--timeout must be positive, got %s", timeout)
	}
	hasContracts := requireContractsAggregator(root) == nil
	dep := resolveNucleusDependency(root)
	if dep.known && !dep.carriesAPIDocument {
		if auto && hasContracts {
			return exportContractsDocument(root, modulePath)
		}
		return nil, fmt.Errorf("%s required by %s predates the OpenAPI document derived from the application: raise the requirement to read it, or use --from contracts to export internal/contracts", dep.describe(), filepath.Join(root, "go.mod"))
	}

	if err := ensureMainPackage(dir, root, "openapi"); err != nil {
		if auto && !hasContracts {
			return nil, fmt.Errorf("nothing to export in %s: %v.\n"+
				"There is no internal/contracts package in %s either (a hand-written contract): run `nucleus generate resource <Name>` (or `nucleus startapp <name>`) to create one, or point --dir at the main package of the application whose document you want",
				root, err, root)
		}
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "nucleus-openapi-")
	if err != nil {
		return nil, fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, "app")

	sigCtx, stop := signal.NotifyContext(context.Background(), routesStopSignals()...)
	defer stop()
	if out, err := buildMainPackage(sigCtx, dir, bin); err != nil {
		if sigCtx.Err() != nil {
			return nil, fmt.Errorf("stopped on signal while building the application in %s", dir)
		}
		return nil, fmt.Errorf("go build . (in %s) failed: %w\n%s", dir, err, out)
	}
	doc, err := readRouteDumpDocument(sigCtx, bin, dir, root, timeout, nil)
	if err != nil {
		return nil, err
	}
	if doc.OpenAPI == nil {
		if auto && hasContracts {
			return exportContractsDocument(root, modulePath)
		}
		return nil, fmt.Errorf("the application in %s printed its routes but no OpenAPI document: its nucleus predates the derivation; raise the requirement, or use --from contracts", dir)
	}
	if auto && doc.OpenAPI.Pattern == "" && hasContracts {
		fmt.Fprintf(stderr, "NOTE: the application serves no OpenAPI document (no WithOpenAPIDocument), so this is internal/contracts as written by hand.\n"+
			"Serve the document derived from the routes with WithOpenAPIDocument(\"/openapi.json\", contracts.NewDocument()), or pass --from app to export it.\n")
		return exportContractsDocument(root, modulePath)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, doc.OpenAPI.Document, "", "  "); err != nil {
		return nil, fmt.Errorf("the application printed an OpenAPI document that is not JSON: %w", err)
	}
	return pretty.Bytes(), nil
}

// writeOpenAPIOutput writes the document to stdout (-) or to the --out
// path, relative to the project root.
func writeOpenAPIOutput(body []byte, outPath, root string, stdout io.Writer) error {
	if strings.TrimSpace(outPath) == "-" {
		if _, err := stdout.Write(body); err != nil {
			return fmt.Errorf("write openapi stdout: %w", err)
		}
		if len(body) > 0 && body[len(body)-1] != '\n' {
			_, _ = io.WriteString(stdout, "\n")
		}
		return nil
	}

	targetPath := strings.TrimSpace(outPath)
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(root, targetPath)
	}
	if err := ensureDir(filepath.Dir(targetPath)); err != nil {
		return err
	}
	if err := os.WriteFile(targetPath, body, 0644); err != nil {
		return fmt.Errorf("write openapi document %s: %w", targetPath, err)
	}

	fmt.Fprintf(stdout, "OpenAPI document exported: %s\n", targetPath)
	return nil
}

// exportContractsDocument compiles a throwaway exporter that calls the
// project's internal/contracts.NewDocument() and returns its JSON — the
// document as written by hand, which is all this command could export
// before it could read the application.
func exportContractsDocument(root, modulePath string) ([]byte, error) {
	if err := requireContractsAggregator(root); err != nil {
		return nil, err
	}

	exporterDir, err := os.MkdirTemp(root, ".nucleus-openapi-*")
	if err != nil {
		return nil, fmt.Errorf("create exporter workspace: %w", err)
	}
	defer os.RemoveAll(exporterDir)

	exporterMainPath := filepath.Join(exporterDir, "main.go")
	exporterMainBody := fmt.Sprintf(openAPIExporterTemplate, modulePath)
	if err := os.WriteFile(exporterMainPath, []byte(exporterMainBody), 0644); err != nil {
		return nil, fmt.Errorf("write exporter entrypoint: %w", err)
	}

	exportRel, err := filepath.Rel(root, exporterDir)
	if err != nil {
		return nil, fmt.Errorf("resolve exporter path: %w", err)
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
			return nil, fmt.Errorf("export openapi document: %w", err)
		}
		return nil, fmt.Errorf("export openapi document: %w: %s", err, msg)
	}
	if !json.Valid(body.Bytes()) {
		return nil, fmt.Errorf("openapi export produced invalid JSON")
	}

	return body.Bytes(), nil
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
