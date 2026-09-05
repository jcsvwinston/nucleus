package notes

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// module holds the framework-managed database handle so the Routes closure
// can capture it directly. The handle is nil until OnStart wires it in via
// rt.DB(); because OnStart is guaranteed to run before Routes,
// eager capture inside Routes is correct and the lazy-accessor workaround is
// no longer needed.
type module struct {
	db *sql.DB
}

// Module returns the nucleus.ModuleSpec for the notes feature.
// It is registered via nucleus.New().Mount(notes.Module()) in main.go.
//
// Lifecycle:
//   - OnStart: receives a nucleus.Runtime and captures rt.DB() into m.db.
//     If no database is configured, OnStart returns an error immediately.
//     There is no OnShutdown: the framework owns the managed connection pool
//     and closes it at shutdown; a module closing it would be a bug.
//   - Routes: runs after OnStart, so it can build the controller eagerly from
//     the already-populated m.db rather than deferring to request time.
//
// Database schema is managed by explicit SQL migrations in
// examples/mvc_api/migrations/; run `nucleus migrate up` before starting
// the server (see README.md for the exact command with flags).
//
// Route registration note: routes are registered with their full paths in
// Routes for readability; Module Prefix + Resource("") also works (the old
// empty-pattern panic was fixed in pkg/nucleus/router.go).
func Module() nucleus.ModuleSpec {
	m := &module{}

	return nucleus.Module[struct{}]{
		Name:   "notes",
		Models: []any{Note{}},

		// The module carries the access it needs, so mounting it is the
		// whole integration: no rbac_policy.csv rows, no csrf_exempt_paths
		// edit in the host. These rows open the whole resource to anonymous
		// callers — a development default for a reference application; an
		// operator deny in the host policy file always overrides them.
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/notes", Action: "read"},
			{Subject: "anonymous", Object: "/notes", Action: "create"},
			{Subject: "anonymous", Object: "/notes/*", Action: "read"},
			{Subject: "anonymous", Object: "/notes/*", Action: "update"},
			{Subject: "anonymous", Object: "/notes/*", Action: "delete"},
		},
		// The JSON API takes cookie-less POST/PUT/DELETE from curl and SDK
		// clients; a header-token API is not CSRF-forgeable.
		CSRFExempt: []string{"/notes"},

		// OnStart wires the framework-managed *sql.DB into the module. The
		// framework opens the connection from databases.default.url in
		// nucleus.yaml, owns its lifecycle, and closes it at shutdown.
		// Modules must NOT open or close the connection themselves.
		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			m.db = rt.DB()
			if m.db == nil {
				return fmt.Errorf("notes: no managed database configured (set databases.default.url in nucleus.yaml)")
			}
			rt.Logger().Info("notes: database connection ready")
			return nil
		},

		// No OnShutdown: the framework owns the managed pool and closes it.
		// A module closing rt.DB() would be a double-close bug.

		Routes: func(r nucleus.Router, _ struct{}) {
			// OnStart has already run, so m.db is non-nil here. Build the
			// controller eagerly — no lazy accessor needed.
			ctl := NewController(m.db)
			r.Resource("/notes", ctl, nucleus.Methods(
				nucleus.Index,
				nucleus.Show,
				nucleus.Create,
				nucleus.Update,
				nucleus.Destroy,
			))
		},
	}.Build()
}
