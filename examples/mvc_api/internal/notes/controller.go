package notes

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// NewController creates a Controller backed by the given database connection.
// The caller is responsible for the lifetime of db.
// This is the preferred constructor for tests, which wire the DB directly.
func NewController(db *sql.DB) *Controller {
	return &Controller{dbFn: func() *sql.DB { return db }}
}

// newLazyController creates a Controller that resolves its database handle
// by calling dbFn at request time rather than at construction time.
// This is used by Module.Routes so that the handle populated by OnStart
// (which runs AFTER Routes during the nucleus startup sequence) is always
// observed before any real HTTP request reaches the handler.
func newLazyController(dbFn func() *sql.DB) *Controller {
	return &Controller{dbFn: dbFn}
}

// Controller implements the Nucleus REST Resource sub-interfaces for the
// five CRUD verbs: Index, Show, Create, Update, Destroy.
//
// It satisfies:
//
//	nucleus.Indexer   — GET  /notes
//	nucleus.Shower    — GET  /notes/{id}
//	nucleus.Creator   — POST /notes
//	nucleus.Updater   — PUT  /notes/{id}
//	nucleus.Destroyer — DELETE /notes/{id}
//
// The database handle is resolved lazily via dbFn so that the controller can
// be registered during Routes (which runs before OnStart in the nucleus
// lifecycle) without capturing a nil *sql.DB.
type Controller struct {
	dbFn func() *sql.DB
}

// db returns the live database handle. It panics if dbFn is nil (a
// construction-time bug). If the module has not yet completed OnStart the
// closure returns a nil *sql.DB; calling QueryContext/ExecContext on a nil
// *sql.DB will itself panic, so callers should ensure requests cannot reach
// handlers before OnStart completes (nucleus guarantees this in normal
// operation).
func (ctl *Controller) db() *sql.DB {
	if ctl.dbFn == nil {
		panic("notes.Controller: dbFn is nil — use NewController or newLazyController")
	}
	return ctl.dbFn()
}

// createInput is the request body for POST /notes.
type createInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// updateInput is the request body for PUT /notes/{id}.
type updateInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Index handles GET /notes — returns all non-deleted notes ordered by id desc.
func (ctl *Controller) Index(c *nucleus.Context) error {
	rows, err := ctl.db().QueryContext(c.Request.Context(),
		`SELECT id, title, body, created_at, updated_at FROM notes WHERE deleted_at IS NULL ORDER BY id DESC`)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to list notes", err)
	}
	defer rows.Close()

	notes := make([]noteRow, 0, 16)
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return respondError(c, http.StatusInternalServerError, "failed to scan note", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return respondError(c, http.StatusInternalServerError, "row iteration error", err)
	}

	return c.JSON(http.StatusOK, map[string]any{"notes": notes, "count": len(notes)})
}

// Show handles GET /notes/{id} — returns a single note by id.
func (ctl *Controller) Show(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}

	var n noteRow
	err = ctl.db().QueryRowContext(c.Request.Context(),
		`SELECT id, title, body, created_at, updated_at FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&n.ID, &n.Title, &n.Body, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to fetch note", err)
	}

	return c.JSON(http.StatusOK, n)
}

// Create handles POST /notes — creates a new note and returns 201 with the created row.
func (ctl *Controller) Create(c *nucleus.Context) error {
	var input createInput
	if err := c.BindJSON(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if input.Title == "" {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "title is required"})
	}

	now := time.Now().UTC()
	res, err := ctl.db().ExecContext(c.Request.Context(),
		`INSERT INTO notes (title, body, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		input.Title, input.Body, now, now,
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to create note", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to retrieve new note id", err)
	}

	return c.JSON(http.StatusCreated, noteRow{
		ID:        uint(lastID),
		Title:     input.Title,
		Body:      input.Body,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// Update handles PUT /notes/{id} — replaces a note's title and body.
func (ctl *Controller) Update(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}

	var input updateInput
	if err := c.BindJSON(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if input.Title == "" {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "title is required"})
	}

	now := time.Now().UTC()
	res, err := ctl.db().ExecContext(c.Request.Context(),
		`UPDATE notes SET title = ?, body = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		input.Title, input.Body, now, id,
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to update note", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to check rows affected", err)
	}
	if n == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}

	// Re-fetch the row so the response shape is consistent with Show
	// (includes created_at which is not available from the UPDATE statement).
	var updated noteRow
	err = ctl.db().QueryRowContext(c.Request.Context(),
		`SELECT id, title, body, created_at, updated_at FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&updated.ID, &updated.Title, &updated.Body, &updated.CreatedAt, &updated.UpdatedAt)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to fetch updated note", err)
	}
	return c.JSON(http.StatusOK, updated)
}

// Destroy handles DELETE /notes/{id} — soft-deletes a note.
func (ctl *Controller) Destroy(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}

	now := time.Now().UTC()
	res, err := ctl.db().ExecContext(c.Request.Context(),
		`UPDATE notes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now, id,
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to delete note", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to check rows affected", err)
	}
	if n == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}

	return c.NoContent()
}

// parseID extracts the {id} URL parameter and returns a positive integer.
func parseID(c *nucleus.Context) (int64, error) {
	raw := c.Param("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

// respondError logs the underlying error server-side and returns an opaque
// JSON response to the client. Teaching note: always log internal errors —
// never silently swallow them. The client receives only a generic message so
// implementation details are not leaked.
func respondError(c *nucleus.Context, code int, msg string, err error) error {
	slog.ErrorContext(c.Request.Context(), msg, "err", err, "status", code)
	return c.JSON(code, map[string]string{"error": msg})
}
