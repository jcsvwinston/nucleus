// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type schemaAuthor struct {
	ID   int64  `json:"id"`
	Name string `json:"name" validate:"required,min=2,max=80" doc:"Display name"`
}

type schemaBase struct {
	CreatedAt time.Time `json:"created_at"`
}

type schemaArticle struct {
	schemaBase
	ID       int64             `json:"id"`
	Title    string            `json:"title" validate:"required,max=200"`
	Body     string            `json:"body,omitempty"`
	Status   string            `json:"status" validate:"oneof=draft published 'in review'"`
	Rating   *float64          `json:"rating"`
	Tags     []string          `json:"tags" validate:"max=5,dive,min=1"`
	Meta     map[string]string `json:"meta,omitempty"`
	Author   schemaAuthor      `json:"author"`
	Ref      uuid.UUID         `json:"ref"`
	Email    string            `json:"email,omitempty" validate:"omitempty,email"`
	Count    uint8             `json:"count"`
	Raw      []byte            `json:"raw,omitempty"`
	Secret   string            `json:"-"`
	internal string
	NoTag    bool
	Parent   *schemaArticle `json:"parent,omitempty"`
}

func TestSchemaOfFollowsEncodingJSON(t *testing.T) {
	doc := NewDocument("t", "1")
	ref := SchemaOf[schemaArticle](doc)
	if ref.Ref != "#/components/schemas/schemaArticle" {
		t.Fatalf("named struct should be referenced, got %+v", ref)
	}
	art := doc.Components.Schemas["schemaArticle"]
	if art.Type != "object" {
		t.Fatalf("article schema type %q", art.Type)
	}

	var names []string
	for n := range art.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	want := []string{"NoTag", "author", "body", "count", "created_at", "email", "id", "meta", "parent", "rating", "raw", "ref", "status", "tags", "title"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("properties %v, want %v (json:\"-\" and unexported left out, the embedded struct flattened)", names, want)
	}

	req := map[string]bool{}
	for _, r := range art.Required {
		req[r] = true
	}
	for _, n := range []string{"id", "title", "status", "tags", "author", "ref", "count", "created_at", "NoTag"} {
		if !req[n] {
			t.Errorf("%s should be required (encoding/json always writes it)", n)
		}
	}
	for _, n := range []string{"body", "meta", "rating", "email", "raw", "parent"} {
		if req[n] {
			t.Errorf("%s should be optional (omitempty or a pointer)", n)
		}
	}

	p := art.Properties
	if p["created_at"].Type != "string" || p["created_at"].Format != "date-time" {
		t.Errorf("time.Time → %+v", p["created_at"])
	}
	if p["title"].MaxLength == nil || *p["title"].MaxLength != 200 {
		t.Errorf("title max → %+v", p["title"])
	}
	if got := p["status"].Enum; len(got) != 3 || got[2] != "in review" {
		t.Errorf("oneof → enum %v", got)
	}
	if !p["rating"].Nullable || p["rating"].Type != "number" {
		t.Errorf("*float64 → %+v, want a nullable number", p["rating"])
	}
	if !p["tags"].Nullable || p["tags"].MaxItems == nil || *p["tags"].MaxItems != 5 || p["tags"].Items.MinLength == nil {
		t.Errorf("tags → %+v (want nullable, maxItems 5, items minLength 1)", p["tags"])
	}
	if p["author"].Ref != "#/components/schemas/schemaAuthor" {
		t.Errorf("nested named struct → %+v", p["author"])
	}
	if p["ref"].Format != "uuid" || p["ref"].Type != "string" {
		t.Errorf("uuid.UUID → %+v", p["ref"])
	}
	if p["email"].Format != "email" {
		t.Errorf("email → %+v", p["email"])
	}
	if p["count"].Minimum == nil || *p["count"].Minimum != 0 {
		t.Errorf("uint8 → %+v", p["count"])
	}
	if p["raw"].Format != "byte" {
		t.Errorf("[]byte → %+v", p["raw"])
	}
	if p["parent"].Ref != "#/components/schemas/schemaArticle" {
		t.Errorf("a self-reference → %+v", p["parent"])
	}
	author := doc.Components.Schemas["schemaAuthor"]
	if author.Properties["name"].Description != "Display name" || author.Properties["name"].MinLength == nil {
		t.Errorf("author.name → %+v", author.Properties["name"])
	}
}

func TestSchemaMarshalsNullableAsTypeList(t *testing.T) {
	s := Schema{Type: "string", Nullable: true, MaxLength: intp(3)}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":["string","null"]`) {
		t.Fatalf("nullable → %s", raw)
	}
	var back Schema
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, s) {
		t.Fatalf("round trip %+v != %+v", back, s)
	}
	var plain Schema
	if err := json.Unmarshal([]byte(`{"type":"integer","format":"int64"}`), &plain); err != nil || plain.Type != "integer" || plain.Nullable {
		t.Fatalf("plain type → %+v %v", plain, err)
	}
}

func TestSchemaForSameNameInTwoPackagesStaysApart(t *testing.T) {
	doc := NewDocument("t", "1")
	type Article struct {
		ID int64 `json:"id"`
	}
	doc.AddSchema("Article", ObjectSchema(map[string]Schema{"hand": {Type: "string"}}))
	ref := SchemaOf[Article](doc)
	if ref.Ref == "#/components/schemas/Article" {
		t.Fatalf("a derived type took the name of a hand-written schema: %+v", ref)
	}
	if _, ok := doc.Components.Schemas["Article"].Properties["hand"]; !ok {
		t.Fatal("the hand-written schema was overwritten")
	}
}

func TestSchemaForGenericName(t *testing.T) {
	if got := sanitizeComponentName("Page[example.com/shop.Article]"); got != "PageArticle" {
		t.Fatalf("generic name → %q", got)
	}
}

func intp(n int) *int { return &n }
