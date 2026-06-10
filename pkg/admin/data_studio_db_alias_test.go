package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestHandleListRecords_DBAliasSelectsDatabase is the regression guard for
// fleetdesk finding #10: the admin UI's Data Studio sends `db_alias=<alias>`
// when a database pill is selected, but collectFilters treated it as a
// column filter — every non-default-database listing failed with
// `invalid filter field "db_alias"`. The parameter must (a) be reserved,
// and (b) actually route the query to the selected database handle.
func TestHandleListRecords_DBAliasSelectsDatabase(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	// Second, isolated database that holds one AdminUser row the default DB
	// does not have.
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
	if _, err := otherSQL.Exec(
		`INSERT INTO admin_users (email, name, active, created_at, updated_at) VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"tenant-only@example.com", "Tenant Only"); err != nil {
		t.Fatalf("insert(other): %v", err)
	}

	panel.config.DatabaseHandles = map[string]*db.DB{
		"default":      panel.db,
		"tenant_other": other,
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// 1. The reserved parameter must not be rejected as a filter…
	res, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=tenant_other")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing via db_alias, got %d", res.StatusCode)
	}

	// 2. …and must actually select the aliased database.
	var payload struct {
		Items []struct {
			Email string `json:"email"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Email != "tenant-only@example.com" {
		t.Fatalf("expected the tenant_other row, got %+v", payload.Items)
	}

	// 3. An unknown alias is a clean 400, not a fall-through to default.
	res2, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=ghost")
	if err != nil {
		t.Fatalf("request(ghost): %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown alias, got %d", res2.StatusCode)
	}
}
