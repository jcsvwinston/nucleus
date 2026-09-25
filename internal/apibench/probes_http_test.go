// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// The http family asks what a handler author gets from the routing layer —
// the listón is Echo and Goa: typed binding of body, query, path and headers
// with one error shape (problem+json), content negotiation, declarative
// versioning, timeouts where the route says. Most probes call the bench
// module's routes; the ones about a helper the author would call ask the
// Context's method set and say what they found.

func contextMethods() []string { return methodNames((*nucleus.Context)(nil)) }

// HT-01: a JSON body binds into a struct and is validated by its tags.
func probeJSONBinding(t *testing.T, e *env) verdict {
	ok, raw := e.do(t, http.MethodPost, "/bench/echo", map[string]any{"name": "one", "age": 3}, nil)
	if ok.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"name":"one"`) {
		t.Logf("valid body → %d %.200s", ok.StatusCode, raw)
		return absent
	}
	bad, raw := e.do(t, http.MethodPost, "/bench/echo", map[string]any{"age": 3}, nil)
	if bad.StatusCode != http.StatusBadRequest && bad.StatusCode != http.StatusUnprocessableEntity {
		t.Logf("a body missing a required field → %d %.200s", bad.StatusCode, raw)
		return partial
	}
	return present
}

// HT-02: query parameters bind into a struct, typed.
func probeQueryBinding(t *testing.T, _ *env) verdict {
	if n, ok := anyMethod(contextMethods(), "BindQuery", "QueryStruct", "BindParams"); ok {
		t.Logf("query binding: %s", n)
		return present
	}
	t.Log("Query(name) returns one string; a filter with a page, a size and a sort is parsed field by field in every handler")
	return absent
}

// HT-03: path parameters bind typed.
func probePathBinding(t *testing.T, _ *env) verdict {
	if n, ok := anyMethod(contextMethods(), "BindPath", "ParamInt", "ParamInt64", "ParamUUID"); ok {
		t.Logf("typed path params: %s", n)
		return present
	}
	t.Log("Param(name) returns a string; every numeric id is converted and range-checked by the handler")
	return absent
}

// HT-04: headers bind typed.
func probeHeaderBinding(t *testing.T, _ *env) verdict {
	if n, ok := anyMethod(contextMethods(), "BindHeader", "BindHeaders", "Header"); ok {
		t.Logf("header binding: %s", n)
		return present
	}
	t.Log("no header binding; handlers read c.Request.Header by hand")
	return absent
}

// HT-05: a validation failure names the field that failed.
func probeStructuredValidationErrors(t *testing.T, e *env) verdict {
	resp, raw := e.do(t, http.MethodPost, "/bench/echo", map[string]any{"age": 3}, nil)
	body := string(raw)
	if resp.StatusCode >= 500 || len(body) == 0 {
		t.Logf("%d %.200s", resp.StatusCode, body)
		return absent
	}
	if !strings.Contains(strings.ToLower(body), "name") {
		t.Logf("the failure does not name the field: %.300s", body)
		return partial
	}
	if jsonKeys(raw) == nil {
		t.Logf("the failure is not a JSON object: %.300s", body)
		return partial
	}
	t.Logf("validation failure: %.300s", body)
	return present
}

// HT-06: errors are problem+json (RFC 9457).
func probeProblemJSON(t *testing.T, e *env) verdict {
	resp, raw := e.do(t, http.MethodGet, "/bench/notfound", nil, nil)
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/problem+json") {
		return present
	}
	keys := jsonKeys(raw)
	t.Logf("a domain error answers %d as %q with keys %v — a JSON envelope of the framework's own shape, not problem+json", resp.StatusCode, ct, keys)
	return absent
}

// HT-07: content negotiation by Accept — one handler, the representation the
// client asked for.
func probeNegotiation(t *testing.T, _ *env) verdict {
	if n, ok := anyMethod(contextMethods(), "Negotiate", "Accepts", "Format", "Respond"); ok {
		t.Logf("negotiation: %s", n)
		return present
	}
	t.Log("JSON(), XML(), HTML() and String() each commit to one representation; nothing reads Accept and picks")
	return absent
}

// HT-08: declarative API versioning.
func probeVersioning(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`(?i)\bVersion(ed|ing)?\b`)
	files := sourceMatches(t, "pkg/nucleus", regexp.MustCompile(`Router interface`))
	if len(files) == 0 {
		return absent
	}
	for _, f := range files {
		if strings.Contains(f, "router.go") {
			if src := sourceMatches(t, "pkg/nucleus", regexp.MustCompile(`Router interface[\s\S]*?\}`)); len(src) > 0 {
				// The Router interface exists; does it (or Module) declare a version?
				if vs := sourceMatches(t, "pkg/nucleus", regexp.MustCompile(`(?m)^\s*(Version|APIVersion)\s`)); len(vs) > 0 {
					t.Logf("version declared in %v", vs)
					return present
				}
			}
		}
	}
	_ = re
	t.Log("neither Router nor Module declares a version; /v1 is a Prefix the author types, with no header or negotiation behind it")
	return absent
}

// HT-09: a timeout where the route says.
func probePerRouteTimeout(t *testing.T, _ *env) verdict {
	global := sourceMatches(t, "pkg/router", regexp.MustCompile(`func WithTimeout\(`))
	exempt := sourceMatches(t, "pkg/router", regexp.MustCompile(`func WithTimeoutExempt\(`))
	perRoute := sourceMatches(t, "pkg/router", regexp.MustCompile(`RouteTimeout|WithRouteTimeout|Timeout\(.*time\.Duration\).*Router`))
	switch {
	case len(perRoute) > 0:
		return present
	case len(global) > 0 && len(exempt) > 0:
		t.Log("one timeout for the whole router (WithTimeout) with exempt path prefixes (WithTimeoutExempt); a slow export and a fast lookup share the same limit unless one is exempted entirely")
		return partial
	case len(global) > 0:
		return partial
	}
	return absent
}

// HT-10: an unknown route answers a JSON 404 in the framework's envelope.
func probeUnknownRouteJSON(t *testing.T, e *env) verdict {
	resp, raw := e.do(t, http.MethodGet, "/bench/nope", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Logf("unknown route → %d", resp.StatusCode)
		return absent
	}
	top, topRaw := e.do(t, http.MethodGet, "/nope", nil, nil)
	t.Logf("outside any module: GET /nope → %d %.120q", top.StatusCode, topRaw)
	if jsonKeys(raw) == nil {
		t.Logf("404 body under the module prefix is not JSON: %.200s", raw)
		return partial
	}
	return present
}

// HT-11: the raw-HTML writer is named as such; the template renderer is HTML.
// nucleus.Context.HTML(code, html) writes a raw string while
// router.Context.HTML(status, template, data) renders a template — same name,
// two meanings (NU-41).
func probeRawHTMLNamed(t *testing.T, _ *env) verdict {
	names := contextMethods()
	if _, ok := anyMethod(names, "RawHTML"); ok {
		return present
	}
	t.Logf("nucleus.Context has HTML (raw string) and Render (template) while router.Context.HTML renders a template; no RawHTML. methods: %v", names)
	return absent
}

// HT-12: one error envelope — a domain error and the router's own 404 share
// the same shape.
func probeErrorEnvelopeConsistent(t *testing.T, e *env) verdict {
	_, domain := e.do(t, http.MethodGet, "/bench/notfound", nil, nil)
	_, routed := e.do(t, http.MethodGet, "/bench/nope", nil, nil)
	dk, rk := jsonKeys(domain), jsonKeys(routed)
	if dk == nil || rk == nil {
		t.Logf("domain: %.200s\nrouter: %.200s", domain, routed)
		return absent
	}
	if strings.Join(dk, ",") != strings.Join(rk, ",") {
		t.Logf("domain error keys %v, router 404 keys %v", dk, rk)
		return partial
	}
	t.Logf("both answer with keys %v", dk)
	return present
}
