// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// doublesApp is the application whose routes emit the four things the
// doubles capture: a mail, a file, a job and an outgoing HTTP call.
func doublesApp(t *testing.T, webhookURL string) nucleus.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.JWTSecret = strings.Repeat("nucleustest-secret-", 3)
	cfg.Databases = TempSQLite(t)
	cfg.Storage.Provider = "memory"
	m := nucleus.Module[struct{}]{
		Name: "emit",
		Jobs: func(j nucleus.JobRegistry, _ struct{}) {
			_ = j.Register("emit.tick", nucleus.JobSpec{Every: time.Hour, Handler: func(context.Context) error { return nil }})
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/mail", func(c *nucleus.Context) error {
				rt := runtimeOf(c)
				return rt.Mailer().Send(c.Request.Context(), mail.Message{To: []string{"to@example.test"}, Subject: "welcome", Body: "hi"})
			})
			r.Post("/upload", func(c *nucleus.Context) error {
				rt := runtimeOf(c)
				_, err := rt.Storage().Put(c.Request.Context(), "uploads/hello.txt", strings.NewReader("hello"), storage.PutOptions{ContentType: "text/plain"})
				if err != nil {
					return err
				}
				return c.NoContent()
			})
			r.Post("/enqueue", func(c *nucleus.Context) error {
				rt := runtimeOf(c)
				id, err := rt.Tasks().EnqueueJSON("emit.report", map[string]string{"kind": "daily"})
				if err != nil {
					return err
				}
				return c.JSON(http.StatusAccepted, map[string]string{"id": id})
			})
			r.Post("/notify", func(c *nucleus.Context) error {
				resp, err := http.Post(webhookURL+"/hooks/order", "application/json", strings.NewReader(`{"order":7}`))
				if err != nil {
					return err
				}
				_ = resp.Body.Close()
				return c.JSON(http.StatusOK, map[string]int{"upstream": resp.StatusCode})
			})
		},
	}.Build()
	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		Options: []app.Option{app.WithOpenAuthz()},
	}
}

// runtimeOf is how these fixture handlers reach the runtime: the kit's
// probe module captured it, and the test set it here before the request.
var fixtureRuntime nucleus.Runtime

func runtimeOf(*nucleus.Context) nucleus.Runtime { return fixtureRuntime }

func TestSentMailCapturesWhatTheApplicationSends(t *testing.T) {
	srv := StartApp(t, doublesApp(t, "http://127.0.0.1:1"))
	fixtureRuntime = srv.Runtime()
	if len(srv.SentMail()) != 0 {
		t.Fatal("mail before any request")
	}
	if r := srv.Post("/mail", nil); r.Status/100 != 2 {
		t.Fatalf("POST /mail: %d %s", r.Status, r)
	}
	sent := srv.SentMail()
	if len(sent) != 1 || sent[0].Subject != "welcome" || sent[0].To[0] != "to@example.test" {
		t.Fatalf("captured %+v", sent)
	}
	srv.ResetMail()
	if len(srv.SentMail()) != 0 {
		t.Fatal("ResetMail kept messages")
	}
}

func TestStoredReadsBackWhatTheApplicationPut(t *testing.T) {
	srv := StartApp(t, doublesApp(t, "http://127.0.0.1:1"))
	fixtureRuntime = srv.Runtime()
	if r := srv.Post("/upload", nil); r.Status != http.StatusNoContent {
		t.Fatalf("POST /upload: %d %s", r.Status, r)
	}
	if got := string(srv.Stored("uploads/hello.txt")); got != "hello" {
		t.Fatalf("Stored = %q", got)
	}
	if keys := srv.StoredKeys("uploads/"); len(keys) != 1 || keys[0] != "uploads/hello.txt" {
		t.Fatalf("StoredKeys = %v", keys)
	}
}

func TestEnqueuedTasksRecordsWhatTheApplicationEnqueued(t *testing.T) {
	srv := StartApp(t, doublesApp(t, "http://127.0.0.1:1"))
	fixtureRuntime = srv.Runtime()
	if r := srv.Post("/enqueue", nil); r.Status != http.StatusAccepted {
		t.Fatalf("POST /enqueue: %d %s", r.Status, r)
	}
	recs := srv.EnqueuedTasks()
	if len(recs) != 1 || recs[0].Type != "emit.report" || !strings.Contains(string(recs[0].Payload), `"daily"`) {
		t.Fatalf("recorded %+v", recs)
	}
	srv.ResetEnqueuedTasks()
	if len(srv.EnqueuedTasks()) != 0 {
		t.Fatal("ResetEnqueuedTasks kept records")
	}
}

func TestHTTPRecorderSeesWhatTheApplicationCalls(t *testing.T) {
	rec := NewHTTPRecorder(t)
	rec.Respond(http.StatusCreated, `{"ok":true}`, "Content-Type", "application/json")
	srv := StartApp(t, doublesApp(t, rec.URL))
	fixtureRuntime = srv.Runtime()
	var out map[string]int
	srv.Post("/notify", nil).JSON(t, &out)
	if out["upstream"] != http.StatusCreated {
		t.Fatalf("the application saw upstream %d", out["upstream"])
	}
	reqs := rec.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/hooks/order" || !strings.Contains(string(reqs[0].Body), `"order":7`) {
		t.Fatalf("recorded %+v", reqs)
	}
	// The client variant: a real URL, answered by the recorder.
	resp, err := rec.Client().Get("https://api.example.test/v1/ping?x=1")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	last := rec.Requests()[1]
	if last.Path != "/v1/ping" || last.Query.Get("x") != "1" {
		t.Fatalf("the redirected request arrived as %+v", last)
	}
}
