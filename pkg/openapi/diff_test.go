// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"strings"
	"testing"
)

type diffArticleV1 struct {
	ID     int64          `json:"id"`
	Title  string         `json:"title" validate:"required,max=200"`
	Body   string         `json:"body,omitempty"`
	Status string         `json:"status" validate:"oneof=draft live"`
	Parent *diffArticleV1 `json:"parent,omitempty"`
}

func diffDoc(body, resp Schema, public bool) *Document {
	doc := NewDocument("t", "1")
	doc.AddSecurityScheme("bearerAuth", BearerAuthScheme("JWT"))
	doc.Security = []SecurityRequirement{Require("bearerAuth")}
	op := &Operation{
		Parameters:  []Parameter{QueryParameter("page", Schema{Type: "integer"}, "", false)},
		RequestBody: JSONRequestBody(body, true),
		Responses:   map[string]Response{"201": JSONResponse("created", resp)},
	}
	if public {
		op.Security = PublicSecurity()
	}
	doc.Paths["/articles"] = PathItem{Post: op, Get: &Operation{Responses: map[string]Response{"200": EmptyResponse("ok")}}}
	return doc
}

func changeList(cs []Change) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.String())
	}
	return strings.Join(out, "\n")
}

func TestBreakingChangesIgnoresWhatAClientCanIgnore(t *testing.T) {
	prev := NewDocument("t", "1")
	ref := SchemaOf[diffArticleV1](prev)
	prev = diffDoc(ref, ref, true)
	SchemaOf[diffArticleV1](prev)

	next := diffDoc(ref, ref, true)
	SchemaOf[diffArticleV1](next)
	// Additions: an optional field in the response, an optional parameter,
	// a new operation, a wider request bound.
	art := next.Components.Schemas["diffArticleV1"]
	art.Properties["slug"] = Schema{Type: "string"}
	art.Properties["title"] = Schema{Type: "string", MaxLength: intp(400)}
	next.Components.Schemas["diffArticleV1"] = art
	post := next.Paths["/articles"].Post
	post.Parameters = append(post.Parameters, QueryParameter("draft", Schema{Type: "boolean"}, "", false))
	next.Paths["/articles/{id}"] = PathItem{Get: &Operation{Responses: map[string]Response{"200": EmptyResponse("ok")}}}

	if cs := BreakingChanges(prev, next); len(cs) != 0 {
		t.Fatalf("additions reported as breaking:\n%s", changeList(cs))
	}
}

func TestBreakingChangesNamesEachBreak(t *testing.T) {
	prev := NewDocument("t", "1")
	ref := SchemaOf[diffArticleV1](prev)
	prev = diffDoc(ref, ref, true)
	SchemaOf[diffArticleV1](prev)

	next := diffDoc(ref, ref, false) // the operation now needs the bearer
	SchemaOf[diffArticleV1](next)
	art := next.Components.Schemas["diffArticleV1"]
	delete(art.Properties, "body")                                          // response field removed
	art.Properties["status"] = Schema{Type: "string", Enum: []any{"draft"}} // request: value no longer accepted
	art.Properties["id"] = Schema{Type: "string"}                           // type changed
	art.Properties["author"] = Schema{Type: "string"}                       //
	art.Required = append(art.Required, "author")                           // new required field in the request
	art.Properties["title"] = Schema{Type: "string", MaxLength: intp(100)}  // tighter bound
	next.Components.Schemas["diffArticleV1"] = art
	post := next.Paths["/articles"].Post
	post.Parameters = append(post.Parameters, QueryParameter("lang", Schema{Type: "string"}, "", true)) // new required parameter
	item := next.Paths["/articles"]
	item.Get = nil // operation removed
	next.Paths["/articles"] = item

	got := changeList(BreakingChanges(prev, next))
	for _, want := range []string{
		"GET /articles: the operation was removed",
		"POST /articles — security: the operation was public and now requires credentials",
		"POST /articles — parameter query.lang: a new required parameter",
		"POST /articles — request body/author: a new required field",
		"POST /articles — request body/id: the type changed from integer to string",
		`POST /articles — request body/status: the values "live" are no longer accepted`,
		"POST /articles — request body/title: the maximum length was lowered",
		"POST /articles — response 201/body: the field was removed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
