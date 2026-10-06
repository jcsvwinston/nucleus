// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package listenhook tells a caller inside this module the moment app.Run
// has bound its listener, carried on the context Run receives. It exists
// for the CLI's `serve`, which prints its own "Nucleus server listening on"
// line: printed before Run, the line came before the bind, and a tool that
// waited for it dialed a port nothing accepted on yet (NU-115). It is
// internal on purpose: applications read the framework's own log line,
// which Run emits at the same moment.
package listenhook

import "context"

// Func receives the address the server listens on — the configured host
// with the port actually bound, so a configured port 0 reports the one the
// system assigned — and the URL built from it.
type Func func(addr, url string)

type key struct{}

// With returns a context that carries fn to app.Run.
func With(ctx context.Context, fn Func) context.Context {
	return context.WithValue(ctx, key{}, fn)
}

// From returns the hook ctx carries, or nil.
func From(ctx context.Context) Func {
	fn, _ := ctx.Value(key{}).(Func)
	return fn
}
