package app

import (
	"context"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"html/template"
	"io/fs"
)

// Extension is the interface that subsystems implement to register themselves
// with the App container during initialization. Extensions are attached after
// the core components (config, logger, router, DB, sessions, models) are
// initialized but before the HTTP server starts.
//
// The lifecycle is:
//  1. app.New(cfg) initializes core components.
//  2. Each Extension.Attach(a) is called in registration order.
//  3. app.Run(ctx) starts the HTTP server.
//  4. On shutdown, Extension.Shutdown(ctx) is called in reverse order.
type Extension interface {
	// Name returns a human-readable identifier for this extension (e.g. "admin", "storage").
	Name() string

	// Attach initializes the extension and wires it into the App.
	//
	// It receives the fully constructed core App and may:
	//   - READ the framework services it needs (Config, Logger, Router, DB,
	//     DBs, Models, Session, JWT, Authorizer, Mailer, Storage,
	//     Observability);
	//   - mount HTTP routes and register middleware through Router;
	//   - hold on to whatever it needs for its own lifetime.
	//
	// It may NOT reassign the framework's own fields on App. That used to
	// be documented as permitted — "or set fields on App" — and it was a
	// blank cheque: whatever an extension reached for became API in
	// practice while being covered by no contract, so nothing could be
	// promised about it from one version to the next. No extension ever
	// used it (the one in-tree consumer reads five fields and writes
	// none), so the permission cost stability and bought nothing.
	//
	// What an extension may rely on is frozen in
	// contracts/baseline/extension_surface.txt: adding or removing a field
	// there is a deliberate, reviewed act, which is what makes it possible
	// to promise anything to a plugin author at all.
	Attach(a *App) error

	// Shutdown releases resources held by this extension.
	// Called in reverse registration order during App.Shutdown.
	Shutdown(ctx context.Context) error
}

// Option configures the App during construction via app.New(cfg, opts...).
type Option func(*appOptions)

// appOptions holds the optional configuration for App construction.
type appOptions struct {
	extensions   []Extension
	skipDefaults bool
	withStorage  bool
	openAuthz    bool
	// problemDetails makes RFC 9457 problem details the shape of every
	// error (WithProblemDetails).
	problemDetails bool
	templateFuncs  template.FuncMap
	templateBase   *template.Template
	templateFS     []templateFSSource
	// userProvider backs the "local" authentication backend when set.
	userProvider     auth.UserProvider
	userProviderName string
	// apiKeys turns on API-key authentication (WithAPIKeys).
	apiKeys bool
	// withMail builds the mail sender on an application built
	// WithoutDefaults (WithMail).
	withMail bool
	// realtime gives the application a hub and its channel route
	// (WithRealtime).
	realtime bool
}

// templateFSSource is one WithTemplatesFS registration: an fs.FS whose
// .html files parse into the engine under the given name prefix.
type templateFSSource struct {
	prefix string
	fsys   fs.FS
}

// WithExtensions registers one or more extensions to be attached during app.New().
//
// Example:
//
//	a, err := app.New(cfg,
//	    app.WithExtensions(
//	        admin.Extension(),
//	        storage.Extension(storageCfg),
//	    ),
//	)
//
// WithUserProvider registers the application's own user table as an
// authentication backend, under the name "local" unless overridden.
//
// It is what finally connects auth.UserProvider — the interface that has
// described how to reach your users since v0.x, frozen in the contract and
// called by nothing — to the login path. An application that authenticates
// only against a directory simply does not call this.
func WithUserProvider(provider auth.UserProvider) Option {
	return func(o *appOptions) { o.userProvider = provider }
}

// WithUserProviderNamed is WithUserProvider with an explicit backend name,
// for an application that wants its table to appear in the chain as
// something other than "local".
func WithUserProviderNamed(name string, provider auth.UserProvider) Option {
	return func(o *appOptions) {
		o.userProvider = provider
		o.userProviderName = name
	}
}

func WithExtensions(exts ...Extension) Option {
	return func(o *appOptions) {
		o.extensions = append(o.extensions, exts...)
	}
}

// WithoutDefaults disables automatic initialization of the default extensions
// (admin, storage, mail, authz). When used, only the core components are
// initialized and the caller must explicitly register desired extensions
// via WithExtensions — or, for the framework's own storage and mail
// subsystems, WithStorage and WithMail.
//
// This is useful for lightweight API services that don't need the admin panel,
// file storage, or RBAC enforcement.
func WithoutDefaults() Option {
	return func(o *appOptions) {
		o.skipDefaults = true
	}
}

// WithStorage builds the storage subsystem on an application built
// WithoutDefaults(): the provider the configuration's storage block selects,
// with the same tenant scoping, cleaner, public routes and shutdown the
// default path gives it.
//
// It builds storage when the configuration WRITES storage — a storage.* key
// in a configuration file or a NUCLEUS_STORAGE__* variable, which the
// loaders record as Config.StorageDeclared — and nothing otherwise.
// WithoutDefaults() turns off the local-filesystem default along with the
// rest, so an application that carries this option and writes no storage
// block stays exactly as light as one that does not: no store, no storage/
// directory. That is why the api starter carries it — `nucleus add s3` and a
// storage block are all it then takes. A Config built in Go asks for
// storage by setting StorageDeclared.
//
// Without it, an application built WithoutDefaults() whose configuration
// writes storage still starts with the block ignored, as it always did, but
// no longer without a word: the boot log carries one ERROR line naming this
// option (NU-99). From v2.0.0 that configuration refuses to start
// (DEP-2026-013).
//
// On an application built with the defaults it changes nothing: storage is
// one of them.
func WithStorage() Option {
	return func(o *appOptions) {
		o.withStorage = true
	}
}

// WithOpenAuthz disables the default-deny RBAC middleware mounted by
// App.New (see ADR-004). It switches off authorization ONLY:
// authentication still runs — a configured JWT manager decodes bearers
// in open mode too, so handlers and request interceptors keep seeing
// the caller's identity via auth.ClaimsFromContext (QCD-FW-25). Use
// only for early development, internal tooling, or demos where
// unauthenticated access is acceptable. The option emits a startup
// WARN log so the choice is visible in operational telemetry. There is
// no `Config.OpenAuthz` config key on purpose — opting out requires
// touching code and surfaces in PR review.
func WithOpenAuthz() Option {
	return func(o *appOptions) {
		o.openAuthz = true
	}
}

// WithProblemDetails makes RFC 9457 problem details
// (application/problem+json) the shape of every error the application
// answers — handler errors, binding failures, the router's own 404 and
// 405, timeouts, the CSRF middleware's and the rate limiter's refusals, and
// the ones written through errors.WriteError (the authorizer's 403, the
// bearer middleware's 401). Without it the
// framework's envelope, {"error": {"code", "message", "details"}}, stays
// the default, and a client gets problem details only when its Accept
// header prefers application/problem+json.
//
// There is no config key for it on purpose, as for WithOpenAuthz: the
// shape of an API's errors is part of its contract with its clients, a
// decision that belongs in code and in review rather than in a deployment
// file.
func WithProblemDetails() Option {
	return func(o *appOptions) {
		o.problemDetails = true
	}
}

// WithTemplateFuncs registers template functions available to every template
// app.New parses from templates_dir (QCD-FW-9). Order of operations at
// startup: registered functions → recursive parse of templates_dir →
// SetHTMLTemplates on the router. Without this, every piece of presentation
// logic (date formats, percentages, pagination URLs) has to be precomputed
// in Go and passed through the data map. Repeated options merge; a later
// registration of the same name wins.
//
//	a, err := app.New(cfg, app.WithTemplateFuncs(template.FuncMap{
//	    "fecha": func(t time.Time) string { return t.Format("02/01/2006") },
//	}))
func WithTemplateFuncs(funcs template.FuncMap) Option {
	return func(o *appOptions) {
		if o.templateFuncs == nil {
			o.templateFuncs = template.FuncMap{}
		}
		for name, fn := range funcs {
			o.templateFuncs[name] = fn
		}
	}
}

// WithTemplates injects a prebuilt *template.Template as the BASE the
// startup loader parses templates_dir into (QCD-FW-9): templates and
// {{define}} blocks already present on the base stay available, and files
// from templates_dir are added on top under their relative-path names —
// enabling wrapping layouts and programmatic templates. Functions from
// WithTemplateFuncs are applied to the base before parsing. When
// templates_dir has no templates, the base itself (if it has any parsed
// templates) is still wired into the router.
func WithTemplates(base *template.Template) Option {
	return func(o *appOptions) {
		o.templateBase = base
	}
}

// WithTemplatesFS parses every `.html` file of an fs.FS into the template
// namespace at startup, each registered under `<prefix>/<path>` (or its
// bare slash-path when prefix is empty) — the fs.FS counterpart of
// templates_dir, for templates embedded in the binary (`embed.FS`).
//
// Unlike WithTemplates (which replaces the base), WithTemplatesFS
// ACCUMULATES: each call adds a source, applied in registration order.
// Load order on a name collision is last-parse-wins: the WithTemplates
// base first, then every FS source, then templates_dir — so the host's
// on-disk files always override an embedded source's. Functions from
// WithTemplateFuncs are applied before any parse, so FS templates see
// them. `nucleus.Run` feeds each mounted module's `Module.Templates`
// through this option under the module's name.
func WithTemplatesFS(prefix string, fsys fs.FS) Option {
	return func(o *appOptions) {
		o.templateFS = append(o.templateFS, templateFSSource{prefix: prefix, fsys: fsys})
	}
}
