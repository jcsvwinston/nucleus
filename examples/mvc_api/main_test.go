package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/app"
)

func TestExampleMVCAPIAdmin_Smoke(t *testing.T) {
	a, cleanup := newExampleTestApp(t)
	defer cleanup()

	respHome := mustGET(t, a.Router, "/")
	if respHome.StatusCode != http.StatusOK {
		t.Fatalf("home status=%d", respHome.StatusCode)
	}
	bodyHome := mustReadBody(t, respHome)
	if !strings.Contains(bodyHome, "GoFrame MVC") || !strings.Contains(bodyHome, "API Example") {
		t.Fatalf("home body does not contain title: %s", bodyHome)
	}

	respHealth := mustGET(t, a.Router, "/api/health")
	if respHealth.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", respHealth.StatusCode)
	}
	var health map[string]any
	mustDecodeJSON(t, respHealth.Body, &health)
	if health["status"] != "ok" {
		t.Fatalf("unexpected health payload: %#v", health)
	}

	respArticles := mustGET(t, a.Router, "/api/articles")
	if respArticles.StatusCode != http.StatusOK {
		t.Fatalf("articles status=%d", respArticles.StatusCode)
	}
	var listBefore struct {
		Items []articleDTO `json:"items"`
		Total int          `json:"total"`
	}
	mustDecodeJSON(t, respArticles.Body, &listBefore)
	if listBefore.Total < 1 || len(listBefore.Items) < 1 {
		t.Fatalf("expected seeded data in /api/articles, got total=%d len=%d", listBefore.Total, len(listBefore.Items))
	}

	payload := map[string]any{
		"title":     "E2E Smoke Article",
		"content":   "Created from smoke test",
		"published": true,
	}
	body, _ := json.Marshal(payload)
	createRes := mustRequest(t, a.Router, http.MethodPost, "/api/articles", bytes.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if createRes.StatusCode != http.StatusCreated {
		raw := mustReadBody(t, createRes)
		t.Fatalf("create status=%d body=%s", createRes.StatusCode, raw)
	}
	var created map[string]any
	mustDecodeJSON(t, createRes.Body, &created)
	if created["id"] == nil {
		t.Fatalf("create response missing id: %#v", created)
	}

	respAfter := mustGET(t, a.Router, "/api/articles")
	if respAfter.StatusCode != http.StatusOK {
		t.Fatalf("articles (after create) status=%d", respAfter.StatusCode)
	}
	var listAfter struct {
		Items []articleDTO `json:"items"`
		Total int          `json:"total"`
	}
	mustDecodeJSON(t, respAfter.Body, &listAfter)
	if listAfter.Total <= listBefore.Total {
		t.Fatalf("expected total to increase after create (before=%d after=%d)", listBefore.Total, listAfter.Total)
	}
	if !containsArticleTitle(listAfter.Items, "E2E Smoke Article") {
		t.Fatalf("created article not found in list: %#v", listAfter.Items)
	}

	respAdmin := mustGET(t, a.Router, "/admin/")
	if respAdmin.StatusCode != http.StatusOK {
		t.Fatalf("admin index status=%d", respAdmin.StatusCode)
	}
	bodyAdmin := mustReadBody(t, respAdmin)
	if !strings.Contains(bodyAdmin, "Command palette") {
		t.Fatalf("admin index missing expected content")
	}

	respAdminModels := mustGET(t, a.Router, "/admin/api/models")
	if respAdminModels.StatusCode != http.StatusOK {
		t.Fatalf("admin models status=%d", respAdminModels.StatusCode)
	}
	var adminModels struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	mustDecodeJSON(t, respAdminModels.Body, &adminModels)
	if !containsModel(adminModels.Models, "Article") {
		t.Fatalf("Article model not found in admin models payload: %#v", adminModels.Models)
	}

	respComponents := mustGET(t, a.Router, "/admin/static/components.js")
	if respComponents.StatusCode != http.StatusOK {
		t.Fatalf("components.js status=%d", respComponents.StatusCode)
	}
	bodyComponents := mustReadBody(t, respComponents)
	if !strings.Contains(bodyComponents, "window.AdminUI") {
		t.Fatalf("components.js missing window.AdminUI export")
	}
}

func TestExampleMVCAPI_Minimal_Smoke(t *testing.T) {
	a, cleanup := newExampleTestApp(t)
	defer cleanup()

	respHome := mustGET(t, a.Router, "/")
	if respHome.StatusCode != http.StatusOK {
		t.Fatalf("home status=%d", respHome.StatusCode)
	}
	bodyHome := mustReadBody(t, respHome)
	if !strings.Contains(bodyHome, "GoFrame MVC") || !strings.Contains(bodyHome, "API Example") {
		t.Fatalf("home body does not contain title: %s", bodyHome)
	}

	respHealth := mustGET(t, a.Router, "/api/health")
	if respHealth.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", respHealth.StatusCode)
	}
	var health map[string]any
	mustDecodeJSON(t, respHealth.Body, &health)
	if health["status"] != "ok" {
		t.Fatalf("unexpected health payload: %#v", health)
	}

	respArticles := mustGET(t, a.Router, "/api/articles")
	if respArticles.StatusCode != http.StatusOK {
		t.Fatalf("articles status=%d", respArticles.StatusCode)
	}
	var listBefore struct {
		Items []articleDTO `json:"items"`
		Total int          `json:"total"`
	}
	mustDecodeJSON(t, respArticles.Body, &listBefore)
	if listBefore.Total < 1 || len(listBefore.Items) < 1 {
		t.Fatalf("expected seeded data in /api/articles, got total=%d len=%d", listBefore.Total, len(listBefore.Items))
	}

	payload := map[string]any{
		"title":     "Minimal Smoke Article",
		"content":   "Created from minimal smoke test",
		"published": true,
	}
	body, _ := json.Marshal(payload)
	createRes := mustRequest(t, a.Router, http.MethodPost, "/api/articles", bytes.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if createRes.StatusCode != http.StatusCreated {
		raw := mustReadBody(t, createRes)
		t.Fatalf("create status=%d body=%s", createRes.StatusCode, raw)
	}
	var created map[string]any
	mustDecodeJSON(t, createRes.Body, &created)
	if created["id"] == nil {
		t.Fatalf("create response missing id: %#v", created)
	}
}

func newExampleTestApp(t *testing.T) (*app.App, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "example_test.db")
	cfg := defaultExampleConfig()
	cfg.Databases["default"] = app.DatabaseConfig{
		URL:         "sqlite://" + dbPath,
		MaxOpen:     10,
		MaxIdle:     5,
		MaxLifetime: 5 * time.Minute,
	}
	cfg.Port = 0
	cfg.LogLevel = "error"
	cfg.LogFormat = "text"

	a, err := newExampleApp(cfg)
	if err != nil {
		t.Fatalf("newExampleApp failed: %v", err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	}

	return a, cleanup
}

func mustGET(t *testing.T, handler http.Handler, url string) *http.Response {
	t.Helper()

	return mustRequest(t, handler, http.MethodGet, url, nil, nil)
}

func mustRequest(t *testing.T, handler http.Handler, method, url string, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, url, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result()
}

func mustReadBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	return string(raw)
}

func mustDecodeJSON(t *testing.T, r io.ReadCloser, out any) {
	t.Helper()
	defer r.Close()
	if err := json.NewDecoder(r).Decode(out); err != nil {
		t.Fatalf("decode json failed: %v", err)
	}
}

func containsArticleTitle(items []articleDTO, title string) bool {
	for _, it := range items {
		if it.Title == title {
			return true
		}
	}
	return false
}

func containsModel(models []struct {
	Name string `json:"name"`
}, name string) bool {
	for _, m := range models {
		if m.Name == name {
			return true
		}
	}
	return false
}
