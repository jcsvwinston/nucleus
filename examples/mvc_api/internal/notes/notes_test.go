package notes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/notes"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// openTestDB opens an in-memory SQLite database and creates the notes table.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS notes (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		title      TEXT     NOT NULL,
		body       TEXT     NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		deleted_at DATETIME
	)`)
	if err != nil {
		t.Fatalf("create notes table: %v", err)
	}
	return db
}

// adaptNucleusHandler converts a nucleus.Handler to a routerpkg.Handler so it
// can be registered directly on a *routerpkg.Mux in tests.
func adaptNucleusHandler(h nucleus.Handler) routerpkg.Handler {
	return func(c *routerpkg.Context) error {
		return h(&nucleus.Context{Context: c})
	}
}

// buildTestRouter wires the notes controller onto a fresh *routerpkg.Mux and
// returns it as an http.Handler for httptest.
func buildTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	mux := routerpkg.NewMux()
	ctl := notes.NewController(db)

	mux.Get("/notes", adaptNucleusHandler(ctl.Index))
	mux.Get("/notes/{id}", adaptNucleusHandler(ctl.Show))
	mux.Post("/notes", adaptNucleusHandler(ctl.Create))
	mux.Put("/notes/{id}", adaptNucleusHandler(ctl.Update))
	mux.Delete("/notes/{id}", adaptNucleusHandler(ctl.Destroy))

	return mux
}

func TestNotesRoundTrip(t *testing.T) {
	db := openTestDB(t)
	handler := buildTestRouter(t, db)

	// POST — create a note
	body, _ := json.Marshal(map[string]string{"title": "hello", "body": "world"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/notes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /notes: want 201, got %d: %s", rec.Code, rec.Body)
	}

	var created map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created["title"] != "hello" {
		t.Fatalf("title mismatch: want hello, got %v", created["title"])
	}

	// GET /notes — index
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/notes", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notes: want 200, got %d: %s", rec.Code, rec.Body)
	}
	var list map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if count, ok := list["count"].(float64); !ok || count < 1 {
		t.Fatalf("expected at least 1 note in index, got count=%v", list["count"])
	}

	// GET /notes/1 — show
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/notes/1", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notes/1: want 200, got %d: %s", rec.Code, rec.Body)
	}

	// PUT /notes/1 — update
	body, _ = json.Marshal(map[string]string{"title": "updated", "body": "new body"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/notes/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /notes/1: want 200, got %d: %s", rec.Code, rec.Body)
	}

	// DELETE /notes/1 — soft delete
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/notes/1", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /notes/1: want 204, got %d: %s", rec.Code, rec.Body)
	}

	// Index after delete — count must be 0
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/notes", nil)
	handler.ServeHTTP(rec, req)
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if count, ok := list["count"].(float64); ok && count != 0 {
		t.Fatalf("expected 0 notes after soft delete, got %v", count)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	db := openTestDB(t)
	handler := buildTestRouter(t, db)

	body, _ := json.Marshal(map[string]string{"body": "no title"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/notes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rec.Code, rec.Body)
	}
}

func TestShowNotFound(t *testing.T) {
	db := openTestDB(t)
	handler := buildTestRouter(t, db)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/notes/999", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

// TestBadIDsAndNotFound covers the edge cases a reader would naturally ask about:
//   - non-numeric IDs → 400
//   - zero ID → 400 (parseID rejects id < 1)
//   - non-existent resource on PUT → 404
//   - soft-delete idempotency: DELETE same id twice → second returns 404
func TestBadIDsAndNotFound(t *testing.T) {
	db := openTestDB(t)
	handler := buildTestRouter(t, db)

	t.Run("GET /notes/abc returns 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/notes/abc", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("GET /notes/0 returns 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/notes/0", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("PUT /notes/999 returns 404 for non-existent note", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"title": "ghost", "body": ""})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/notes/999", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("DELETE same id twice: second returns 404 (soft-delete idempotency)", func(t *testing.T) {
		// Seed a note to delete.
		body, _ := json.Marshal(map[string]string{"title": "to-delete", "body": ""})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/notes", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed note: want 201, got %d body=%s", rec.Code, rec.Body)
		}
		var created map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatalf("decode created: %v", err)
		}
		// The id is returned as a float64 from JSON decode.
		id := int(created["id"].(float64))

		path := "/notes/" + strconv.Itoa(id)

		// First DELETE → 204.
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, path, nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("first DELETE: want 204, got %d body=%s", rec.Code, rec.Body)
		}

		// Second DELETE → 404: deleted_at IS NOT NULL, so WHERE deleted_at IS NULL matches nothing.
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, path, nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("second DELETE: want 404 (soft-delete idempotency), got %d body=%s", rec.Code, rec.Body)
		}
	})
}
