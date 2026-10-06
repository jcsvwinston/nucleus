// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// pprofPrefix is where the profiler lives when it is turned on.
const pprofPrefix = "/debug/pprof"

// mountPprof registers the profiler when profiling_enabled is set.
//
// It is OFF by default and BEHIND authorization when on, which is the only
// combination that makes sense. net/http/pprof registers itself on
// http.DefaultServeMux at import time, and a framework that imported it
// unguarded would put heap and goroutine dumps — which contain live data — on
// every application's public surface. But an on-call engineer with a process
// burning CPU needs it, and telling them to redeploy with a patched binary is
// telling them to reproduce the problem later.
//
// There is no entry in the bootstrap allow-list on purpose: unlike /healthz and
// /readyz, these routes expose the process's memory, so on the default stack
// they answer only to whoever the application's own policy says can see them.
//
// coreOnly is an application built WithoutDefaults() without WithAuthz(),
// which builds no RBAC enforcer: there is no policy to put the profiler
// behind, and it answers anyone the application's own middleware does not
// refuse (NU-124). It is still served, as it always was (QADR-0010), and the
// boot log says so in one ERROR line instead of a WARN advising a policy the
// application cannot have; from v2.0.0 that configuration refuses to start
// unless WithAuthz() guards the profiler (DEP-2026-018). With WithAuthz() the
// profiler sits behind the default-deny gate, as on the default stack.
func (a *App) mountPprof(coreOnly bool) {
	if a == nil || a.Config == nil || !a.Config.ProfilingEnabled {
		return
	}
	handlers := map[string]http.HandlerFunc{
		"/":        pprof.Index,
		"/cmdline": pprof.Cmdline,
		"/profile": pprof.Profile,
		"/symbol":  pprof.Symbol,
		"/trace":   pprof.Trace,
	}
	for suffix, handler := range handlers {
		h := handler
		a.Router.Get(pprofPrefix+suffix, func(c *router.Context) error {
			h(c.Writer, c.Request)
			return nil
		})
	}
	// The named profiles (heap, goroutine, allocs, block, mutex, threadcreate)
	// all come from the same handler, which reads the name off the path.
	a.Router.Get(pprofPrefix+"/{profile}", func(c *router.Context) error {
		name := strings.TrimSpace(c.Param("profile"))
		pprof.Handler(name).ServeHTTP(c.Writer, c.Request)
		return nil
	})
	if coreOnly {
		logPprofUnguarded(a.Logger, a.Config)
		return
	}
	a.Logger.Warn("nucleus: the profiler is mounted at " + pprofPrefix +
		" — it exposes heap and goroutine dumps, so keep it behind a policy that names who may read them")
}

// logPprofUnguarded is the NU-124 warning: one structured ERROR line naming
// the profiler an application built WithoutDefaults() serves with no policy
// in front of it, what to do about it, and that the configuration stops
// booting at the major. In production it says in so many words what a heap
// dump of the process carries.
func logPprofUnguarded(logger *slog.Logger, cfg *Config) {
	msg := "profiler UNGUARDED: profiling_enabled mounts " + pprofPrefix + " and this application is built " +
		"WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so no policy can say who may read it — " +
		"heap and goroutine dumps answer anyone who reaches the port, unless the application's own middleware refuses them"
	if cfg.IsProd() {
		msg = "profiler UNGUARDED in production: profiling_enabled mounts " + pprofPrefix + " and this application is " +
			"built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so no policy can say who may " +
			"read it — anyone who reaches the port can download heap dumps of this production process, with the live " +
			"memory they carry (session tokens, credentials, request payloads), unless the application's own middleware " +
			"refuses them"
	}
	logger.Error(msg,
		"prefix", pprofPrefix,
		"env", strings.TrimSpace(cfg.Env),
		"fix", "add WithAuthz() beside WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithAuthz(), "+
			"or app.New(cfg, app.WithoutDefaults(), app.WithAuthz()) — and grant "+pprofPrefix+"/* to an on-call role in the "+
			"policy, never to anonymous; or set profiling_enabled: false, and serve net/http/pprof from a listener only "+
			"operators reach when you need a profile",
		"deprecation", depPprofUnguarded+": from v2.0.0 this configuration refuses to start unless WithAuthz() guards the profiler")
}

// depPprofUnguarded is the deprecation notice for an application built
// WithoutDefaults() without WithAuthz() that turns the profiler on: today it
// is served with an ERROR line at boot; from v2.0.0 the application refuses
// to start unless WithAuthz() guards it (docs/deprecations/DEP-2026-018-*.md).
const depPprofUnguarded = "DEP-2026-018"
