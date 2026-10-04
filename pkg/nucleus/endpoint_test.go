// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"net/http"
	"strings"
	"testing"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

type epNote struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type epCreate struct {
	Title string `json:"title" validate:"required,max=20"`
}

type epShow struct {
	ID      int64  `path:"id" validate:"min=1"`
	Verbose bool   `query:"verbose"`
	Tenant  string `header:"X-Tenant"`
}

type epNotes struct{}

func (epNotes) createNote(_ *nucleus.Context, in epCreate) (epNote, error) {
	if in.Title == "taken" {
		return epNote{}, gferrors.Conflict("that title is taken")
	}
	return epNote{ID: 7, Title: in.Title}, nil
}

func (epNotes) showNote(_ *nucleus.Context, in epShow) (epNote, error) {
	return epNote{ID: in.ID, Title: in.Tenant}, nil
}

func (epNotes) purge(_ *nucleus.Context, _ struct{}) (struct{}, error) { return struct{}{}, nil }

// A typed endpoint binds and validates its input, answers its output with
// its status, and the derived document carries both — which the kit then
// holds the answers to.
func TestTypedEndpoints(t *testing.T) {
	n := epNotes{}
	m := nucleus.Module[struct{}]{
		Name:   "notes",
		Prefix: "/notes",
		Routes: func(r nucleus.Router, _ struct{}) {
			nucleus.Handle(r, http.MethodPost, "/", n.createNote, nucleus.Status(http.StatusCreated), nucleus.Summary("Create a note"))
			nucleus.Handle(r, http.MethodGet, "/{id}", n.showNote)
			nucleus.Handle(r, http.MethodDelete, "/", n.purge)
		},
	}.Build()
	a, err := nucleus.New().WithOpenAuthz().WithOpenAPIDocument("/openapi.json").Mount(m).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = docConfig(t)
	srv := nucleustest.StartApp(t, a)

	created := srv.Post("/notes", map[string]any{"title": "fresh"})
	if created.Status != http.StatusCreated || !strings.Contains(string(created.Body), `"title":"fresh"`) {
		t.Fatalf("create → %d %s", created.Status, created.Body)
	}
	srv.AssertConforms(t, created)
	if bad := srv.Post("/notes", map[string]any{"title": ""}); bad.Status != http.StatusUnprocessableEntity && bad.Status != http.StatusBadRequest {
		t.Fatalf("an invalid body → %d %s", bad.Status, bad.Body)
	}
	conflict := srv.Post("/notes", map[string]any{"title": "taken"})
	if conflict.Status != http.StatusConflict {
		t.Fatalf("a domain error → %d %s", conflict.Status, conflict.Body)
	}
	srv.AssertConforms(t, conflict)
	shown := srv.Get("/notes/42?verbose=true", nucleustest.WithHeader("X-Tenant", "acme"))
	if shown.Status != http.StatusOK || !strings.Contains(string(shown.Body), `"id":42`) || !strings.Contains(string(shown.Body), "acme") {
		t.Fatalf("show → %d %s", shown.Status, shown.Body)
	}
	srv.AssertConforms(t, shown)
	if gone := srv.Delete("/notes"); gone.Status != http.StatusNoContent || len(gone.Body) != 0 {
		t.Fatalf("an endpoint with no output → %d %q", gone.Status, gone.Body)
	}

	doc := srv.Document()
	post := doc.Paths["/notes"].Post
	if post == nil || post.OperationID != "createNote" || post.Summary != "Create a note" || post.RequestBody == nil {
		t.Fatalf("POST /notes in the document: %+v", post)
	}
	if _, ok := post.Responses["201"]; !ok {
		t.Errorf("POST /notes responses %v, want 201", post.Responses)
	}
	if _, ok := doc.Components.Schemas["epNote"]; !ok {
		t.Errorf("the output type is not a component: %v", doc.Components.Schemas)
	}
	get := doc.Paths["/notes/{id}"].Get
	var in []string
	for _, p := range get.Parameters {
		in = append(in, p.In+"."+p.Name)
	}
	if strings.Join(in, ",") != "path.id,query.verbose,header.X-Tenant" {
		t.Errorf("GET /notes/{id} parameters %v", in)
	}
	if get.RequestBody != nil {
		t.Error("a GET endpoint documents a body")
	}
	if _, ok := doc.Paths["/notes"].Delete.Responses["204"]; !ok {
		t.Errorf("DELETE /notes responses %v, want 204", doc.Paths["/notes"].Delete.Responses)
	}
	_ = openapi.Document{}
}
