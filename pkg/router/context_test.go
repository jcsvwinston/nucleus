package router

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

func TestContextHandler_UnifiedAccess(t *testing.T) {
	mux := NewMux()
	mux.Post("/users/{id}", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"id":         c.Param("id"),
			"tenant":     c.Query("tenant"),
			"title":      c.Form("title"),
			"value_id":   c.Value("id"),
			"value_ten":  c.Value("tenant"),
			"value_form": c.Value("title"),
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/users/42?tenant=acme", strings.NewReader("title=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload["id"] != "42" {
		t.Fatalf("expected id=42, got %q", payload["id"])
	}
	if payload["tenant"] != "acme" {
		t.Fatalf("expected tenant=acme, got %q", payload["tenant"])
	}
	if payload["title"] != "hello" {
		t.Fatalf("expected title=hello, got %q", payload["title"])
	}
	if payload["value_id"] != "42" || payload["value_ten"] != "acme" || payload["value_form"] != "hello" {
		t.Fatalf("unexpected unified value resolution: %+v", payload)
	}
}

func TestContext_HTMLWithBindings(t *testing.T) {
	tpl := template.Must(template.New("page.html").Parse(`{{define "page.html"}}{{.Title}}|{{.ID}}|{{.State}}{{end}}`))

	mux := NewMux()
	mux.SetHTMLTemplates(tpl)
	mux.Get("/", func(c *Context) error {
		c.Set("Title", "Dashboard")
		c.BindData(map[string]interface{}{"State": "ok"})
		return c.HTML(http.StatusCreated, "page.html", map[string]interface{}{"ID": 7})
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", ct)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "Dashboard|7|ok" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestContext_XML(t *testing.T) {
	type payload struct {
		XMLName xml.Name `xml:"health"`
		Status  string   `xml:"status"`
	}

	mux := NewMux()
	mux.Get("/xml", func(c *Context) error {
		return c.XML(http.StatusOK, payload{Status: "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/xml", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("unexpected content type: %s", rec.Header().Get("Content-Type"))
	}
	if body := rec.Body.String(); !strings.Contains(body, "<status>ok</status>") {
		t.Fatalf("unexpected xml body: %s", body)
	}
}

func TestContext_Download(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(path, []byte("report-content"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	mux := NewMux()
	mux.Get("/download", func(c *Context) error {
		return c.Download(path, "export.txt")
	})

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment;") || !strings.Contains(cd, `filename="export.txt"`) {
		t.Fatalf("unexpected content disposition: %s", cd)
	}
	if body := rec.Body.String(); body != "report-content" {
		t.Fatalf("unexpected download body: %q", body)
	}
}

func TestContext_SessionHelpers(t *testing.T) {
	sessionManager := auth.NewSessionManager(auth.SessionConfig{})
	mux := NewMux()
	mux.Use(sessionManager.Middleware())
	mux.SetSessionManager(sessionManager)

	mux.Get("/set", func(c *Context) error {
		if err := c.SessionPutString("name", "alice"); err != nil {
			return err
		}
		return c.NoContent()
	})

	mux.Get("/get", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"name": c.SessionGetString("name"),
		})
	})

	setReq := httptest.NewRequest(http.MethodGet, "/set", nil)
	setRec := httptest.NewRecorder()
	mux.ServeHTTP(setRec, setReq)

	if setRec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", setRec.Code)
	}

	var sessionCookie *http.Cookie
	for _, c := range setRec.Result().Cookies() {
		if c.Name == sessionManager.SCS().Cookie.Name {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie after /set")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/get", nil)
	getReq.AddCookie(sessionCookie)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getRec.Code)
	}

	var payload map[string]string
	if err := json.NewDecoder(getRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["name"] != "alice" {
		t.Fatalf("expected session value alice, got %q", payload["name"])
	}
}

func TestContextHandler_EmptyHandlers(t *testing.T) {
	mux := NewMux()
	mux.HandleFunc("/", ContextHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

// An error a handler did not classify is an internal error, and its text
// was written for the log: a driver names tables, a wrapped os error names
// a path. The body must not carry it unless the router was built for
// development, and the log must carry it always — joined to the request by
// its id — or the operator has a 500 with no way to find the cause.
func TestHandleError_UnclassifiedErrorIsMaskedAndLogged(t *testing.T) {
	boom := errors.New("pq: relation \"accounts\" does not exist at /srv/app/internal/x.go")
	handler := func(c *Context) error { return boom }

	t.Run("production masks the body and logs the cause", func(t *testing.T) {
		var log bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&log, nil))
		r := New(logger)
		r.Get("/boom", handler)

		req := httptest.NewRequest(http.MethodGet, "/boom", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "accounts") || strings.Contains(rec.Body.String(), "/srv/app") {
			t.Fatalf("the body leaks the error detail:\n%s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "internal server error") {
			t.Fatalf("expected the generic message, got:\n%s", rec.Body.String())
		}
		if !strings.Contains(log.String(), "accounts") || !strings.Contains(log.String(), "handler error") {
			t.Fatalf("the log must carry the cause:\n%s", log.String())
		}
	})

	t.Run("development puts the cause on the wire", func(t *testing.T) {
		r := New(slog.New(slog.NewTextHandler(io.Discard, nil)), WithDevelopmentErrors(true))
		r.Get("/boom", handler)

		req := httptest.NewRequest(http.MethodGet, "/boom", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "accounts") {
			t.Fatalf("development should expose the detail, got:\n%s", rec.Body.String())
		}
	})

	t.Run("a bare Mux without a router still masks", func(t *testing.T) {
		mux := NewMux()
		mux.Get("/boom", handler)
		req := httptest.NewRequest(http.MethodGet, "/boom", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "accounts") {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
	})
}
