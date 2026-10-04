// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/cli"
	"github.com/jcsvwinston/nucleus/internal/routedump"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

// The probes of the document the application DERIVES (A10 S5): read from
// its routes, its Go structs and its security, served by the application
// and by the application the scaffold generates.

// OA-03: the document is DERIVED from the registered routes — an application
// that writes no document by hand still publishes every route its modules
// register, flat, grouped or as a resource, under the module's tag.
func probeDerivedFromRoutes(t *testing.T, _ *env) verdict {
	resp, raw := doOn(t, startWithDocument(t, false), http.MethodGet, "/openapi.json", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("an application with routes and WithOpenAPIDocument answers %d at /openapi.json", resp.StatusCode)
		return absent
	}
	var doc openapi.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Logf("not a document: %v", err)
		return absent
	}
	want := map[string]bool{"GET /bench/things": false, "GET /docs/grouped/{id}": false, "GET /docs/widgets": false, "GET /docs/widgets/{id}": false}
	for p, item := range doc.Paths {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			op := item.Operation(m)
			if _, wanted := want[m+" "+p]; wanted && op != nil && op.OperationID != "" && len(op.Tags) == 1 {
				want[m+" "+p] = true
			}
		}
	}
	missing := 0
	for k, ok := range want {
		if !ok {
			t.Logf("the document does not list %s with an operationId and its module's tag", k)
			missing++
		}
	}
	switch {
	case missing == 0:
		return present
	case missing < len(want):
		return partial
	}
	return absent
}

// OA-04: schemas derive from Go structs the way encoding/json writes them:
// names from the json tags, required from what is always written or what
// validate requires, constraints from validate, named structs referenced.
func probeSchemaFromStruct(t *testing.T, _ *env) verdict {
	doc := openapi.NewDocument("bench", "1.0.0")
	ref := openapi.SchemaOf[BenchArticle](doc)
	art, ok := doc.Components.Schemas["BenchArticle"]
	if ref.Ref != "#/components/schemas/BenchArticle" || !ok {
		t.Logf("SchemaOf[BenchArticle] → %+v; components %v", ref, doc.Components.Schemas)
		return absent
	}
	_, secret := art.Properties["Secret"]
	checks := []struct {
		what string
		ok   bool
	}{
		{"json tag names", art.Properties["title"].Type == "string" && art.Properties["Untagged"].Type == "boolean"},
		{`json:"-" left out`, !secret},
		{"validate required", containsString(art.Required, "title")},
		{"omitempty optional", !containsString(art.Required, "body")},
		{"validate max → maxLength", art.Properties["title"].MaxLength != nil && *art.Properties["title"].MaxLength == 120},
		{"validate oneof → enum", len(art.Properties["state"].Enum) == 2},
		{"time.Time → date-time", art.Properties["at"].Format == "date-time"},
		{"nested named struct → $ref", art.Properties["author"].Ref == "#/components/schemas/BenchAuthor"},
		{"pointer → nullable", art.Properties["score"].Nullable},
	}
	failed := 0
	for _, c := range checks {
		if !c.ok {
			t.Logf("schema from struct: %s does not hold — %+v", c.what, art)
			failed++
		}
	}
	if failed == 0 {
		return present
	}
	return partial
}

// OA-08: the document declares the application's security — the bearer
// scheme when it verifies JWTs, and an explicit "no authentication" on
// exactly the operations its policy opens to anonymous callers — and the
// contract the generator writes by hand says the same (NU-97).
func probeSecuritySchemeDeclared(t *testing.T, _ *env) verdict {
	resp, raw := doOn(t, startWithDocument(t, true), http.MethodGet, "/openapi.json", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("a default-deny application answers %d at its document route", resp.StatusCode)
		return absent
	}
	var doc openapi.Document
	_ = json.Unmarshal(raw, &doc)
	scheme := doc.Components.SecuritySchemes["bearerAuth"]
	derived := scheme.Type == "http" && scheme.Scheme == "bearer" && len(doc.Security) == 1
	open, closed := doc.Paths["/docs/widgets"].Get, doc.Paths["/docs/widgets"].Post
	if open == nil || closed == nil || open.Security == nil || len(*open.Security) != 0 || closed.Security != nil {
		t.Logf("the per-operation security does not follow the policy: open %+v closed %+v", open, closed)
		derived = false
	}
	if !derived {
		t.Logf("served document security: %v schemes %v", doc.Security, doc.Components.SecuritySchemes)
	}

	// The hand-written contract the generator writes (generate resource).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/benchgen\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"generate", "resource", "Note", "--out", dir, "--dialect", "sqlite"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Logf("generate resource exited %d: %s", code, errOut.String())
		return partial
	}
	agg, _ := os.ReadFile(filepath.Join(dir, "internal", "contracts", "contracts.go"))
	written := regexp.MustCompile(`(?m)^\s*doc\.AddSecurityScheme\("bearerAuth", openapi\.BearerAuthScheme\(`).Match(agg) &&
		regexp.MustCompile(`(?m)^\s*doc\.Security = \[\]openapi\.SecurityRequirement\{openapi\.Require\("bearerAuth"\)\}`).Match(agg)
	if !written {
		t.Log("the contracts aggregator the generator writes declares no security scheme: the hand-written document says the API is open")
	}
	switch {
	case derived && written:
		return present
	case derived || written:
		return partial
	}
	return absent
}

// OA-10: the generated application publishes its document. The probe
// scaffolds `nucleus new --template api`, builds it against this checkout
// and boots it the way `nucleus openapi` does (NUCLEUS_PRINT_ROUTES): the
// application must declare the route it serves the document at, and the
// document must be the OpenAPI 3.1 one (NU-98).
func probeStarterPublishesSpec(t *testing.T, _ *env) verdict {
	root := repoRoot(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"new", "benchapp", "--out", out, "--template", "api", "--module", "example.com/benchapp", "--offline"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Logf("nucleus new exited %d: %s", code, stderr.String())
		return absent
	}
	project := filepath.Join(out, "benchapp")
	goMod := filepath.Join(project, "go.mod")
	raw, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	pinned := regexp.MustCompile(`(?m)^require github\.com/jcsvwinston/nucleus v\S+$`).ReplaceAllString(string(raw), "require github.com/jcsvwinston/nucleus v0.0.0")
	pinned += "\nreplace github.com/jcsvwinston/nucleus => \"" + root + "\"\n" +
		"\nrequire github.com/jcsvwinston/nucleus/drivers/sqlite v0.0.0\n" +
		"\nreplace github.com/jcsvwinston/nucleus/drivers/sqlite => \"" + filepath.Join(root, "drivers", "sqlite") + "\"\n"
	if err := os.WriteFile(goMod, []byte(pinned), 0o644); err != nil {
		t.Fatal(err)
	}
	goCmd := func(args ...string) error {
		cmd := exec.Command("go", args...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Logf("go %v: %v\n%s", args, err, b)
			return err
		}
		return nil
	}
	if goCmd("mod", "tidy") != nil || goCmd("build", "-o", "app", ".") != nil {
		return absent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run := exec.CommandContext(ctx, filepath.Join(project, "app"))
	run.Dir = project
	run.Env = append(os.Environ(), "NUCLEUS_PRINT_ROUTES=1")
	dumped, err := run.Output()
	if err != nil {
		t.Logf("the scaffolded application under NUCLEUS_PRINT_ROUTES: %v", err)
		return absent
	}
	doc, found, err := routedump.Parse(dumped)
	if err != nil || !found {
		t.Logf("no route dump: %v", err)
		return absent
	}
	if doc.OpenAPI == nil {
		t.Log("the scaffolded application derives no OpenAPI document")
		return absent
	}
	var api struct {
		OpenAPI string `json:"openapi"`
	}
	_ = json.Unmarshal(doc.OpenAPI.Document, &api)
	if doc.OpenAPI.Pattern == "" || api.OpenAPI != "3.1.0" {
		t.Logf("the scaffolded application does not serve its document (pattern %q, version %q): `nucleus openapi` can print it, a client cannot fetch it", doc.OpenAPI.Pattern, api.OpenAPI)
		return partial
	}
	t.Logf("the scaffolded application serves its OpenAPI %s document at %s", api.OpenAPI, doc.OpenAPI.Pattern)
	return present
}

// BenchAuthor and BenchArticle are the structs the schema probe derives.
type BenchAuthor struct {
	Name string `json:"name"`
}

type BenchArticle struct {
	Title    string      `json:"title" validate:"required,max=120"`
	Body     string      `json:"body,omitempty"`
	State    string      `json:"state" validate:"oneof=draft live"`
	At       time.Time   `json:"at"`
	Author   BenchAuthor `json:"author"`
	Score    *float64    `json:"score"`
	Untagged bool
	Secret   string `json:"-"`
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type docsWidgets struct{}

func (docsWidgets) Index(c *nucleus.Context) error { return c.NoContent() }
func (docsWidgets) Show(c *nucleus.Context) error  { return c.NoContent() }

// startWithDocument boots the bench module and a "docs" module that
// registers a resource, a grouped route and a write, with the derived
// document served at /openapi.json. enforced keeps the default-deny
// authorizer (with a policy that opens the widget list to anonymous
// callers) instead of the bench's open one.
func startWithDocument(t *testing.T, enforced bool) *nucleustest.Server {
	docs := nucleus.Module[struct{}]{
		Name:   "docs",
		Prefix: "/docs",
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/widgets", Action: "read"},
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Resource("/widgets", docsWidgets{}, nucleus.Methods(nucleus.Index, nucleus.Show))
			r.Post("/widgets", func(c *nucleus.Context) error { return c.NoContent() })
			r.Group("/grouped", func(g nucleus.Router) {
				g.Get("/{id}", func(c *nucleus.Context) error { return c.NoContent() })
			})
		},
	}.Build()
	b := nucleus.New()
	if !enforced {
		b = b.WithOpenAuthz()
	}
	a, err := b.WithOpenAPIDocument("/openapi.json").Mount(benchModule(), docs).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a.Config = benchConfig(t)
	return nucleustest.StartApp(t, a)
}
