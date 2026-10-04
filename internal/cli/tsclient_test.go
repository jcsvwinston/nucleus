// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

type tsArticle struct {
	ID     int64    `json:"id"`
	Title  string   `json:"title" validate:"required"`
	Status string   `json:"status" validate:"oneof=draft live"`
	Tags   []string `json:"tags,omitempty"`
	Score  *float64 `json:"score"`
}

type tsCreate struct {
	Title string `json:"title" validate:"required"`
}

func tsTestDocument() *openapi.Document {
	doc := openapi.NewDocument("Shop", "1.2.0")
	article := openapi.SchemaOf[tsArticle](doc)
	doc.Paths["/api/articles"] = openapi.PathItem{
		Get: &openapi.Operation{
			OperationID: "listArticles",
			Parameters:  []openapi.Parameter{openapi.QueryParameter("author_id", openapi.Schema{Type: "integer"}, "", false)},
			Responses:   map[string]openapi.Response{"200": openapi.JSONResponse("ok", openapi.ArraySchema(article)), "default": openapi.ErrorResponse("error")},
		},
		Post: &openapi.Operation{
			OperationID: "createArticle",
			RequestBody: openapi.JSONRequestBody(openapi.SchemaOf[tsCreate](doc), true),
			Responses:   map[string]openapi.Response{"201": openapi.JSONResponse("created", article), "default": openapi.ErrorResponse("error")},
		},
	}
	doc.Paths["/api/articles/{id}"] = openapi.PathItem{Delete: &openapi.Operation{
		OperationID: "deleteArticle",
		Parameters:  []openapi.Parameter{openapi.PathParameter("id", openapi.Schema{Type: "integer"}, "")},
		Responses:   map[string]openapi.Response{"204": openapi.EmptyResponse("gone"), "default": openapi.ErrorResponse("error")},
	}}
	doc.Paths["/api/ping"] = openapi.PathItem{Get: &openapi.Operation{
		OperationID: "ping",
		Responses:   map[string]openapi.Response{"default": {Description: "plain handler"}},
	}}
	return doc
}

func TestGenerateTypeScriptClientShape(t *testing.T) {
	ts, err := generateTypeScriptClient(tsTestDocument())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export interface tsArticle {",
		`  status: "draft" | "live";`,
		"  score?: number | null;",
		"  tags?: string[];",
		"async listArticles(params?: { author_id?: number }): Promise<tsArticle[]> {",
		"async createArticle(body: tsCreate): Promise<tsArticle> {",
		"async deleteArticle(id: number): Promise<void> {",
		"async ping(): Promise<unknown> {",
		"export class ApiError extends Error {",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("the client lacks %q:\n%s", want, ts)
		}
	}
}

// nodeStripsTypes reports whether the node on PATH runs a .ts file as is
// (type stripping is on by default from 22.18 and 23.6).
func nodeStripsTypes(t *testing.T) (string, bool) {
	node, err := exec.LookPath("node")
	if err != nil {
		return "", false
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		return "", false
	}
	m := regexp.MustCompile(`^v(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return node, major > 23 || (major == 23 && minor >= 6) || (major == 22 && minor >= 18)
}

// TestGeneratedTypeScriptClientRuns runs the generated client under node
// against a server that answers like a Nucleus application: the typed
// list, a create, a 204, and an error in the framework's envelope read
// back as ApiError.
func TestGeneratedTypeScriptClientRuns(t *testing.T) {
	node, ok := nodeStripsTypes(t)
	if !ok {
		t.Skip("needs node 22.18+ (runs .ts without a build step)")
	}
	ts, err := generateTypeScriptClient(tsTestDocument())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/articles":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "title": "Hello " + r.URL.Query().Get("author_id"), "status": "live", "score": nil}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/articles":
			body, _ := io.ReadAll(r.Body)
			var in map[string]string
			_ = json.Unmarshal(body, &in)
			if in["title"] == "taken" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":{"code":"CONFLICT","message":"that title is taken"}}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 2, "title": in["title"], "status": "draft", "score": 1.5, "auth": r.Header.Get("Authorization")})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/articles/2":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "client.ts"), []byte(ts), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `import { Client, ApiError } from "./client.ts";
import type { tsArticle } from "./client.ts";

const api = new Client({ baseUrl: process.argv[2], token: "t0ken" });
const list: tsArticle[] = await api.listArticles({ author_id: 7 });
if (list.length !== 1 || list[0].title !== "Hello 7" || list[0].score !== null) throw new Error("list: " + JSON.stringify(list));
const created = await api.createArticle({ title: "fresh" });
if (created.id !== 2 || created.status !== "draft") throw new Error("create: " + JSON.stringify(created));
if ((created as unknown as { auth: string }).auth !== "Bearer t0ken") throw new Error("the bearer was not sent");
const gone = await api.deleteArticle(2);
if (gone !== undefined) throw new Error("204 should resolve undefined");
try {
  await api.createArticle({ title: "taken" });
  throw new Error("a 409 did not throw");
} catch (err) {
  if (!(err instanceof ApiError) || err.status !== 409 || err.code !== "CONFLICT" || err.message !== "that title is taken") throw err;
}
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "run.ts", srv.URL)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("node run.ts: %v\n%s\n--- client.ts ---\n%s", err, out, ts)
	}
}
