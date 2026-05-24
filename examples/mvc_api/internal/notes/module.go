package notes

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	// modernc pure-Go SQLite driver — no CGo required.
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// module holds the live database handle so the Routes closure can
// close over it. The handle is nil until OnStart wires it in.
type module struct {
	db *sql.DB
}

// Module returns the nucleus.ModuleSpec for the notes feature.
// It is registered via nucleus.New().Mount(notes.Module()) in main.go.
//
// Lifecycle:
//   - OnStart: opens the SQLite connection from the app config URL.
//   - Routes:  registers GET/POST /notes and GET/PUT/DELETE /notes/{id}.
//   - OnShutdown: closes the database connection.
//
// Database schema is managed by explicit SQL migrations in
// examples/mvc_api/migrations/; run `nucleus migrate up` before starting
// the server (see README.md for the exact command with flags).
//
// Route registration note: the module does NOT set a Prefix — routes are
// registered with their full paths directly in Routes. This avoids the
// framework footgun where Resource("") called inside a prefix-scoped
// sub-router produces the invalid pattern "GET " (empty host/path) and
// panics net/http.ServeMux at startup.
func Module() nucleus.ModuleSpec {
	m := &module{}

	return nucleus.Module[struct{}]{
		Name:   "notes",
		Models: []any{Note{}},

		OnStart: func(ctx context.Context, a *nucleus.App, _ struct{}) error {
			dbURL := resolveDBURL(a)
			if dbURL == "" {
				return fmt.Errorf("notes: no database URL configured (set databases.default.url in nucleus.yaml)")
			}

			db, err := openSQLite(dbURL)
			if err != nil {
				return fmt.Errorf("notes: failed to open database: %w", err)
			}
			m.db = db
			slog.Info("notes: database connection ready", "url", sanitizeURL(dbURL))
			return nil
		},

		Routes: func(r nucleus.Router, _ struct{}) {
			// newLazyController defers the m.db dereference to request time.
			// Routes runs BEFORE OnStart in the nucleus lifecycle (nucleus.go
			// mountModule → OnStart), so capturing m.db by value here would
			// always snapshot nil. The lazy accessor reads m.db after OnStart
			// has populated it, which is guaranteed before any request arrives.
			ctl := newLazyController(func() *sql.DB { return m.db })
			r.Resource("/notes", ctl, nucleus.Methods(
				nucleus.Index,
				nucleus.Show,
				nucleus.Create,
				nucleus.Update,
				nucleus.Destroy,
			))
		},

		OnShutdown: func(ctx context.Context, a *nucleus.App, _ struct{}) error {
			if m.db != nil {
				return m.db.Close()
			}
			return nil
		},
	}.Build()
}

// resolveDBURL returns the URL for the default database alias from the
// app config. The nucleus.App embeds app.Config, so we reach the
// Databases map directly.
func resolveDBURL(a *nucleus.App) string {
	if dbcfg, ok := a.Config.Databases["default"]; ok {
		return dbcfg.URL
	}
	return ""
}

// openSQLite opens a *sql.DB for the given sqlite:// URL by stripping
// the scheme and opening the resulting file path. The modernc driver is
// registered under the "sqlite" driver name.
func openSQLite(rawURL string) (*sql.DB, error) {
	// Strip "sqlite://" prefix — modernc driver expects a plain file path
	// or ":memory:".
	path := rawURL
	path = strings.TrimPrefix(path, "sqlite://")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// sanitizeURL removes any credential portion from a URL for safe logging.
// For SQLite file paths (no "://") this is a no-op.
//
// Guard: if the string contains "@" but no "://" (malformed or a plain file
// path that happens to contain "@"), the function returns the input unchanged
// rather than panicking on negative slice indices.
func sanitizeURL(u string) string {
	atIdx := strings.Index(u, "@")
	if atIdx < 0 {
		return u
	}
	schemeEnd := strings.Index(u, "://")
	if schemeEnd < 0 {
		// No scheme found — cannot safely extract credentials; return as-is.
		return u
	}
	scheme := u[:schemeEnd+3]
	rest := u[atIdx+1:]
	return scheme + "***@" + rest
}
