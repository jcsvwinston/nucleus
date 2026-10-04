// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIVersion declares one version of an API: the path segment it is served
// under and, once it is on its way out, the dates and links a client needs
// to move off it. The router mounts the version's routes under
// "/<Name>" (Mux.Version; in a nucleus module, Module.Version or
// nucleus.Versioned) and stamps every response of the version — its 404s
// included — with the standard headers:
//
//   - Deprecation (RFC 9745), "@<unix seconds>", when Deprecated is set —
//     a date in the future announces the deprecation ahead of time;
//   - Sunset (RFC 8594), an HTTP date, when Sunset is set: the moment after
//     which the version may stop answering;
//   - Link with rel="successor-version" (RFC 5829) when Successor is set,
//     and rel="deprecation" (RFC 9745) when Policy is set.
//
// A version that is neither deprecated nor sunset adds no header: the path
// segment is the whole declaration. The router does not stop serving a
// version after its Sunset date; the header is the promise, and removing
// the routes is the application's change to make.
type APIVersion struct {
	// Name is the version's path segment: "v1", "v2", "2026-10-01". One
	// segment of letters, digits, '.', '-', '_' or '~'. Required.
	Name string
	// Deprecated, when set, is the moment the version is (or was)
	// deprecated.
	Deprecated time.Time
	// Sunset, when set, is the moment after which the version may stop
	// answering.
	Sunset time.Time
	// Successor, when set, is the URL (absolute, or a path on this server)
	// of the version that replaces this one.
	Successor string
	// Policy, when set, is the URL of a page that explains the deprecation:
	// what changes, how to migrate.
	Policy string
}

// Validate reports a version the router cannot mount: a Name that is
// empty or not a single path segment, or a link that would break the
// header it goes in.
func (v APIVersion) Validate() error {
	if v.Name == "" {
		return fmt.Errorf("router: APIVersion.Name is required")
	}
	for _, c := range v.Name {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '-' || c == '_' || c == '~'
		if !ok {
			return fmt.Errorf("router: APIVersion.Name %q must be one path segment of letters, digits, '.', '-', '_' or '~'", v.Name)
		}
	}
	if v.Name == "." || v.Name == ".." {
		return fmt.Errorf("router: APIVersion.Name %q is not a path segment", v.Name)
	}
	for field, link := range map[string]string{"Successor": v.Successor, "Policy": v.Policy} {
		if strings.ContainsAny(link, "<>\r\n\" ") {
			return fmt.Errorf("router: APIVersion.%s %q cannot go in a Link header", field, link)
		}
	}
	return nil
}

// Path is the segment the version is mounted under, "/<Name>".
func (v APIVersion) Path() string { return "/" + v.Name }

// Headers returns the middleware that stamps the version's Deprecation,
// Sunset and Link headers (see APIVersion) on every response it wraps.
// Mux.Version mounts it; use it directly to stamp routes that are not
// mounted under the version's path.
func (v APIVersion) Headers() Middleware {
	var links []string
	if v.Successor != "" {
		links = append(links, "<"+v.Successor+`>; rel="successor-version"`)
	}
	if v.Policy != "" {
		links = append(links, "<"+v.Policy+`>; rel="deprecation"`)
	}
	deprecation, sunset := "", ""
	if !v.Deprecated.IsZero() {
		deprecation = "@" + strconv.FormatInt(v.Deprecated.Unix(), 10)
	}
	if !v.Sunset.IsZero() {
		sunset = v.Sunset.UTC().Format(http.TimeFormat)
	}
	return func(next http.Handler) http.Handler {
		if deprecation == "" && sunset == "" && len(links) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if deprecation != "" {
				h.Set("Deprecation", deprecation)
			}
			if sunset != "" {
				h.Set("Sunset", sunset)
			}
			for _, l := range links {
				h.Add("Link", l)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Version mounts the routes fn registers under the version's path,
// "/<Name>", and stamps their responses with the version's headers (see
// APIVersion):
//
//	r.Version(router.APIVersion{Name: "v1", Deprecated: d, Sunset: s, Successor: "/v2"}, func(v1 *router.Mux) {
//	    v1.Get("/notes", listNotesV1)
//	})
//	r.Version(router.APIVersion{Name: "v2"}, func(v2 *router.Mux) {
//	    v2.Get("/notes", listNotes)
//	})
//
// It panics on a version Validate rejects, as a conflicting pattern does:
// a malformed version is a programming error caught at startup.
func (m *Mux) Version(v APIVersion, fn func(sub *Mux)) {
	if err := v.Validate(); err != nil {
		panic(err)
	}
	m.Group(func(g *Mux) {
		g.Use(v.Headers())
		g.Route(v.Path(), fn)
	})
}
