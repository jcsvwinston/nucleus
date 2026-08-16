// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package observe is the one you almost always want: structured logging
// (slog with context-aware fields), request/trace IDs and the small set of
// helpers the rest of the framework logs through.
//
// Not to be confused with the sibling package observability, which is the
// IN-PROCESS EVENT BUS (HTTP/SQL/session events) that admin panels such as
// orbit subscribe to. If you are wiring logs, metrics labels or trace IDs,
// use observe; reach for observability only when you need to consume the
// event stream itself.
package observe
