package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
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
	if !strings.Contains(bodyHome, "GoFrame MVC") || !strings.Contains(bodyHome, "Showcase") {
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
		Data  []articleDTO `json:"data"`
		Count int          `json:"count"`
	}
	mustDecodeJSON(t, respArticles.Body, &listBefore)
	if listBefore.Count < 1 || len(listBefore.Data) < 1 {
		t.Fatalf("expected seeded data in /api/articles, got count=%d len=%d", listBefore.Count, len(listBefore.Data))
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
	createdData, _ := created["data"].(map[string]any)
	if createdData["id"] == nil {
		t.Fatalf("create response missing id: %#v", created)
	}

	respAfter := mustGET(t, a.Router, "/api/articles")
	if respAfter.StatusCode != http.StatusOK {
		t.Fatalf("articles (after create) status=%d", respAfter.StatusCode)
	}
	var listAfter struct {
		Data  []articleDTO `json:"data"`
		Count int          `json:"count"`
	}
	mustDecodeJSON(t, respAfter.Body, &listAfter)
	if listAfter.Count <= listBefore.Count {
		t.Fatalf("expected count to increase after create (before=%d after=%d)", listBefore.Count, listAfter.Count)
	}
	if !containsArticleTitle(listAfter.Data, "E2E Smoke Article") {
		t.Fatalf("created article not found in list: %#v", listAfter.Data)
	}

	respArticlesPage := mustGET(t, a.Router, "/articles")
	if respArticlesPage.StatusCode != http.StatusOK {
		t.Fatalf("articles page status=%d", respArticlesPage.StatusCode)
	}
	bodyArticlesPage := mustReadBody(t, respArticlesPage)
	if !strings.Contains(bodyArticlesPage, "Published Articles") || !strings.Contains(bodyArticlesPage, "Welcome to GoFrame") {
		t.Fatalf("articles page missing expected content: %s", bodyArticlesPage)
	}
	if strings.Contains(bodyArticlesPage, "Draft roadmap note") {
		t.Fatalf("draft article should not appear on public articles page")
	}

	// Live feature-flag demo: published_only (default false for preview mode).
	unpublishedPayload := map[string]any{
		"title":     "Draft Preview Article",
		"content":   "Should appear only when preview mode is enabled",
		"published": false,
	}
	unpublishedBody, _ := json.Marshal(unpublishedPayload)
	unpublishedRes := mustRequest(t, a.Router, http.MethodPost, "/api/articles", bytes.NewReader(unpublishedBody), map[string]string{
		"Content-Type": "application/json",
	})
	if unpublishedRes.StatusCode != http.StatusCreated {
		raw := mustReadBody(t, unpublishedRes)
		t.Fatalf("create unpublished status=%d body=%s", unpublishedRes.StatusCode, raw)
	}

	respLiveFlagDefault := mustGET(t, a.Router, "/api/articles/live-flag")
	if respLiveFlagDefault.StatusCode != http.StatusOK {
		t.Fatalf("live-flag default status=%d", respLiveFlagDefault.StatusCode)
	}
	var liveFlagDefault struct {
		FeatureFlag string       `json:"feature_flag"`
		Enabled     bool         `json:"enabled"`
		Mode        string       `json:"mode"`
		Data        []articleDTO `json:"data"`
	}
	mustDecodeJSON(t, respLiveFlagDefault.Body, &liveFlagDefault)
	if liveFlagDefault.FeatureFlag != "articles_preview_mode" {
		t.Fatalf("unexpected feature_flag: %q", liveFlagDefault.FeatureFlag)
	}
	if liveFlagDefault.Enabled {
		t.Fatalf("expected default preview mode disabled")
	}
	if liveFlagDefault.Mode != "published_only" {
		t.Fatalf("unexpected mode when disabled: %q", liveFlagDefault.Mode)
	}
	if containsArticleTitle(liveFlagDefault.Data, "Draft Preview Article") {
		t.Fatalf("draft article should not be visible with preview mode disabled")
	}

	a.Admin.SetFeatureFlag("articles_preview_mode", true)
	respLiveFlagEnabled := mustGET(t, a.Router, "/api/articles/live-flag")
	if respLiveFlagEnabled.StatusCode != http.StatusOK {
		t.Fatalf("live-flag enabled status=%d", respLiveFlagEnabled.StatusCode)
	}
	var liveFlagEnabled struct {
		Enabled bool         `json:"enabled"`
		Mode    string       `json:"mode"`
		Data    []articleDTO `json:"data"`
	}
	mustDecodeJSON(t, respLiveFlagEnabled.Body, &liveFlagEnabled)
	if !liveFlagEnabled.Enabled {
		t.Fatalf("expected preview mode enabled")
	}
	if liveFlagEnabled.Mode != "preview_all" {
		t.Fatalf("unexpected mode when enabled: %q", liveFlagEnabled.Mode)
	}
	if !containsArticleTitle(liveFlagEnabled.Data, "Draft Preview Article") {
		t.Fatalf("draft article should be visible with preview mode enabled")
	}

	respContactPage := mustGET(t, a.Router, "/contact")
	if respContactPage.StatusCode != http.StatusOK {
		t.Fatalf("contact page status=%d", respContactPage.StatusCode)
	}
	bodyContactPage := mustReadBody(t, respContactPage)
	if !strings.Contains(bodyContactPage, "Request a Demo") {
		t.Fatalf("contact page missing expected content: %s", bodyContactPage)
	}

	respContactInvalid := mustRequest(t, a.Router, http.MethodPost, "/contact", strings.NewReader(url.Values{
		"name":       {"A"},
		"email":      {"not-an-email"},
		"company":    {"Bad Corp"},
		"wants_demo": {"1"},
	}.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if respContactInvalid.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("contact invalid status=%d", respContactInvalid.StatusCode)
	}
	bodyContactInvalid := mustReadBody(t, respContactInvalid)
	if !strings.Contains(bodyContactInvalid, "must be a valid email address") {
		t.Fatalf("contact invalid body missing validation message: %s", bodyContactInvalid)
	}

	respLeadsBefore := mustGET(t, a.Router, "/api/leads")
	if respLeadsBefore.StatusCode != http.StatusOK {
		t.Fatalf("leads before status=%d", respLeadsBefore.StatusCode)
	}
	var leadsBefore struct {
		Data  []leadDTO `json:"data"`
		Count int       `json:"count"`
	}
	mustDecodeJSON(t, respLeadsBefore.Body, &leadsBefore)

	respContactCreate := mustRequest(t, a.Router, http.MethodPost, "/contact", strings.NewReader(url.Values{
		"name":       {"Grace Hopper"},
		"email":      {"grace@example.com"},
		"company":    {"Compilers Inc."},
		"wants_demo": {"1"},
	}.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if respContactCreate.StatusCode != http.StatusSeeOther {
		body := mustReadBody(t, respContactCreate)
		t.Fatalf("contact create status=%d body=%s", respContactCreate.StatusCode, body)
	}
	if got := respContactCreate.Header.Get("Location"); got != "/contact?submitted=1" {
		t.Fatalf("expected redirect to success state, got %q", got)
	}

	respContactSubmitted := mustGET(t, a.Router, "/contact?submitted=1")
	if respContactSubmitted.StatusCode != http.StatusOK {
		t.Fatalf("contact submitted status=%d", respContactSubmitted.StatusCode)
	}
	bodyContactSubmitted := mustReadBody(t, respContactSubmitted)
	if !strings.Contains(bodyContactSubmitted, "Gracias") {
		t.Fatalf("contact submitted body missing success message: %s", bodyContactSubmitted)
	}

	respLeadsAfter := mustGET(t, a.Router, "/api/leads")
	if respLeadsAfter.StatusCode != http.StatusOK {
		t.Fatalf("leads after status=%d", respLeadsAfter.StatusCode)
	}
	var leadsAfter struct {
		Data  []leadDTO `json:"data"`
		Count int       `json:"count"`
	}
	mustDecodeJSON(t, respLeadsAfter.Body, &leadsAfter)
	if leadsAfter.Count <= leadsBefore.Count {
		t.Fatalf("expected lead count to increase (before=%d after=%d)", leadsBefore.Count, leadsAfter.Count)
	}
	if !containsLeadEmail(leadsAfter.Data, "grace@example.com") {
		t.Fatalf("expected submitted lead to appear in API payload: %#v", leadsAfter.Data)
	}

	respAppDashboardRedirect := mustGET(t, a.Router, "/app/dashboard")
	if respAppDashboardRedirect.StatusCode != http.StatusSeeOther {
		t.Fatalf("app dashboard unauthenticated status=%d", respAppDashboardRedirect.StatusCode)
	}
	if got := respAppDashboardRedirect.Header.Get("Location"); got != "/app/login" {
		t.Fatalf("expected redirect to /app/login, got %q", got)
	}

	appCookies := mustAppLogin(t, a.Router, "/app/login", demoAppUsername, demoAppPassword)
	respAppDashboard := mustRequestWithCookies(t, a.Router, http.MethodGet, "/app/dashboard", nil, nil, appCookies)
	if respAppDashboard.StatusCode != http.StatusOK {
		t.Fatalf("app dashboard authenticated status=%d", respAppDashboard.StatusCode)
	}
	bodyAppDashboard := mustReadBody(t, respAppDashboard)
	if !strings.Contains(bodyAppDashboard, "Showcase Dashboard") || !strings.Contains(bodyAppDashboard, demoAppUsername) {
		t.Fatalf("dashboard body missing expected content: %s", bodyAppDashboard)
	}
	if !strings.Contains(bodyAppDashboard, "Grace Hopper") || !strings.Contains(bodyAppDashboard, "E2E Smoke Article") {
		t.Fatalf("dashboard missing recent business data: %s", bodyAppDashboard)
	}

	respAdmin := mustGET(t, a.Router, "/admin/")
	if respAdmin.StatusCode != http.StatusFound {
		t.Fatalf("admin index status=%d", respAdmin.StatusCode)
	}
	if got := respAdmin.Header.Get("Location"); !strings.HasPrefix(got, "/admin/login?next=") {
		t.Fatalf("expected redirect to /admin/login with next parameter, got %q", got)
	}

	adminCookies := mustAdminLogin(t, a.Router, "/admin/login", "admin", "supersecret123")

	respAdmin = mustRequestWithCookies(t, a.Router, http.MethodGet, "/admin/", nil, nil, adminCookies)
	if respAdmin.StatusCode != http.StatusOK {
		t.Fatalf("admin index authenticated status=%d", respAdmin.StatusCode)
	}
	bodyAdmin := mustReadBody(t, respAdmin)
	if !strings.Contains(bodyAdmin, `content="/admin"`) {
		t.Fatalf("admin index missing injected prefix metadata")
	}
	if !strings.Contains(bodyAdmin, `./assets/`) {
		t.Fatalf("admin index missing Vite asset references")
	}

	respAdminModels := mustRequestWithCookies(t, a.Router, http.MethodGet, "/admin/api/models", nil, nil, adminCookies)
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

	assetPath := regexp.MustCompile(`\./assets/[^"]+\.js`).FindString(bodyAdmin)
	if assetPath == "" {
		t.Fatalf("admin index missing javascript asset path")
	}

	respComponents := mustRequestWithCookies(t, a.Router, http.MethodGet, "/admin/"+strings.TrimPrefix(assetPath, "./"), nil, nil, adminCookies)
	if respComponents.StatusCode != http.StatusOK {
		t.Fatalf("vite asset status=%d", respComponents.StatusCode)
	}
	bodyComponents := mustReadBody(t, respComponents)
	if !strings.Contains(bodyComponents, "createRoot") {
		t.Fatalf("vite asset missing expected bundle content")
	}

	respOpenAPI := mustGET(t, a.Router, "/openapi.json")
	if respOpenAPI.StatusCode != http.StatusOK {
		t.Fatalf("openapi status=%d", respOpenAPI.StatusCode)
	}
	var openapiPayload map[string]any
	mustDecodeJSON(t, respOpenAPI.Body, &openapiPayload)
	if openapiPayload["openapi"] != "3.1.0" {
		t.Fatalf("unexpected openapi payload: %#v", openapiPayload)
	}

	respRuntime := mustGET(t, a.Router, "/api/demo/runtime")
	if respRuntime.StatusCode != http.StatusOK {
		t.Fatalf("runtime demo status=%d", respRuntime.StatusCode)
	}
	var runtimePayload map[string]any
	mustDecodeJSON(t, respRuntime.Body, &runtimePayload)
	if runtimePayload["openapi_path"] != "/openapi.json" {
		t.Fatalf("unexpected runtime payload: %#v", runtimePayload)
	}

	respOutbox := mustRequest(t, a.Router, http.MethodPost, "/api/demo/outbox", nil, nil)
	if respOutbox.StatusCode != http.StatusCreated {
		raw := mustReadBody(t, respOutbox)
		t.Fatalf("enqueue outbox status=%d body=%s", respOutbox.StatusCode, raw)
	}
	respDrain := mustRequest(t, a.Router, http.MethodPost, "/api/demo/outbox/drain", nil, nil)
	if respDrain.StatusCode != http.StatusOK {
		raw := mustReadBody(t, respDrain)
		t.Fatalf("drain outbox status=%d body=%s", respDrain.StatusCode, raw)
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
	if !strings.Contains(bodyHome, "GoFrame MVC") || !strings.Contains(bodyHome, "Showcase") {
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
		Data  []articleDTO `json:"data"`
		Count int          `json:"count"`
	}
	mustDecodeJSON(t, respArticles.Body, &listBefore)
	if listBefore.Count < 1 || len(listBefore.Data) < 1 {
		t.Fatalf("expected seeded data in /api/articles, got count=%d len=%d", listBefore.Count, len(listBefore.Data))
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
	createdData, _ := created["data"].(map[string]any)
	if createdData["id"] == nil {
		t.Fatalf("create response missing id: %#v", created)
	}

	respArticlesPage := mustGET(t, a.Router, "/articles")
	if respArticlesPage.StatusCode != http.StatusOK {
		t.Fatalf("articles page status=%d", respArticlesPage.StatusCode)
	}

	respContactPage := mustGET(t, a.Router, "/contact")
	if respContactPage.StatusCode != http.StatusOK {
		t.Fatalf("contact page status=%d", respContactPage.StatusCode)
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
	cfg.AdminBootstrapUsername = "admin"
	cfg.AdminBootstrapEmail = "admin@example.com"
	cfg.AdminBootstrapPassword = "supersecret123"

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

func mustRequestWithCookies(
	t *testing.T,
	handler http.Handler,
	method string,
	url string,
	body io.Reader,
	headers map[string]string,
	cookies []*http.Cookie,
) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, url, body)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result()
}

func mustAdminLogin(t *testing.T, handler http.Handler, loginPath, username, password string) []*http.Cookie {
	t.Helper()

	form := url.Values{
		"username": {username},
		"password": {password},
		"next":     {"/admin/"},
	}
	res := mustRequest(t, handler, http.MethodPost, loginPath, strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if res.StatusCode != http.StatusSeeOther {
		body := mustReadBody(t, res)
		t.Fatalf("admin login status=%d body=%s", res.StatusCode, body)
	}
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("admin login did not set any session cookie")
	}
	return cookies
}

func mustAppLogin(t *testing.T, handler http.Handler, loginPath, username, password string) []*http.Cookie {
	t.Helper()

	form := url.Values{
		"username": {username},
		"password": {password},
	}
	res := mustRequest(t, handler, http.MethodPost, loginPath, strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if res.StatusCode != http.StatusSeeOther {
		body := mustReadBody(t, res)
		t.Fatalf("app login status=%d body=%s", res.StatusCode, body)
	}
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatal("app login did not set any session cookie")
	}
	return cookies
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

func containsLeadEmail(items []leadDTO, email string) bool {
	for _, it := range items {
		if it.Email == email {
			return true
		}
	}
	return false
}
