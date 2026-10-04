// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/internal/routedump"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// ModuleCheck is the verdict of one check CheckModule ran on a module.
type ModuleCheck struct {
	// Name says which check it was — one of the names CheckModule lists.
	Name string
	// Err is nil when the module passed. Otherwise it is the error the
	// boot sequence returns for the same defect, word for word, where
	// boot returns one; for a defect boot lets through silently it says
	// what goes wrong at run time instead.
	Err error
}

// CheckModule runs on one module the checks the boot sequence applies to
// it, one at a time — plus a few boot does not make, for defects a module
// otherwise carries without a word — and returns every verdict, not only
// the first failure. Boot stops at the first error, so a module with three
// defects meets them one boot at a time. pkg/nucleustest wraps this for a
// test.
//
// a is the application the module would be mounted in: its configuration
// (databases included), its options and, when it was built from
// FromConfigFile, its modules.<name> subtrees. a.Modules holds the OTHER
// modules of that application; they take part in the name check only and
// are never started. The checks that need a running framework — templates,
// models, start, migrations, jobs, webhooks, routes, shutdown — run against
// a fresh application container built from a, which CheckModule tears down
// before it returns; they write to a's databases (a test passes a
// throwaway one).
//
// The checks, in the order they are reported:
//   - name: Mount's rules (non-empty, not another module's name), and a
//     name the framework can address everywhere it uses one — lowercase
//     letters, digits and underscores, starting with a letter. The name is
//     the modules.<name> configuration key (a dot splits it), the
//     NUCLEUS_MODULES__<NAME>__ variables (the environment layer lowercases
//     them, so an upper-case name never receives one), the <name>/
//     template namespace and the <webhooks_prefix>/<name> path.
//   - prefix: empty, or a clean absolute path — leading slash, no trailing
//     slash, no wildcard. The router normalises a sloppy prefix when it
//     mounts the routes; the policy rows and the CSRF exemptions resolve
//     against it as written, so with "api" the routes answer at /api and
//     the rows name api/… — a path no request carries.
//   - config: the typed configuration binds (from the application's file,
//     when a has one), takes its default: tags and passes its validate:
//     tags — boot's layer 5, ErrInvalidModuleConfig.
//   - requires: every database Requires names is configured (boot's layer
//     4, ErrInvalidConfigReference), and so is DefaultDB, which boot does
//     not check: an unconfigured DefaultDB hands OnStart a nil database.
//   - policies: every row is well formed (ErrInvalidModulePolicy), loads
//     into the enforcer, and grants something the module serves — its
//     object, resolved against the prefix, matches a route the module
//     registered with a method its action covers. A row that matches
//     nothing loads and never applies.
//   - csrf-exempt: every exemption is well formed and does not switch CSRF
//     off for the whole application (ErrInvalidModulePolicy), and covers a
//     route the module registered.
//   - templates: the module's embedded templates parse into the engine.
//   - models: every model registers.
//   - start: OnStart returns nil.
//   - migrations: the embedded migrations apply on the application's
//     database, through the module-scoped migrator ApplyModuleMigrations
//     uses (a second apply is a no-op, so a module that applies its own in
//     OnStart is checked all the same).
//   - jobs, webhooks: the registrations are valid (ErrInvalidJobSpec,
//     ErrInvalidWebhookSpec).
//   - routes: the routes register under the prefix without the router
//     refusing one — a duplicate, a pattern that conflicts with the
//     framework's own, a Resource verb the controller does not implement.
//   - shutdown: OnShutdown returns nil within the framework's shutdown
//     budget, run where boot runs it: before the framework closes its own
//     resources, with the database still open.
//
// What a policy row or an exemption is about is judged against the
// routes, so it is judged when the routes register; when they do not, the
// routes check says why and those two judge the declarations alone.
//
// A check that could not run is left out, and the check that stopped it
// says why: shutdown is left out when start failed (boot never calls
// OnShutdown on a module whose OnStart failed), and every check that needs
// a running framework is left out, replaced by an "application" check,
// when a itself cannot be built into one. The checks after start run even
// when OnStart failed, so a defect there is not hidden behind it.
func CheckModule(ctx context.Context, a App, spec ModuleSpec) []ModuleCheck {
	if ctx == nil {
		ctx = context.Background()
	}
	if spec == nil {
		return []ModuleCheck{{Name: "name", Err: errors.New("nucleus: CheckModule: the module is nil")}}
	}
	name := spec.Name()
	verdicts := map[string]error{}
	var order []string
	record := func(check string, err error) {
		if _, seen := verdicts[check]; !seen {
			order = append(order, check)
		}
		verdicts[check] = errors.Join(verdicts[check], err)
	}

	// ---- what boot checks before it builds anything ----------------------

	others := make([]ModuleSpec, 0, len(a.Modules)+1)
	for _, other := range sortedModuleSpecs(a.Modules) {
		others = append(others, other)
	}
	record("name", New().Mount(append(others, spec)...).Err())
	record("name", checkModuleName(name))
	record("prefix", checkModulePrefix(name, spec.Prefix()))

	cfg := a.Config
	app.NormalizeRuntimeConfig(&cfg)

	if binder, ok := spec.(moduleConfigBinder); ok {
		bound, err := binder.bindConfig(a.moduleConfigsRaw[name])
		record("config", err)
		if err == nil {
			spec = bound
		}
	} else {
		record("config", nil)
	}

	record("requires", validateModuleRequires(&cfg, map[string]ModuleSpec{name: spec}))
	if alias := spec.DefaultDB(); alias != "" {
		if _, ok := cfg.Databases[alias]; !ok {
			record("requires", fmt.Errorf("%w: module %q DefaultDB %q is not a configured database, so OnStart receives a nil rt.DB()", ErrInvalidConfigReference, name, alias))
		}
	}

	carrier, hasPolicies := spec.(modulePolicyCarrier)
	policiesWellFormed := !hasPolicies || validateModulePolicyRules(spec, carrier) == nil
	if hasPolicies {
		record("policies", validateModulePolicyRules(spec, carrier))
		record("csrf-exempt", validateModuleCSRFDeclarations(spec, carrier))
	} else {
		record("policies", nil)
		record("csrf-exempt", nil)
	}

	// ---- what needs a running framework ------------------------------------

	if err := validateSemantics(&cfg); err != nil {
		record("application", err)
		return moduleChecks(order, verdicts)
	}
	if err := validateReferential(&cfg); err != nil {
		record("application", err)
		return moduleChecks(order, verdicts)
	}
	only := map[string]ModuleSpec{name: spec}
	options := a.Options
	core, err := app.New(&cfg, options...)
	if err != nil {
		record("application", fmt.Errorf("nucleus: app.New: %w", err))
		return moduleChecks(order, verdicts)
	}
	if tplOpts := moduleTemplateOptions(only); len(tplOpts) > 0 {
		// Templates parse inside app.New, so the module's are judged by a
		// second container built with them: the first one, without, is
		// what the remaining checks run on either way.
		withTemplates, terr := app.New(&cfg, append(options[:len(options):len(options)], tplOpts...)...)
		if terr != nil {
			record("templates", fmt.Errorf("nucleus: app.New: %w", terr))
		} else {
			record("templates", nil)
			shutdownQuietly(withTemplates)
		}
	} else {
		record("templates", nil)
	}

	if hasPolicies && policiesWellFormed {
		record("policies", applyModulePolicies(core, []ModuleSpec{spec}))
	}
	record("models", registerModuleModels(core, []ModuleSpec{spec}))

	rt := newModuleRuntime(core, spec)
	rt.tasksRef = &taskManagerRef{}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	startErr := guarded("OnStart", func() error { return spec.OnStart(runCtx, rt) })
	if startErr != nil {
		startErr = fmt.Errorf("nucleus: module %q OnStart: %w", name, startErr)
	}
	record("start", startErr)

	if moduleHasMigrations(spec.Migrations()) {
		record("migrations", guarded("ApplyModuleMigrations", rt.ApplyModuleMigrations))
	} else {
		record("migrations", nil)
	}

	record("jobs", guarded("Jobs", func() error { return newModuleJobs(moduleLogger(core)).collect(spec) }))
	record("webhooks", guarded("Webhooks", func() error { return newModuleWebhooks(moduleLogger(core)).collect(spec) }))

	inv := &routeInventory{}
	routesErr := mountModule(core, spec, inv)
	record("routes", routesErr)
	if routesErr == nil && hasPolicies {
		if policiesWellFormed {
			record("policies", checkPoliciesServe(spec, carrier.policyRules(), inv.routes))
		}
		if validateModuleCSRFDeclarations(spec, carrier) == nil {
			record("csrf-exempt", checkCSRFExemptionsServe(spec, carrier.csrfExemptPaths(), inv.routes))
		}
	}

	// Shutdown, the way boot orders it: the context OnStart received ends
	// first, then the module's hook — registered on the container after the
	// framework's own, and hooks run in reverse — stops the module while
	// its database is still open. A module whose OnStart failed is never
	// stopped: boot never registers its hook.
	shutdownErr := make(chan error, 1)
	if startErr == nil {
		core.OnShutdown(func(ctx context.Context) error {
			err := guarded("OnShutdown", func() error { return spec.OnShutdown(ctx, rt) })
			if err != nil {
				err = fmt.Errorf("nucleus: module %q OnShutdown: %w", name, err)
			}
			shutdownErr <- err
			return nil
		})
	}
	cancel()
	budget := lifecycleShutdownTimeout(core)
	returned := shutdownWithin(core, budget)
	if startErr == nil {
		select {
		case err := <-shutdownErr:
			record("shutdown", err)
		default:
			if returned {
				record("shutdown", fmt.Errorf("nucleus: module %q OnShutdown never ran", name))
			} else {
				record("shutdown", fmt.Errorf("nucleus: module %q OnShutdown did not return within %s, the framework's shutdown budget (write_timeout): boot would give up on it with what the module opened still open", name, budget))
			}
		}
	}
	return moduleChecks(order, verdicts)
}

// moduleChecks lays the verdicts out in the documented order.
func moduleChecks(order []string, verdicts map[string]error) []ModuleCheck {
	rank := map[string]int{}
	for i, n := range []string{"name", "prefix", "config", "requires", "policies", "csrf-exempt", "application",
		"templates", "models", "start", "migrations", "jobs", "webhooks", "routes", "shutdown"} {
		rank[n] = i
	}
	out := make([]ModuleCheck, 0, len(order))
	for _, n := range order {
		out = append(out, ModuleCheck{Name: n, Err: verdicts[n]})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && rank[out[j].Name] < rank[out[j-1].Name]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// moduleNamePattern is a name every place the framework uses one can carry.
var moduleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func checkModuleName(name string) error {
	if name == "" || moduleNamePattern.MatchString(name) {
		return nil // empty is Mount's error, already reported
	}
	return fmt.Errorf("nucleus: module name %q is not lowercase letters, digits and underscores starting with a letter: "+
		"the name is the modules.%s configuration key, the NUCLEUS_MODULES__%s__ variables (the environment layer lowercases them), "+
		"the %s/ template namespace and the webhooks path segment, and a dot, an upper-case letter, a slash or a hyphen breaks one of them",
		name, name, strings.ToUpper(name), name)
}

func checkModulePrefix(module, prefix string) error {
	if prefix == "" {
		return nil
	}
	clean := path.Clean("/" + strings.TrimSpace(prefix))
	switch {
	case !strings.HasPrefix(prefix, "/"):
		return fmt.Errorf("nucleus: module %q Prefix %q does not start with \"/\": the router mounts the routes at %q, while the module's policy rows and CSRF exemptions resolve against %q as written and match no request", module, prefix, clean, prefix)
	case strings.ContainsAny(prefix, "{}*"):
		return fmt.Errorf("nucleus: module %q Prefix %q carries a wildcard: the prefix is stripped literally from the request path, so a pattern segment there matches nothing", module, prefix)
	case prefix != clean:
		return fmt.Errorf("nucleus: module %q Prefix %q is not a clean path (%q): the router mounts the routes at the clean path, while the module's policy rows and CSRF exemptions resolve against the prefix as written", module, prefix, clean)
	}
	return nil
}

// checkPoliciesServe asks, for every row, whether it grants anything the
// module serves: a scratch enforcer with the framework's own RBAC model
// holds that row alone, and each route the module registered is put to it
// as a request — the path with a value in every wildcard, the action the
// authz middleware derives from the method.
func checkPoliciesServe(spec ModuleSpec, rules []PolicyRule, routes []routedump.Route) error {
	var errs []error
	for i, rule := range rules {
		scratch, err := authz.New(slog.New(slog.DiscardHandler))
		if err != nil {
			return err
		}
		objects := resolveModulePolicyObjects(spec.Prefix(), rule.Object)
		for _, obj := range objects {
			if err := scratch.AddPolicy(rule.Subject, obj, rule.Action); err != nil {
				return err
			}
		}
		served := false
		for _, r := range routes {
			action := methodAction(r.Method)
			if r.Method == "*" {
				action = rule.Action
				if action == "*" {
					action = "read"
				}
			}
			if scratch.Can(rule.Subject, samplePath(r.Pattern), action) {
				served = true
				break
			}
		}
		if !served {
			errs = append(errs, fmt.Errorf("%w: module %q Policies[%d] (%s, %s, %s) grants nothing the module serves: its object resolves to %s and no route the module registered matches it with a method %q covers",
				ErrInvalidModulePolicy, spec.Name(), i, rule.Subject, rule.Object, rule.Action, strings.Join(objects, " and "), rule.Action))
		}
	}
	return errors.Join(errs...)
}

// checkCSRFExemptionsServe asks, for every exemption, whether its resolved
// prefix — matched raw, the way the CSRF middleware matches it — covers a
// route the module registered.
func checkCSRFExemptionsServe(spec ModuleSpec, exempt []string, routes []routedump.Route) error {
	var errs []error
	for i, p := range exempt {
		resolved := resolveModuleCSRFPath(spec.Prefix(), p)
		covered := false
		for _, r := range routes {
			if strings.HasPrefix(samplePath(r.Pattern), resolved) {
				covered = true
				break
			}
		}
		if !covered {
			errs = append(errs, fmt.Errorf("%w: module %q CSRFExempt[%d] %q resolves to %q, which covers no route the module registered",
				ErrInvalidModulePolicy, spec.Name(), i, p, resolved))
		}
	}
	return errors.Join(errs...)
}

// methodAction is the verb the default authz middleware asks the enforcer
// for on a request with this method (pkg/app's httpMethodToAction).
func methodAction(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return "read"
	}
}

// samplePath turns a route pattern into a path a request could carry: a
// value in every {wildcard} and in a trailing subtree "*".
func samplePath(pattern string) string {
	segs := strings.Split(pattern, "/")
	for i, s := range segs {
		if (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) || s == "*" {
			segs[i] = "x"
		}
	}
	return strings.Join(segs, "/")
}

// guarded runs one of the module's own functions and turns a panic into
// the error it reports: boot would take the process down with it, and the
// check has more to say after it.
func guarded(what string, fn func() error) (err error) {
	defer func() {
		if rv := recover(); rv != nil {
			err = fmt.Errorf("%s panicked: %v", what, rv)
		}
	}()
	return fn()
}

// shutdownWithin runs the container's shutdown hooks and reports whether
// they returned within the budget (plus a grace for the framework's own).
func shutdownWithin(core *app.App, budget time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = core.Shutdown(ctx)
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(budget + 2*time.Second):
		return false
	}
}

func shutdownQuietly(core *app.App) {
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleShutdownTimeout(core))
	defer cancel()
	_ = core.Shutdown(ctx)
}

// moduleHasMigrations reports whether the module's embedded migrations
// hold at least one entry.
func moduleHasMigrations(fsys fs.FS) bool {
	if fsys == nil {
		return false
	}
	entries, err := fs.ReadDir(fsys, ".")
	return err == nil && len(entries) > 0
}
