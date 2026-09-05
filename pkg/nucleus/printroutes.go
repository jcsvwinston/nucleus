// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/jcsvwinston/nucleus/internal/routedump"
	"github.com/jcsvwinston/nucleus/pkg/app"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// printRoutesOut is where the NUCLEUS_PRINT_ROUTES document goes. A
// variable, not os.Stdout inline, so the in-process test can capture the
// line without redirecting the process's file descriptor.
var printRoutesOut io.Writer = os.Stdout

// routeInventory accumulates, during RunContext, the route table of THIS
// binary with each entry attributed to the module that registered it. It
// is the source of the NUCLEUS_PRINT_ROUTES document, and the same walk
// feeds the development boot log ("nucleus: module route mounted"), so
// the two inventories cannot disagree.
type routeInventory struct {
	routes []routedump.Route
}

func (inv *routeInventory) add(routes ...routedump.Route) {
	if inv == nil {
		return
	}
	inv.routes = append(inv.routes, routes...)
}

// moduleRoutes lists the routes a module just registered on m, module
// prefix applied. skip is the number of entries m held before the module
// registered (a module without prefix or middleware registers directly on
// the shared root mux); a sub-router starts empty, so skip is 0 there.
func moduleRoutes(name, prefix string, m *routerpkg.Mux, skip int) []routedump.Route {
	var routes []routedump.Route
	i := 0
	_ = m.Walk(func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		i++
		if i <= skip {
			return nil
		}
		if method == "" {
			method = "*"
		}
		routes = append(routes, routedump.Route{
			Method:      method,
			Pattern:     prefix + pattern,
			Module:      name,
			Middlewares: len(mws),
		})
		return nil
	})
	return routes
}

// frameworkRoutes lists the root mux entries that no module accounts for:
// everything registered before the first module mounted (index < skipFrom)
// and everything registered after the last module-attributed entry
// (index >= skipTo, the OpenAPI document today). The range in between is
// the modules' own registrations — direct routes, group copies and the
// `<prefix>/*` mount entries — which the inventory already carries with
// better fidelity (the sub-router's real patterns instead of one subtree).
func frameworkRoutes(m *routerpkg.Mux, skipFrom, skipTo int) []routedump.Route {
	var routes []routedump.Route
	i := 0
	_ = m.Walk(func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		idx := i
		i++
		if idx >= skipFrom && idx < skipTo {
			return nil
		}
		if method == "" {
			method = "*"
		}
		routes = append(routes, routedump.Route{
			Method:      method,
			Pattern:     pattern,
			Middlewares: len(mws),
		})
		return nil
	})
	return routes
}

// printRoutesRequested reports whether the process was started to print
// its route table instead of serving. Read once per RunContext; the value
// is the caller's environment, never the config file, because the point is
// to inspect a binary without editing anything it reads.
func printRoutesRequested() bool {
	return routedump.Enabled(os.Getenv(routedump.EnvVar))
}

// printRoutesAndStop is the NUCLEUS_PRINT_ROUTES exit of RunContext: it
// writes the document, then unwinds what boot already set up (module
// OnShutdown hooks, the database pools and every other shutdown hook
// app.New registered) with the same budget a graceful shutdown gets. No
// listener was ever opened, so nothing else needs closing.
func printRoutesAndStop(core *app.App, inv *routeInventory, skipFrom, skipTo int) error {
	doc := routedump.Document{Env: core.Config.Env, Routes: []routedump.Route{}}
	if core.Router != nil {
		doc.Routes = append(doc.Routes, frameworkRoutes(core.Router.Mux, skipFrom, skipTo)...)
	}
	if inv != nil {
		doc.Routes = append(doc.Routes, inv.routes...)
	}
	if err := routedump.Encode(printRoutesOut, doc); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleShutdownTimeout(core))
	defer cancel()
	return core.Shutdown(ctx)
}
