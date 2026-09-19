// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
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
// /readyz, these routes expose the process's memory, so they answer only to
// whoever the application's own policy says can see them.
func (a *App) mountPprof() {
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
	a.Logger.Warn("nucleus: the profiler is mounted at " + pprofPrefix +
		" — it exposes heap and goroutine dumps, so keep it behind a policy that names who may read them")
}
