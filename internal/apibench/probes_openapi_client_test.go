// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/cli"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// The client generated from the document (A10 S7). The arc's gate runs the
// generated client against the suite starter in the CI lane that builds the
// starter; the probe measures the generator on an application of its own:
// typed endpoints, the document they derive, the client written from it.

type BenchWidget struct {
	ID   int64  `json:"id"`
	Name string `json:"name" validate:"required"`
}

type benchWidgetInput struct {
	Name string `json:"name" validate:"required,max=20"`
}

type benchWidgetQuery struct {
	ID int64 `path:"id"`
}

func clientServer(t *testing.T) *nucleustest.Server {
	m := nucleus.Module[struct{}]{
		Name:   "widgets",
		Prefix: "/widgets",
		Routes: func(r nucleus.Router, _ struct{}) {
			nucleus.Handle(r, http.MethodPost, "/", func(_ *nucleus.Context, in benchWidgetInput) (BenchWidget, error) {
				if in.Name == "taken" {
					return BenchWidget{}, gferrors.Conflict("taken")
				}
				return BenchWidget{ID: 1, Name: in.Name}, nil
			}, nucleus.Status(http.StatusCreated))
			nucleus.Handle(r, http.MethodGet, "/{id}", func(_ *nucleus.Context, q benchWidgetQuery) (BenchWidget, error) {
				return BenchWidget{ID: q.ID, Name: "kept"}, nil
			})
		},
	}.Build()
	a, err := nucleus.New().WithOpenAuthz().WithOpenAPIDocument("/openapi.json").Mount(m).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a.Config = benchConfig(t)
	return nucleustest.StartApp(t, a)
}

// OA-07: a client is generated from the document — `nucleus openapi
// --client typescript` writes, from the document a typed application
// serves, the types of its schemas and one typed method per operation; and
// where node can run TypeScript, the client is run against the application.
func probeClientGenerator(t *testing.T, _ *env) verdict {
	srv := clientServer(t)
	resp := srv.Get("/openapi.json")
	if resp.Status != http.StatusOK {
		t.Logf("document → %d", resp.Status)
		return absent
	}
	dir := t.TempDir()
	docPath := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(docPath, resp.Body, 0o644); err != nil {
		t.Fatal(err)
	}
	clientPath := filepath.Join(dir, "client.ts")
	var out, errOut bytes.Buffer
	if code := cli.Run([]string{"openapi", "--document", docPath, "--client", "typescript", "--out", clientPath}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Logf("nucleus openapi --client typescript: exit %d: %s", code, errOut.String())
		return absent
	}
	raw, _ := os.ReadFile(clientPath)
	ts := string(raw)
	missing := 0
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`export interface BenchWidget \{`),
		regexp.MustCompile(`async postWidgets\(body: \w+\): Promise<BenchWidget>`),
		regexp.MustCompile(`async getWidgetsId\(id: number\): Promise<BenchWidget>`),
		regexp.MustCompile(`export class ApiError extends Error`),
	} {
		if !want.MatchString(ts) {
			t.Logf("the generated client lacks %s", want)
			missing++
		}
	}
	if missing > 0 {
		t.Logf("client:\n%s", ts)
		return partial
	}

	node, err := exec.LookPath("node")
	if err != nil || !nodeRunsTypeScript(node) {
		t.Log("the generated client is typed from the document; node 22.18+ is not on PATH here, so it is not run (the starter lane runs it)")
		return present
	}
	script := `import { Client, ApiError } from "./client.ts";
import type { BenchWidget } from "./client.ts";
const api = new Client({ baseUrl: process.argv[2] });
const made: BenchWidget = await api.postWidgets({ name: "made" });
if (made.name !== "made") throw new Error("create: " + JSON.stringify(made));
const read: BenchWidget = await api.getWidgetsId(7);
if (read.id !== 7) throw new Error("read: " + JSON.stringify(read));
try {
  await api.postWidgets({ name: "taken" });
  throw new Error("the 409 did not throw");
} catch (e) {
  if (!(e instanceof ApiError) || e.status !== 409) throw e;
}
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "run.ts", srv.BaseURL)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(b), "ok") {
		t.Logf("node run.ts: %v\n%s\nclient:\n%s", err, b, ts)
		return partial
	}
	t.Log("the generated client ran under node against the application: created through it, and the 409 arrived as ApiError")
	return present
}

func nodeRunsTypeScript(node string) bool {
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`^v(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 23 || (major == 23 && minor >= 6) || (major == 22 && minor >= 18)
}
