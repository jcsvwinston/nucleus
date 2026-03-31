package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jcsvwinston/GoFrame/pkg/db"
	"github.com/jcsvwinston/GoFrame/pkg/model"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

type AdminUser struct {
	model.BaseModel
	Email  string `gorm:"not null" json:"email" admin:"list,search"`
	Name   string `gorm:"not null" json:"name" admin:"list,search"`
	Active bool   `gorm:"default:true" json:"active" admin:"list,filter"`
}

func (AdminUser) TableName() string {
	return "admin_users"
}

func TestPanel_CRUDWithDualEngine(t *testing.T) {
	engines := []db.Engine{db.EngineGORM, db.EngineBun}

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
	engines := []db.Engine{db.EngineBun}

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
	panel, cleanup := setupPanelForTest(t, db.EngineBun)
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

func TestPanel_BulkExportSelected(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineBun)
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

func TestPanel_UIAssetsServedUnderPrefix(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineBun)
	defer cleanup()

	root := chi.NewRouter()
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

func setupPanelForTest(t *testing.T, engine db.Engine) (*Panel, func()) {
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

	if engine == db.EngineBun {
		sqlDB, err := database.SqlDB()
		if err != nil {
			t.Fatalf("database.SqlDB failed: %v", err)
		}
		_, err = sqlDB.Exec(`
			CREATE TABLE admin_users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				created_at DATETIME,
				updated_at DATETIME,
				deleted_at DATETIME,
				email TEXT NOT NULL,
				name TEXT NOT NULL,
				active BOOLEAN
			)
		`)
		if err != nil {
			t.Fatalf("create schema failed: %v", err)
		}
	} else {
		if err := database.GormDB().AutoMigrate(&AdminUser{}); err != nil {
			t.Fatalf("automigrate failed: %v", err)
		}
	}

	registry := model.NewRegistry()
	if err := registry.Register(&AdminUser{}); err != nil {
		t.Fatalf("registry.Register failed: %v", err)
	}

	panel := NewPanel(database, registry, logger, PanelConfig{
		Prefix: "/admin",
		Title:  "Test Admin",
	})

	cleanup := func() {
		_ = database.Close()
	}
	return panel, cleanup
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

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
