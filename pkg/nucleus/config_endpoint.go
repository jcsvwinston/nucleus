// Package nucleus — config_endpoint.go implements the auth-gated
// GET /_/config runtime endpoint (ADR-010 Phase 3b, compliance #12). It is
// the runtime counterpart to `nucleus config print --effective` (Phase 3a):
// it returns the effective merged configuration with per-key provenance and
// the same canonical redaction applied.
//
// Gating (three layers, defence in depth):
//
//  1. Mount gate — the endpoint is registered only when the admin subsystem
//     is active (`core.Admin != nil`). A WithoutDefaults() app never exposes
//     it. (ADR-010 §5/#12 assume a `WithAdmin()` toggle that does not exist;
//     admin mounts as a default subsystem gated by WithoutDefaults().)
//  2. Casbin default-deny — the app-wide ADR-004 enforcer wraps the whole
//     router. It only understands JWT claims, so a session-authenticated
//     admin resolves to the `anonymous` subject and would be denied. We add a
//     bootstrap-subject allow policy for the fixed `/_/config` path so the
//     request reaches the real gate — the same precedent the framework uses
//     for `/admin/*` in `authz.BootstrapAllowList`. The exemption is added
//     here, in the nucleus layer that owns the route, rather than by editing
//     the stable `pkg/authz` package.
//  3. Admin-session auth — `admin.NewDatabaseAdminAuth` validates the same
//     server-side admin session that guards `pkg/admin` routes. No session →
//     403 Forbidden.
package nucleus

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/jcsvwinston/nucleus/pkg/admin"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// configEndpointPath is the fixed runtime path of the effective-config
// endpoint. It is deliberately outside the configurable admin prefix (under
// the framework-reserved `/_/` namespace) so its authz exemption and route
// registration are stable regardless of `admin_prefix`.
const configEndpointPath = "/_/config"

// sourceKindRuntime is the ConfigSource.Kind used when the effective snapshot
// is flattened from the live `app.Config` rather than merged from files — the
// direct-struct `Run(App{})` path and builders that never call
// FromConfigFile. There is no file to attribute the value to, so the source
// is the running process. File-backed snapshots use the Phase 3a kinds
// ("default", "yaml", "toml", "json").
const sourceKindRuntime = "runtime"

// adminAuthenticator is the minimal slice of the admin auth provider the
// endpoint gate needs: resolve the admin user from the request's session.
// `*admin.DatabaseAdminAuth` satisfies it. Declaring the interface here keeps
// the gate testable with a stub and documents the exact dependency.
type adminAuthenticator interface {
	Authenticate(r *http.Request) (*auth.User, error)
}

// effectiveSnapshot resolves the EffectiveConfig the endpoint serves. The
// builder path stores a redacted, file-merged snapshot on the App at
// FromConfigFile time (full provenance). When that is absent — direct-struct
// Run(App{}) or a builder that never loaded a file — it falls back to a
// snapshot flattened from the live merged config with `runtime` provenance,
// so the endpoint still answers "what configuration is in effect?".
func (a *App) effectiveSnapshot(core *app.App) EffectiveConfig {
	if a.effective != nil {
		return *a.effective
	}
	if core != nil && core.Config != nil {
		return effectiveFromConfig(*core.Config, core.Config.LogRedactExtraKeys)
	}
	return effectiveFromConfig(a.Config, a.Config.LogRedactExtraKeys)
}

// mountConfigEndpoint registers GET /_/config on the application router when
// the admin subsystem is active. It is a no-op otherwise, so opting out of the
// admin panel (WithoutDefaults) also opts out of the endpoint.
func mountConfigEndpoint(core *app.App, snapshot EffectiveConfig) {
	if core == nil || core.Router == nil || core.Admin == nil {
		return
	}

	// core.Admin != nil means the admin panel started, but it does not by
	// itself guarantee a usable *sql.DB. If DefaultDB() is nil the endpoint
	// still mounts (fail-closed) but every authentication attempt returns
	// 403; log so that misleading-403 is diagnosable rather than silent.
	db := core.DefaultDB()
	if db == nil {
		core.Logger.Warn("nucleus: /_/config mounted but the default database is unavailable; admin authentication will fail",
			"path", configEndpointPath)
	}
	provider := admin.NewDatabaseAdminAuth(db, core.Session, core.Config.AdminPrefix)

	// Layer 2: exempt the fixed path from the app-wide default-deny so the
	// request reaches the admin-session gate (layer 3). Same precedent as
	// `/admin/*`. The enforcer mounted by app.New is the same instance as
	// core.Authorizer, so the policy takes effect for the live middleware.
	// When the enforcer is absent (WithOpenAuthz, or no default subsystems —
	// unreachable here since Admin != nil implies defaults ran) there is no
	// default-deny to exempt and the policy add is simply skipped.
	if core.Authorizer != nil {
		if err := core.Authorizer.AddPolicy(authz.BootstrapSubject, configEndpointPath, "*"); err != nil {
			core.Logger.Error("nucleus: failed to register /_/config authz exemption; endpoint will be unreachable behind default-deny",
				"path", configEndpointPath, "error", err)
			return
		}
	}

	core.Router.
		With(configEndpointAuthMiddleware(provider)).
		Get(configEndpointPath, routerpkg.FromHTTP(configEndpointHandler(snapshot)))
}

// configEndpointAuthMiddleware gates the endpoint on a valid admin session.
// Absent or invalid session → 403 Forbidden (JSON, via the framework error
// writer). The surface is "you (anonymous) are not permitted", not "no
// credentials supplied", matching the default-deny middleware's 403 choice.
func configEndpointAuthMiddleware(provider adminAuthenticator) routerpkg.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := provider.Authenticate(r)
			if err != nil || user == nil {
				gferrors.WriteError(w, r, gferrors.Forbidden("admin authentication required"), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// configEndpointHandler serves the redacted effective-config snapshot as JSON.
// The response is marked no-store: it reflects live configuration and must not
// be cached by intermediaries even though secrets are already redacted.
func configEndpointHandler(snapshot EffectiveConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

// effectiveFromConfig flattens a live app.Config into an EffectiveConfig with
// `runtime` provenance and the canonical redaction applied. It mirrors the
// flatten-and-redact tail of loadEffective but attributes every key to the
// running process rather than a file, for the surfaces that have no file
// provenance to report.
func effectiveFromConfig(cfg app.Config, extraKeys []string) EffectiveConfig {
	k := koanf.New(".")
	// structs.Provider over the "koanf" tag is the same flattening the
	// loader uses to seed defaults (see loadMerged), so the key set matches
	// `config print --effective`. structs.Provider.Read never returns an
	// error, so the discard is intentional and unreachable.
	_ = k.Load(structs.Provider(cfg, "koanf"), nil)

	all := k.All()
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	redactSet := redactionSet(extraKeys)
	values := make([]EffectiveValue, 0, len(keys))
	for _, key := range keys {
		val := all[key]
		redacted := false
		if shouldRedactKey(key, redactSet) {
			val = observe.RedactionPlaceholder
			redacted = true
		}
		values = append(values, EffectiveValue{
			Key:      key,
			Value:    val,
			Source:   ConfigSource{Kind: sourceKindRuntime},
			Redacted: redacted,
		})
	}
	return EffectiveConfig{Values: values}
}
