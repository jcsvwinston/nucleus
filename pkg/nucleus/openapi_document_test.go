// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

type docTicketsController struct{}

func (docTicketsController) Index(c *nucleus.Context) error { return c.NoContent() }
func (docTicketsController) Show(c *nucleus.Context) error  { return c.NoContent() }

type docShop struct{}

func (docShop) listArticles(c *nucleus.Context) error  { return c.NoContent() }
func (docShop) createArticle(c *nucleus.Context) error { return c.NoContent() }
func (docShop) showArticle(c *nucleus.Context) error   { return c.NoContent() }

// docModules are the shapes a module registers routes in: flat at the
// application root, under a Prefix, inside a Group, through With, and as a
// Resource.
func docModules() []nucleus.ModuleSpec {
	s := docShop{}
	shop := nucleus.Module[struct{}]{
		Name: "shop",
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/api/articles", Action: "read"},
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/api/articles", s.listArticles)
			r.Post("/api/articles", s.createArticle)
			r.Get("/api/articles/{id}", s.showArticle)
		},
	}.Build()
	desk := nucleus.Module[struct{}]{
		Name:   "desk",
		Prefix: "/desk",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Resource("/tickets", docTicketsController{}, nucleus.Methods(nucleus.Index, nucleus.Show))
			r.Group("/admin", func(g nucleus.Router) {
				g.With(func(next http.Handler) http.Handler { return next }).Delete("/purge/{days}", func(c *nucleus.Context) error { return c.NoContent() })
			})
			r.Get("/", func(c *nucleus.Context) error { return c.NoContent() })
		},
	}.Build()
	return []nucleus.ModuleSpec{shop, desk}
}

func docConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.JWTSecret = strings.Repeat("openapi-derived-doc-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "doc.db")}}
	return cfg
}

func fetchDocument(t *testing.T, srv *nucleustest.Server, path string) openapi.Document {
	t.Helper()
	resp := srv.Get(path)
	if resp.Status != http.StatusOK {
		t.Fatalf("GET %s → %d %s", path, resp.Status, resp.Body)
	}
	var doc openapi.Document
	if err := json.Unmarshal(resp.Body, &doc); err != nil {
		t.Fatalf("decode the served document: %v\n%s", err, resp.Body)
	}
	return doc
}

// TestDerivedDocumentListsEveryRegistration: every way a module registers
// a route reaches the document, at the path the router serves it, under
// the module's tag, named after its handler.
func TestDerivedDocumentListsEveryRegistration(t *testing.T) {
	a, err := nucleus.New().WithOpenAPIDocument("/openapi.json").Mount(docModules()...).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = docConfig(t)
	srv := nucleustest.StartApp(t, a)
	doc := fetchDocument(t, srv, "/openapi.json")

	want := map[string]string{ // "METHOD path" → operationId
		"GET /api/articles":               "listArticles",
		"POST /api/articles":              "createArticle",
		"GET /api/articles/{id}":          "showArticle",
		"GET /desk/tickets":               "indexTickets",
		"GET /desk/tickets/{id}":          "showTickets",
		"DELETE /desk/admin/purge/{days}": "deleteDeskAdminPurgeDays",
		"GET /desk":                       "getDesk",
	}
	got := map[string]string{}
	for p, item := range doc.Paths {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if op := item.Operation(m); op != nil {
				got[m+" "+p] = op.OperationID
			}
		}
	}
	for k, id := range want {
		if got[k] != id {
			t.Errorf("%s: operationId %q, want %q (document lists %v)", k, got[k], id, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("document lists %d operations, want %d: %v", len(got), len(want), got)
	}

	show := doc.Paths["/api/articles/{id}"].Get
	if len(show.Parameters) != 1 || show.Parameters[0].Name != "id" || show.Parameters[0].In != "path" || !show.Parameters[0].Required {
		t.Errorf("GET /api/articles/{id} parameters = %+v, want the required path parameter id", show.Parameters)
	}
	if tags := doc.Paths["/desk/tickets"].Get.Tags; len(tags) != 1 || tags[0] != "desk" {
		t.Errorf("GET /desk/tickets tags = %v, want [desk]", tags)
	}
	if _, ok := doc.Paths["/openapi.json"]; ok {
		t.Error("the document lists its own route")
	}
}

// TestDerivedDocumentSecurityFollowsThePolicy: with the default-deny
// enforcer and a JWT verifier, the document declares the bearer scheme as
// the default and marks exactly the operations anonymous callers reach as
// open — the contract stops saying the API is open by omission (NU-97).
func TestDerivedDocumentSecurityFollowsThePolicy(t *testing.T) {
	a, err := nucleus.New().WithOpenAPIDocument("").Mount(docModules()...).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = docConfig(t)
	srv := nucleustest.StartApp(t, a)
	// Anonymous: the document route is open even under default-deny.
	doc := fetchDocument(t, srv, "/openapi.json")

	scheme, ok := doc.Components.SecuritySchemes["bearerAuth"]
	if !ok || scheme.Type != "http" || scheme.Scheme != "bearer" {
		t.Fatalf("securitySchemes = %+v, want bearerAuth (http bearer)", doc.Components.SecuritySchemes)
	}
	if len(doc.Security) != 1 {
		t.Fatalf("global security = %v, want [bearerAuth]", doc.Security)
	}
	if _, ok := doc.Security[0]["bearerAuth"]; !ok {
		t.Fatalf("global security = %v, want [bearerAuth]", doc.Security)
	}
	list := doc.Paths["/api/articles"].Get
	if list.Security == nil || len(*list.Security) != 0 {
		t.Errorf("GET /api/articles is granted to anonymous by the module's policy: want security [], got %v", list.Security)
	}
	create := doc.Paths["/api/articles"].Post
	if create.Security != nil {
		t.Errorf("POST /api/articles is not granted to anonymous: want the inherited bearer, got %v", *create.Security)
	}
	// And the document tells the truth: the open one answers anonymous, the
	// other one does not.
	if resp := srv.Get("/api/articles"); resp.Status != http.StatusNoContent {
		t.Errorf("anonymous GET /api/articles → %d", resp.Status)
	}
	if resp := srv.Post("/api/articles", map[string]any{}, srv.WithCSRF()); resp.Status != http.StatusUnauthorized && resp.Status != http.StatusForbidden {
		t.Errorf("anonymous POST /api/articles → %d, want 401/403", resp.Status)
	}
}

// TestDerivedDocumentWithOpenAuthzDeclaresNoSecurity: an application that
// enforces nothing must not claim a bearer requirement.
func TestDerivedDocumentWithOpenAuthzDeclaresNoSecurity(t *testing.T) {
	a, err := nucleus.New().WithOpenAuthz().WithOpenAPIDocument("/api.json").Mount(docModules()...).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = docConfig(t)
	srv := nucleustest.StartApp(t, a)
	doc := fetchDocument(t, srv, "/api.json")
	if len(doc.Security) != 0 || len(doc.Components.SecuritySchemes) != 0 {
		t.Errorf("open authz: security %v, schemes %v — want none", doc.Security, doc.Components.SecuritySchemes)
	}
}

// TestDerivedDocumentMergesTheBase: a hand-written base keeps what it says;
// the derivation adds what it does not and fills the security it left out.
func TestDerivedDocumentMergesTheBase(t *testing.T) {
	base := openapi.NewDocument("Shop API", "2.1.0")
	base.AddSchema("Article", openapi.ObjectSchema(map[string]openapi.Schema{"id": openapi.IDSchema()}, "id"))
	base.Paths["/api/articles"] = openapi.PathItem{Get: &openapi.Operation{
		Summary:   "List the articles",
		Responses: map[string]openapi.Response{"200": openapi.JSONResponse("ok", openapi.ArraySchema(openapi.RefSchema("Article")))},
	}}
	a, err := nucleus.New().WithOpenAPIDocument("/openapi.json", base).Mount(docModules()...).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = docConfig(t)
	srv := nucleustest.StartApp(t, a)
	doc := fetchDocument(t, srv, "/openapi.json")

	if doc.Info.Title != "Shop API" || doc.Info.Version != "2.1.0" {
		t.Errorf("info = %+v, want the base's", doc.Info)
	}
	list := doc.Paths["/api/articles"].Get
	if list.Summary != "List the articles" || list.Responses["200"].Content == nil {
		t.Errorf("the base's operation was not kept as written: %+v", list)
	}
	if list.OperationID != "listArticles" || list.Security == nil {
		t.Errorf("the derivation did not fill what the base left out: id %q security %v", list.OperationID, list.Security)
	}
	if doc.Paths["/api/articles"].Post == nil || doc.Paths["/desk/tickets"].Get == nil {
		t.Error("the operations the base does not mention are missing")
	}
	if _, ok := doc.Components.Schemas["Article"]; !ok {
		t.Error("the base's schema is missing")
	}
}

// TestPrintRoutesCarriesTheServedDocument: the NUCLEUS_PRINT_ROUTES exit —
// what `nucleus openapi` reads — carries byte for byte the document the
// route serves.
func TestPrintRoutesCarriesTheServedDocument(t *testing.T) {
	build := func() nucleus.App {
		a, err := nucleus.New().WithOpenAPIDocument("/openapi.json").Mount(docModules()...).Build()
		if err != nil {
			t.Fatal(err)
		}
		a.Config = docConfig(t)
		return a
	}
	srv := nucleustest.StartApp(t, build())
	served := srv.Get("/openapi.json").Body
	srv.Stop()

	var out bytes.Buffer
	restore := nucleus.SetPrintRoutesOutputForTest(&out)
	defer restore()
	t.Setenv("NUCLEUS_PRINT_ROUTES", "1")
	a := build()
	a.Config.Port = 0
	if err := nucleus.Run(a); err != nil {
		t.Fatalf("Run under NUCLEUS_PRINT_ROUTES: %v", err)
	}
	var dump struct {
		OpenAPI struct {
			Pattern  string          `json:"pattern"`
			Document json.RawMessage `json:"document"`
		} `json:"openapi"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &dump); err != nil {
		t.Fatalf("decode the route dump: %v\n%s", err, out.String())
	}
	if dump.OpenAPI.Pattern != "/openapi.json" {
		t.Errorf("dump pattern %q", dump.OpenAPI.Pattern)
	}
	var a1, a2 bytes.Buffer
	_ = json.Compact(&a1, served)
	_ = json.Compact(&a2, dump.OpenAPI.Document)
	if a1.String() != a2.String() {
		t.Errorf("the printed document differs from the served one:\nserved  %s\nprinted %s", a1.String(), a2.String())
	}
}

type docCreateArticle struct {
	Title  string `json:"title" validate:"required,max=20"`
	Status string `json:"status" validate:"oneof=draft live"`
}

// TestOpenAPIValidationEnforcesTheDocument: with WithOpenAPIValidation a
// request that departs from the operation the document declares — here a
// hand-written base's body and query parameter — is answered 400 naming
// each field before the handler runs; a conforming one reaches it.
func TestOpenAPIValidationEnforcesTheDocument(t *testing.T) {
	base := openapi.NewDocument("Shop", "1.0.0")
	body := openapi.SchemaOf[docCreateArticle](base)
	base.Paths["/api/articles"] = openapi.PathItem{Post: &openapi.Operation{
		Parameters:  []openapi.Parameter{openapi.QueryParameter("notify", openapi.Schema{Type: "boolean"}, "", false)},
		RequestBody: openapi.JSONRequestBody(body, true),
		Responses:   map[string]openapi.Response{"204": openapi.EmptyResponse("created")},
	}}
	reached := 0
	shop := nucleus.Module[struct{}]{
		Name:       "shop",
		CSRFExempt: []string{"/api/"},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/api/articles", func(c *nucleus.Context) error { reached++; return c.NoContent() })
			r.Get("/api/articles/{id}", func(c *nucleus.Context) error { reached++; return c.NoContent() })
		},
	}.Build()
	a, err := nucleus.New().WithOpenAPIValidation().WithOpenAuthz().WithOpenAPIDocument("/openapi.json", base).Mount(shop).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = docConfig(t)
	srv := nucleustest.StartApp(t, a)

	resp := srv.Post("/api/articles?notify=perhaps", map[string]any{"title": "a title far longer than twenty", "status": "gone"})
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("a departing request → %d %s", resp.Status, resp.Body)
	}
	for _, field := range []string{"query.notify", "/title", "/status"} {
		if !strings.Contains(string(resp.Body), field) {
			t.Errorf("the 400 does not name %s: %s", field, resp.Body)
		}
	}
	if reached != 0 {
		t.Fatalf("the handler ran for a request the document rejects")
	}
	if resp := srv.Post("/api/articles?notify=true", map[string]any{"title": "short", "status": "live"}); resp.Status != http.StatusNoContent {
		t.Fatalf("a conforming request → %d %s", resp.Status, resp.Body)
	}
	if resp := srv.Get("/api/articles/7"); resp.Status != http.StatusNoContent || reached != 2 {
		t.Fatalf("a plain route the document only knows by path → %d (reached %d)", resp.Status, reached)
	}

	if _, err := nucleus.New().WithOpenAPIValidation().Build(); err == nil {
		t.Fatal("WithOpenAPIValidation without a document must not build")
	}
}
