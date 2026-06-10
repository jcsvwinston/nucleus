package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestHandleListModels_DatabasesReflectPresence is the regression guard for
// fleetdesk finding #11: model→database attribution used only the DECLARED
// alias, so tenant-isolated topologies (same schema replicated across tenant
// databases, declared alias empty/default) reported every model as living in
// "default" and the Data Studio sidebar's database view collapsed to a
// single group. Attribution must reflect probed table PRESENCE — in fast
// (no-counts) mode too.
func TestHandleListModels_DatabasesReflectPresence(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	// A second database that ALSO holds the admin_users table (the
	// tenant-replicated-schema shape).
	logger := observe.NewLogger("error", "text")
	other, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New(other): %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	otherSQL, err := other.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB(other): %v", err)
	}
	if err := ensureAdminUserSchema(otherSQL); err != nil {
		t.Fatalf("schema(other): %v", err)
	}

	panel.config.DatabaseHandles = map[string]*db.DB{
		"default":      panel.db,
		"tenant_other": other,
	}
	panel.config.Databases = []DatabaseRuntimeInfo{
		{Alias: "default", Engine: "sql", Dialect: "sqlite", IsDefault: true},
		{Alias: "tenant_other", Engine: "sql", Dialect: "sqlite"},
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// FAST mode (no include_counts): presence must still be probed.
	res, err := http.Get(srv.URL + "/api/models")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name      string   `json:"name"`
			Databases []string `json:"databases"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var got []string
	for _, m := range payload.Models {
		if m.Name == "AdminUser" {
			got = m.Databases
		}
	}
	if len(got) != 2 || got[0] != "default" || got[1] != "tenant_other" {
		t.Fatalf("AdminUser databases = %v, want [default tenant_other] (probed presence on both handles)", got)
	}
}
