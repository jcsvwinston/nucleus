package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/auth"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
	"github.com/jcsvwinston/GoFrame/pkg/router"
)

type AdminUser struct {
	model.BaseModel
	Email  string `db:"column:email;required" json:"email" admin:"list,search"`
	Name   string `db:"column:name;required" json:"name" admin:"list,search"`
	Active bool   `db:"column:active" json:"active" admin:"list,filter"`
}

func (AdminUser) TableName() string {
	return "admin_users"
}

type testAdminAuth struct {
	user  *auth.User
	allow map[string]bool
}

func (a *testAdminAuth) Authenticate(_ *http.Request) (*auth.User, error) {
	if a.user == nil {
		return nil, fmt.Errorf("missing auth user")
	}
	return a.user, nil
}

func (a *testAdminAuth) Authorize(_ *auth.User, modelName string, action string) bool {
	if a == nil {
		return true
	}
	key := modelName + ":" + action
	allowed, ok := a.allow[key]
	if !ok {
		return true
	}
	return allowed
}

func (a *testAdminAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("login"))
	})
}

func TestPanel_CRUDWithDualEngine(t *testing.T) {
	engines := []db.Engine{db.EngineSQL}

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			panel, cleanup := setupPanelForTest(t, engine)
			defer cleanup()

			srv := httptest.NewServer(panel.Handler())
			defer srv.Close()

			created := createAdminUser(t, srv.URL, map[string]interface{}{
				"email":  fmt.Sprintf("%s-user@example.com", engine),
				"name":   "Admin Tester",
				"active": true,
			})
			if created.ID == 0 {
				t.Fatal("expected non-zero ID after create")
			}

			resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser", nil)
			if status != http.StatusOK {
				t.Fatalf("list status: got %d, body=%s", status, mustJSON(resp))
			}
			if int(resp["total"].(float64)) != 1 {
				t.Fatalf("expected total=1, got %v", resp["total"])
			}

			modelsResp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models", nil)
			if status != http.StatusOK {
				t.Fatalf("models status: got %d, body=%s", status, mustJSON(modelsResp))
			}

			modelsRaw, ok := modelsResp["models"].([]interface{})
			if !ok || len(modelsRaw) == 0 {
				t.Fatalf("expected at least one model in /api/models: %#v", modelsResp)
			}

			found := false
			for _, raw := range modelsRaw {
				item, _ := raw.(map[string]interface{})
				if item["name"] == "AdminUser" {
					found = true
					if int(item["count"].(float64)) != 1 {
						t.Fatalf("expected model count=1, got %v", item["count"])
					}
				}
			}
			if !found {
				t.Fatalf("AdminUser model not found in /api/models response: %#v", modelsResp)
			}
		})
	}
}

func TestPanel_ListFilterAndOrder(t *testing.T) {
	engines := []db.Engine{db.EngineSQL}

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			panel, cleanup := setupPanelForTest(t, engine)
			defer cleanup()

			srv := httptest.NewServer(panel.Handler())
			defer srv.Close()

			_ = createAdminUser(t, srv.URL, map[string]interface{}{
				"email":  fmt.Sprintf("%s-alpha@example.com", engine),
				"name":   "Alpha",
				"active": true,
			})
			_ = createAdminUser(t, srv.URL, map[string]interface{}{
				"email":  fmt.Sprintf("%s-omega@example.com", engine),
				"name":   "Omega",
				"active": false,
			})
			_ = createAdminUser(t, srv.URL, map[string]interface{}{
				"email":  fmt.Sprintf("%s-zulu@example.com", engine),
				"name":   "Zulu",
				"active": true,
			})

			u, err := url.Parse(srv.URL + "/api/models/AdminUser")
			if err != nil {
				t.Fatalf("url.Parse failed: %v", err)
			}
			q := u.Query()
			q.Set("active", "1")
			q.Set("order_by", "name asc")
			u.RawQuery = q.Encode()

			resp, status := doJSON(t, http.MethodGet, u.String(), nil)
			if status != http.StatusOK {
				t.Fatalf("status=%d body=%s", status, mustJSON(resp))
			}

			if int(resp["total"].(float64)) != 2 {
				t.Fatalf("expected total 2 after active=true filter, got %v", resp["total"])
			}

			items, ok := resp["items"].([]interface{})
			if !ok || len(items) != 2 {
				t.Fatalf("expected 2 items, got %#v", resp["items"])
			}

			first := items[0].(map[string]interface{})
			second := items[1].(map[string]interface{})
			if first["name"] != "Alpha" || second["name"] != "Zulu" {
				t.Fatalf("unexpected order: first=%v second=%v", first["name"], second["name"])
			}
		})
	}
}

func TestPanel_ListRejectsInvalidOrderBy(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/models/AdminUser?order_by=drop table users", nil)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("expected status 400, got %d body=%s", res.StatusCode, string(raw))
	}
}

func TestPanel_ListValidationRejectsInvalidQueryParams(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	cases := []struct {
		name string
		url  string
	}{
		{
			name: "invalid_page",
			url:  srv.URL + "/api/models/AdminUser?page=0",
		},
		{
			name: "invalid_page_size",
			url:  srv.URL + "/api/models/AdminUser?page_size=abc",
		},
		{
			name: "page_size_too_large",
			url:  srv.URL + "/api/models/AdminUser?page_size=201",
		},
		{
			name: "unknown_filter_field",
			url:  srv.URL + "/api/models/AdminUser?nope=value",
		},
		{
			name: "non_filterable_field",
			url:  srv.URL + "/api/models/AdminUser?name=Alpha",
		},
		{
			name: "invalid_boolean_filter_value",
			url:  srv.URL + "/api/models/AdminUser?active=maybe",
		},
		{
			name: "search_too_long",
			url:  srv.URL + "/api/models/AdminUser?search=" + strings.Repeat("x", 257),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := doJSON(t, http.MethodGet, tc.url, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("expected status=400, got=%d body=%s", status, mustJSON(resp))
			}
			errMap, _ := resp["error"].(map[string]interface{})
			if errMap["code"] != "BAD_REQUEST" {
				t.Fatalf("expected BAD_REQUEST code, got %#v", resp)
			}
		})
	}
}

func TestPanel_BulkExportSelected(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	selected := createAdminUser(t, srv.URL, map[string]interface{}{
		"email":  "selected@example.com",
		"name":   "Selected",
		"active": true,
	})
	_ = createAdminUser(t, srv.URL, map[string]interface{}{
		"email":  "other@example.com",
		"name":   "Other",
		"active": true,
	})

	payload := map[string]interface{}{
		"action": "export",
		"ids":    []uint{selected.ID},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bulk export request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("bulk export status=%d body=%s", res.StatusCode, string(raw))
	}

	var out map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode bulk export failed: %v", err)
	}
	exportURL, _ := out["export_url"].(string)
	if exportURL == "" {
		t.Fatalf("expected export_url in bulk export response: %#v", out)
	}

	exportRes, err := http.Get(srv.URL + exportURL)
	if err != nil {
		t.Fatalf("export request failed: %v", err)
	}
	defer exportRes.Body.Close()
	rawCSV, _ := io.ReadAll(exportRes.Body)
	bodyStr := string(rawCSV)

	if !strings.Contains(bodyStr, "selected@example.com") {
		t.Fatalf("expected selected record in csv, got: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "other@example.com") {
		t.Fatalf("did not expect unselected record in csv: %s", bodyStr)
	}
}

func TestPanel_BulkDelete_ErrorReport(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	created := createAdminUser(t, srv.URL, map[string]interface{}{
		"email":  "bulk-delete@example.com",
		"name":   "BulkDelete",
		"active": true,
	})

	reqPayload := map[string]interface{}{
		"action": "delete",
		"ids":    []uint{created.ID, 999999},
	}
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", reqPayload)
	if status != http.StatusOK {
		t.Fatalf("bulk delete status=%d body=%s", status, mustJSON(resp))
	}
	if int(resp["requested"].(float64)) != 2 {
		t.Fatalf("expected requested=2, got %#v", resp["requested"])
	}
	if int(resp["deleted"].(float64)) != 1 {
		t.Fatalf("expected deleted=1, got %#v", resp["deleted"])
	}
	if int(resp["failed"].(float64)) != 1 {
		t.Fatalf("expected failed=1, got %#v", resp["failed"])
	}
	errorsRaw, ok := resp["errors"].([]interface{})
	if !ok || len(errorsRaw) != 1 {
		t.Fatalf("expected one bulk delete error entry, got %#v", resp["errors"])
	}
	errRow, _ := errorsRaw[0].(map[string]interface{})
	if int(errRow["id"].(float64)) != 999999 {
		t.Fatalf("unexpected failed id row: %#v", errRow)
	}
}

func TestPanel_Authorization_ActionLevelCreateDenied(t *testing.T) {
	adminAuth := &testAdminAuth{
		user: &auth.User{ID: "1", Username: "admin"},
		allow: map[string]bool{
			"AdminUser:create": false,
		},
	}

	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, adminAuth)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	payload := map[string]interface{}{
		"email":  "denied@example.com",
		"name":   "Denied",
		"active": true,
	}
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/AdminUser", payload)
	if status != http.StatusForbidden {
		t.Fatalf("expected forbidden create status, got %d body=%s", status, mustJSON(resp))
	}
	errMap, _ := resp["error"].(map[string]interface{})
	if errMap["code"] != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN error code, got %#v", resp)
	}
}

func TestPanel_Authorization_ActionLevelExportDenied(t *testing.T) {
	adminAuth := &testAdminAuth{
		user: &auth.User{ID: "1", Username: "admin"},
		allow: map[string]bool{
			"AdminUser:export_csv": false,
		},
	}

	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, adminAuth)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/models/AdminUser/export?ids=1")
	if err != nil {
		t.Fatalf("export request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected forbidden export status, got %d body=%s", res.StatusCode, string(body))
	}
}

func TestPanel_UIAssetsServedUnderPrefix(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	root := router.NewMux()
	root.Mount("/admin", panel.Handler())
	srv := httptest.NewServer(root)
	defer srv.Close()

	indexRes, err := http.Get(srv.URL + "/admin/")
	if err != nil {
		t.Fatalf("index request failed: %v", err)
	}
	defer indexRes.Body.Close()
	if indexRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(indexRes.Body)
		t.Fatalf("index status=%d body=%s", indexRes.StatusCode, string(body))
	}
	indexBody, _ := io.ReadAll(indexRes.Body)
	indexStr := string(indexBody)
	if !strings.Contains(indexStr, `static/components.js`) {
		t.Fatalf("index is missing components script: %s", indexStr)
	}
	if !strings.Contains(indexStr, `id="cmdk-list" role="listbox"`) {
		t.Fatalf("index is missing command palette listbox semantics: %s", indexStr)
	}

	componentsRes, err := http.Get(srv.URL + "/admin/static/components.js")
	if err != nil {
		t.Fatalf("components request failed: %v", err)
	}
	defer componentsRes.Body.Close()
	if componentsRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(componentsRes.Body)
		t.Fatalf("components status=%d body=%s", componentsRes.StatusCode, string(body))
	}
	componentsBody, _ := io.ReadAll(componentsRes.Body)
	componentsStr := string(componentsBody)
	if !strings.Contains(componentsStr, "window.AdminUI") {
		t.Fatalf("components file missing window.AdminUI export: %s", componentsStr)
	}
	if !strings.Contains(componentsStr, "function error(") {
		t.Fatalf("components file missing error helper: %s", componentsStr)
	}
}

func TestPanel_ListSessions_WithoutSessionManager(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/sessions", nil)
	if status != http.StatusOK {
		t.Fatalf("sessions status: got %d, body=%s", status, mustJSON(resp))
	}
	if enabled, _ := resp["enabled"].(bool); enabled {
		t.Fatalf("expected sessions endpoint disabled without configured session manager: %#v", resp)
	}
}

func TestPanel_ListSessions_WithSessionManager(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	sessionManager := auth.NewSessionManager(auth.SessionConfig{
		Lifetime: 2 * time.Hour,
	})
	panel.config.Session = sessionManager
	panel.config.SessionStore = "memory"
	panel.config.SessionRuntime = auth.SessionRuntimeIdentity{
		Pod:      "pod-1",
		Host:     "node-1",
		Instance: "pod-1@node-1",
	}

	deadline := time.Now().UTC().Add(90 * time.Minute)
	values := map[string]interface{}{
		auth.SessionMetaFirstSeenAtKey: "2026-04-05T10:00:00Z",
		auth.SessionMetaLastSeenAtKey:  "2026-04-05T10:10:00Z",
		auth.SessionMetaPodKey:         "pod-2",
		auth.SessionMetaHostKey:        "node-2",
		auth.SessionMetaInstanceKey:    "pod-2@node-2",
		"user_id":                      "42",
	}

	payload, err := sessionManager.SCS().Codec.Encode(deadline, values)
	if err != nil {
		t.Fatalf("encode session payload failed: %v", err)
	}
	if err := sessionManager.SCS().Store.Commit("token-abc-123", payload, deadline); err != nil {
		t.Fatalf("commit session payload failed: %v", err)
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/sessions?limit=50", nil)
	if status != http.StatusOK {
		t.Fatalf("sessions status: got %d, body=%s", status, mustJSON(resp))
	}
	if enabled, _ := resp["enabled"].(bool); !enabled {
		t.Fatalf("expected enabled sessions response: %#v", resp)
	}
	if store, _ := resp["store"].(string); store != "memory" {
		t.Fatalf("expected store=memory, got %q", store)
	}
	if current, _ := resp["current_active"].(float64); int(current) != 1 {
		t.Fatalf("expected current_active=1, got %v", current)
	}

	sessionsRaw, ok := resp["sessions"].([]interface{})
	if !ok || len(sessionsRaw) != 1 {
		t.Fatalf("expected one session row, got %#v", resp["sessions"])
	}
	row := sessionsRaw[0].(map[string]interface{})
	if row["pod"] != "pod-2" || row["host"] != "node-2" {
		t.Fatalf("expected pod/host from session metadata, got row=%#v", row)
	}
	if row["user"] != "42" {
		t.Fatalf("expected user=42, got row=%#v", row)
	}
}

func TestPanel_SessionsCounterGrowsAcrossBrowsers(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	sessionManager := auth.NewSessionManager(auth.SessionConfig{
		Lifetime: 2 * time.Hour,
	})
	panel.config.Session = sessionManager
	panel.config.SessionStore = "memory"
	panel.config.SessionRuntime = auth.SessionRuntimeIdentity{
		Pod:      "pod-1",
		Host:     "node-1",
		Instance: "pod-1@node-1",
	}

	root := router.NewMux()
	root.Use(sessionManager.Middleware())
	root.Mount("/admin", panel.Handler())
	srv := httptest.NewServer(root)
	defer srv.Close()

	browserA := newCookieClient(t)
	browserB := newCookieClient(t)

	if _, status := doJSONWithClient(t, browserA, http.MethodGet, srv.URL+"/admin/api/sessions", nil); status != http.StatusOK {
		t.Fatalf("browser A sessions status=%d", status)
	}
	if _, status := doJSONWithClient(t, browserB, http.MethodGet, srv.URL+"/admin/api/sessions", nil); status != http.StatusOK {
		t.Fatalf("browser B sessions status=%d", status)
	}

	resp, status := doJSONWithClient(t, browserA, http.MethodGet, srv.URL+"/admin/api/sessions", nil)
	if status != http.StatusOK {
		t.Fatalf("sessions status: got %d, body=%s", status, mustJSON(resp))
	}

	current, _ := resp["current_active"].(float64)
	if int(current) < 2 {
		t.Fatalf("expected current_active >= 2 across browsers, got %v body=%s", current, mustJSON(resp))
	}
}

func setupPanelForTest(t *testing.T, engine db.Engine) (*Panel, func()) {
	return setupPanelForTestWithAuth(t, engine, nil)
}

func setupPanelForTestWithAuth(t *testing.T, engine db.Engine, adminAuth AdminAuth) (*Panel, func()) {
	t.Helper()

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          engine,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}

	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB failed: %v", err)
	}
	if err := ensureAdminUserSchema(sqlDB); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}

	registry := model.NewRegistry()
	if err := registry.Register(&AdminUser{}); err != nil {
		t.Fatalf("registry.Register failed: %v", err)
	}

	panel := NewPanel(database, registry, logger, PanelConfig{
		Prefix: "/admin",
		Title:  "Test Admin",
		Auth:   adminAuth,
	})

	cleanup := func() {
		_ = database.Close()
	}
	return panel, cleanup
}

func ensureAdminUserSchema(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			email TEXT NOT NULL,
			name TEXT NOT NULL,
			active BOOLEAN NOT NULL DEFAULT 0
		)
	`)
	return err
}

func createAdminUser(t *testing.T, baseURL string, payload map[string]interface{}) AdminUser {
	t.Helper()

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/models/AdminUser", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("create status: got %d, body=%s", res.StatusCode, string(body))
	}

	var created AdminUser
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode created payload failed: %v", err)
	}
	return created
}

func doJSON(t *testing.T, method, url string, payload interface{}) (map[string]interface{}, int) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()

	resp := make(map[string]interface{})
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil && res.StatusCode != http.StatusNoContent {
		t.Fatalf("decode response failed: %v", err)
	}
	return resp, res.StatusCode
}

func doJSONWithClient(t *testing.T, client *http.Client, method, url string, payload interface{}) (map[string]interface{}, int) {
	t.Helper()

	if client == nil {
		client = http.DefaultClient
	}

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()

	resp := make(map[string]interface{})
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil && res.StatusCode != http.StatusNoContent {
		t.Fatalf("decode response failed: %v", err)
	}
	return resp, res.StatusCode
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New failed: %v", err)
	}
	return &http.Client{Jar: jar}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
