// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"fmt"
	"sort"
	"strings"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// APIVersion declares one version of an API: the path segment it is served
// under and, when it is on its way out, the dates and links that tell a
// client so — the Deprecation (RFC 9745), Sunset (RFC 8594) and Link
// rel="successor-version" headers on every response of the version. A
// module declares one with Module.Version; a group of routes inside a
// module, with Versioned. See router.APIVersion for the fields.
//
//	nucleus.Module[struct{}]{
//	    Name:   "notes_v1",
//	    Prefix: "/api",
//	    Version: nucleus.APIVersion{
//	        Name:       "v1",
//	        Deprecated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
//	        Sunset:     time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC),
//	        Successor:  "/api/v2/notes",
//	    },
//	    Routes: notesV1Routes, // served under /api/v1
//	}
type APIVersion = routerpkg.APIVersion

// Versioned registers the routes fn adds under the version's path segment,
// "/<Name>" below r, and stamps their responses with the version's headers
// — the route-group form of Module.Version:
//
//	Routes: func(r nucleus.Router, _ struct{}) {
//	    nucleus.Versioned(r, nucleus.APIVersion{Name: "v1", Sunset: sunset, Successor: "/api/v2/notes"}, func(g nucleus.Router) {
//	        g.Get("/notes", listNotesV1)
//	    })
//	    nucleus.Versioned(r, nucleus.APIVersion{Name: "v2"}, func(g nucleus.Router) {
//	        g.Get("/notes", listNotes)
//	    })
//	},
//
// A version APIVersion.Validate rejects is a boot error naming the module,
// like a conflicting route: the routes are not registered and Start
// returns the error (see registrationFailed).
func Versioned(r Router, v APIVersion, fn func(g Router)) {
	if err := v.Validate(); err != nil {
		registrationFailed(r, fmt.Errorf("nucleus.Versioned: %w", err))
		return
	}
	r.With(v.Headers()).Group(v.Path(), fn)
}

// versionedPrefix is the mount point of a module that declares a version:
// the version's segment appended to its Prefix.
func versionedPrefix(prefix string, v APIVersion) string {
	if v.Name == "" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + v.Path()
}

// versionedMiddleware puts the version's headers in front of the module's
// own middleware, so every response the module gives carries them — the
// errors its middleware answers too.
func versionedMiddleware(mws []Middleware, v APIVersion) []Middleware {
	if v.Name == "" {
		return mws
	}
	out := make([]Middleware, 0, len(mws)+1)
	out = append(out, v.Headers())
	return append(out, mws...)
}

// moduleVersionCarrier is the off-contract view of a module's declared
// version (see the carriers in module.go): a foreign ModuleSpec declares
// none.
type moduleVersionCarrier interface {
	apiVersion() APIVersion
}

// validateModuleVersions fails boot on a module whose Version is set and
// malformed, before anything mounts — the version's Name becomes part of
// every path the module serves.
func validateModuleVersions(specs map[string]ModuleSpec) error {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		vc, ok := specs[name].(moduleVersionCarrier)
		if !ok {
			continue
		}
		v := vc.apiVersion()
		if v == (APIVersion{}) {
			continue
		}
		if err := v.Validate(); err != nil {
			return fmt.Errorf("nucleus: module %q Version: %w", name, err)
		}
	}
	return nil
}
