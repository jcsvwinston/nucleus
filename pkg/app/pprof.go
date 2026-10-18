// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
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
// behind, and it would answer anyone the application's own middleware does
// not refuse (NU-124). Such an application refuses to start with the profiler
// on — the error names the stake and the ways out — unless WithAuthz() guards
// it (DEP-2026-018). With WithAuthz() the profiler sits behind the
// default-deny gate, as on the default stack.
func (a *App) mountPprof(coreOnly bool) error {
	if a == nil || a.Config == nil || !a.Config.ProfilingEnabled {
		return nil
	}
	if coreOnly {
		return pprofUnguardedError(a.Config)
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
	a.Logger.Warn("nucleus: the profiler is mounted at " + pprofPrefix +
		" — it exposes heap and goroutine dumps, so keep it behind a policy that names who may read them")
	return nil
}

// pprofUnguardedError is the NU-124 refusal: the error New returns when an
// application built WithoutDefaults() without WithAuthz() turns the profiler
// on, naming the profiler no policy could guard, what to do about it, and
// the notice. In production it says in so many words what a heap dump of the
// process carries. The headline is the v1.x log line's, so the detection
// rule the migration assistant documents still finds it.
func pprofUnguardedError(cfg *Config) error {
	msg := "profiler UNGUARDED: profiling_enabled mounts " + pprofPrefix + " and this application is built " +
		"WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so no policy can say who may read it — " +
		"heap and goroutine dumps would answer anyone who reaches the port, unless the application's own middleware refused them"
	if cfg.IsProd() {
		msg = "profiler UNGUARDED in production: profiling_enabled mounts " + pprofPrefix + " and this application is " +
			"built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so no policy can say who may " +
			"read it — anyone who reaches the port could download heap dumps of this production process, with the live " +
			"memory they carry (session tokens, credentials, request payloads), unless the application's own middleware " +
			"refused them"
	}
	return fmt.Errorf("%s (prefix=%s, env=%s) — add WithAuthz() beside WithoutDefaults() — "+
		"nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithAuthz(), or app.New(cfg, app.WithoutDefaults(), "+
		"app.WithAuthz()) — and grant %s/* to an on-call role in the policy, never to anonymous; or set "+
		"profiling_enabled: false, and serve net/http/pprof from a listener only operators reach when you need a profile "+
		"(%s: refused since v2.0.0 unless WithAuthz() guards the profiler)",
		msg, pprofPrefix, strings.TrimSpace(cfg.Env), pprofPrefix, depPprofUnguarded)
}

// depPprofUnguarded is the deprecation notice for an application built
// WithoutDefaults() without WithAuthz() that turns the profiler on: the
// combination was reported with an ERROR line on the v1.x line and refuses
// to start since v2.0.0 unless WithAuthz() guards the profiler
// (docs/deprecations/DEP-2026-018-*.md).
const depPprofUnguarded = "DEP-2026-018"
