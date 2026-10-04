// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type valAuthor struct {
	Name string `json:"name" validate:"required,min=2"`
}

type valArticle struct {
	ID     int64      `json:"id"`
	Title  string     `json:"title" validate:"required,max=10"`
	Status string     `json:"status" validate:"oneof=draft live"`
	Email  string     `json:"email,omitempty" validate:"omitempty,email"`
	Tags   []string   `json:"tags,omitempty" validate:"max=2"`
	Author *valAuthor `json:"author,omitempty"`
}

func fields(es ValidationErrors) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Field)
	}
	return strings.Join(out, ",")
}

func TestValidateReportsEveryDeparture(t *testing.T) {
	doc := NewDocument("t", "1")
	ref := SchemaOf[valArticle](doc)

	if errs := doc.ValidateJSON(ref, []byte(`{"id":1,"title":"ok","status":"live"}`)); len(errs) != 0 {
		t.Fatalf("a conforming value: %v", errs)
	}
	errs := doc.ValidateJSON(ref, []byte(`{"id":1.5,"title":"far too long a title","status":"gone","email":"nope","tags":["a","b","c"],"author":{"name":"x"}}`))
	want := "/author/name,/email,/id,/status,/tags,/title"
	if got := fields(errs); got != want {
		t.Fatalf("departures at %s, want %s:\n%v", got, want, errs)
	}
	if errs := doc.ValidateJSON(ref, []byte(`{"title":"ok"}`)); fields(errs) != "/id,/status" {
		t.Fatalf("missing required: %v", errs)
	}
	if errs := doc.ValidateJSON(ref, []byte(`{"id":"1","title":"ok","status":"live"}`)); fields(errs) != "/id" {
		t.Fatalf("a string for an integer: %v", errs)
	}
	if errs := doc.ValidateJSON(ref, []byte(`not json`)); fields(errs) != "body" {
		t.Fatalf("not JSON: %v", errs)
	}
}

func TestValidateNullable(t *testing.T) {
	doc := NewDocument("t", "1")
	s := Schema{Type: "string", Nullable: true}
	if errs := doc.Validate(s, nil); len(errs) != 0 {
		t.Fatalf("null for a nullable string: %v", errs)
	}
	if errs := doc.Validate(Schema{Type: "string"}, nil); len(errs) != 1 {
		t.Fatalf("null for a string: %v", errs)
	}
}

func TestFindOperationPrefersTheStaticPath(t *testing.T) {
	doc := NewDocument("t", "1")
	doc.Paths["/articles/{id}"] = PathItem{Get: &Operation{OperationID: "show"}}
	doc.Paths["/articles/new"] = PathItem{Get: &Operation{OperationID: "new"}}
	r, ok := doc.FindOperation("GET", "/articles/new")
	if !ok || r.Operation.OperationID != "new" {
		t.Fatalf("got %+v", r)
	}
	r, ok = doc.FindOperation("GET", "/articles/42")
	if !ok || r.Operation.OperationID != "show" || r.Params["id"] != "42" {
		t.Fatalf("got %+v", r)
	}
	if _, ok := doc.FindOperation("POST", "/articles/42"); ok {
		t.Fatal("a method the path does not declare matched")
	}
}

func TestValidateRequest(t *testing.T) {
	doc := NewDocument("t", "1")
	doc.Paths["/articles/{id}"] = PathItem{Put: &Operation{
		Parameters: []Parameter{
			PathParameter("id", Schema{Type: "integer", Format: "int64"}, ""),
			QueryParameter("notify", Schema{Type: "boolean"}, "", false),
			{Name: "X-Tenant", In: "header", Required: true, Schema: Schema{Type: "string"}},
		},
		RequestBody: JSONRequestBody(SchemaOf[valArticle](doc), true),
		Responses:   map[string]Response{"200": EmptyResponse("ok")},
	}}

	req := httptest.NewRequest(http.MethodPut, "/articles/abc?notify=maybe", strings.NewReader(`{"id":1,"title":"far too long a title","status":"live"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	route, ok := doc.FindOperation(req.Method, req.URL.Path)
	if !ok {
		t.Fatal("no operation")
	}
	errs := doc.ValidateRequest(req, route)
	if got := fields(errs); got != "path.id,query.notify,header.X-Tenant,/title" {
		t.Fatalf("request departures %s:\n%v", got, errs)
	}
	// The body is still there for the handler.
	rest, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(rest), "far too long") {
		t.Fatalf("the body was consumed: %q", rest)
	}

	ok2 := httptest.NewRequest(http.MethodPut, "/articles/7?notify=true", strings.NewReader(`{"id":7,"title":"short","status":"draft"}`))
	ok2.Header.Set("Content-Type", "application/json")
	ok2.Header.Set("X-Tenant", "acme")
	route, _ = doc.FindOperation(ok2.Method, ok2.URL.Path)
	if errs := doc.ValidateRequest(ok2, route); len(errs) != 0 {
		t.Fatalf("a conforming request: %v", errs)
	}

	empty := httptest.NewRequest(http.MethodPut, "/articles/7", nil)
	empty.Header.Set("X-Tenant", "acme")
	if errs := doc.ValidateRequest(empty, route); fields(errs) != "body" {
		t.Fatalf("a missing required body: %v", errs)
	}
}

func TestValidateResponse(t *testing.T) {
	doc := NewDocument("t", "1")
	doc.Paths["/articles"] = PathItem{Post: &Operation{
		Responses: map[string]Response{
			"201":     JSONResponse("created", SchemaOf[valArticle](doc)),
			"default": ErrorResponse("error"),
		},
	}}
	route, _ := doc.FindOperation("POST", "/articles")
	h := http.Header{"Content-Type": []string{"application/json"}}

	if errs := doc.ValidateResponse(route, 201, h, []byte(`{"id":1,"title":"ok","status":"live"}`)); len(errs) != 0 {
		t.Fatalf("a conforming 201: %v", errs)
	}
	if errs := doc.ValidateResponse(route, 201, h, []byte(`{"id":1,"title":"ok"}`)); fields(errs) != "/status" {
		t.Fatalf("a 201 missing a required field: %v", errs)
	}
	if errs := doc.ValidateResponse(route, 409, h, []byte(`{"error":{"code":"CONFLICT","message":"taken"}}`)); len(errs) != 0 {
		t.Fatalf("an error in the envelope under default: %v", errs)
	}
	if errs := doc.ValidateResponse(route, 409, http.Header{"Content-Type": []string{"text/plain"}}, []byte("taken")); fields(errs) != "body" {
		t.Fatalf("an undeclared content type: %v", errs)
	}
	strict := NewDocument("t", "1")
	strict.Paths["/x"] = PathItem{Get: &Operation{Responses: map[string]Response{"200": EmptyResponse("ok")}}}
	route, _ = strict.FindOperation("GET", "/x")
	if errs := strict.ValidateResponse(route, 500, h, nil); fields(errs) != "status" {
		t.Fatalf("an undeclared status: %v", errs)
	}
}
