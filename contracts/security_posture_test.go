// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Track E (Security and Compliance Baseline): the default hardening
// profile, frozen.
//
// Every value in the baseline is OBSERVED, never transcribed: the headers
// come out of a real HTTP response from a really-booted application, the
// cookie attributes out of a real Set-Cookie, and the config values out of
// app.DefaultConfig() read by koanf tag. A baseline cannot therefore claim
// a protection the framework does not actually emit — the failure mode of
// every hand-written security checklist.
//
// The comparison is EXACT, in both directions. A loosened default is the
// regression this exists to catch; a tightened one is a compatibility event
// for anyone whose deployment depended on the old posture. Both must be
// deliberate, which is what regenerating the baseline
// (`go test ./contracts/ -run TestSecurityPosture -update`) and reviewing
// the diff means.
package contracts

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

var updatePosture = flag.Bool("update", false, "rewrite the security posture baseline")

// securityConfigKeys are the koanf keys that make up the posture. The list
// is a POLICY choice — which knobs are security-relevant — so it lives here
// rather than being derived. A rename breaks the test loudly: the key is
// looked up in the struct by tag, and a miss is a failure, not a skip.
var securityConfigKeys = []string{
	"cors_allow_credentials",
	"cors_origins",
	"csrf_enabled",
	"csrf_insecure_cookie",
	"debug",
	"jwt_expiry",
	"rate_limit_burst",
	"rate_limit_requests",
	"rate_limit_window",
	"session_cookie_name",
	"session_cookie_path",
	"session_cookie_samesite",
	"session_cookie_secure",
	"session_idle_timeout",
	"session_lifetime",
	"session_store",
	"trusted_proxies",
}

// securityHeaderNames are the response headers that carry posture. Limiting
// the record to this list keeps the document deterministic (Date, request
// ids and content lengths do not belong in a frozen baseline) while a
// header that STOPS being emitted still shows up — as "(absent)".
var securityHeaderNames = []string{
	"Content-Security-Policy",
	"Cross-Origin-Opener-Policy",
	"Cross-Origin-Resource-Policy",
	"Permissions-Policy",
	"Referrer-Policy",
	"Strict-Transport-Security",
	"X-Content-Type-Options",
	"X-Frame-Options",
	"X-XSS-Protection",
}

// corsHeaderNames are the headers a cross-origin probe must NOT get back
// under the deny-by-default posture (ADR-013 R4 / DEP-2026-007).
var corsHeaderNames = []string{
	"Access-Control-Allow-Credentials",
	"Access-Control-Allow-Headers",
	"Access-Control-Allow-Methods",
	"Access-Control-Allow-Origin",
	"Access-Control-Expose-Headers",
	"Access-Control-Max-Age",
}

func TestSecurityPosture_MatchesBaseline(t *testing.T) {
	got := renderSecurityPosture(t)
	path := filepath.Join("baseline", "security_posture.txt")

	if *updatePosture {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		t.Logf("baseline rewritten: %s", path)
		return
	}

	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline (regenerate with -update): %v", err)
	}
	if want := string(wantBytes); want != got {
		t.Fatalf("the default security posture changed.\n\n%s\n\n"+
			"A LOOSENED default is a regression. A TIGHTENED one is a compatibility\n"+
			"event for deployments that relied on the old posture. Either way it must\n"+
			"be deliberate: state the reason in the PR, then regenerate with\n"+
			"  go test ./contracts/ -run TestSecurityPosture -update",
			firstDifference(want, got))
	}
}

func renderSecurityPosture(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	b.WriteString("# Nucleus default security posture — OBSERVED, not transcribed.\n")
	b.WriteString("# Regenerate: go test ./contracts/ -run TestSecurityPosture -update\n")
	b.WriteString("# See contracts/security_posture_test.go for what each section measures.\n")

	b.WriteString("\n[config-defaults]\n")
	for _, line := range renderConfigDefaults(t) {
		b.WriteString(line + "\n")
	}

	// Two profiles, because the posture legitimately differs: HSTS is
	// emitted unconditionally in production and only over TLS elsewhere
	// (app.go wires router.WithHSTS(cfg.IsProd())). Recording both makes
	// that difference auditable instead of folklore.
	for _, env := range []string{"development", "production"} {
		headers, cors, csrfCookies := observePosture(t, env)

		b.WriteString(fmt.Sprintf("\n[response-headers env=%s]\n", env))
		for _, name := range securityHeaderNames {
			b.WriteString(fmt.Sprintf("%s = %s\n", name, valueOrAbsent(headers[name])))
		}

		b.WriteString(fmt.Sprintf("\n[cross-origin-probe env=%s]\n", env))
		for _, name := range corsHeaderNames {
			b.WriteString(fmt.Sprintf("%s = %s\n", name, valueOrAbsent(cors[name])))
		}

		b.WriteString(fmt.Sprintf("\n[csrf-cookies env=%s]\n", env))
		for _, line := range csrfCookies {
			b.WriteString(line + "\n")
		}
	}

	return b.String()
}

// renderConfigDefaults reads each security-relevant key off
// app.DefaultConfig() BY KOANF TAG, so a renamed field fails the test
// instead of silently dropping a line from the posture.
func renderConfigDefaults(t *testing.T) []string {
	t.Helper()

	cfg := app.DefaultConfig()
	byTag := map[string]reflect.Value{}
	v := reflect.ValueOf(cfg)
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("koanf"), ",")[0]
		if tag != "" {
			byTag[tag] = v.Field(i)
		}
	}

	lines := make([]string, 0, len(securityConfigKeys))
	for _, key := range securityConfigKeys {
		field, ok := byTag[key]
		if !ok {
			t.Fatalf("posture key %q has no field in app.Config — renamed or removed? "+
				"Update securityConfigKeys deliberately; do not drop the key silently.", key)
		}
		lines = append(lines, fmt.Sprintf("%s = %s", key, formatPostureValue(field)))
	}
	sort.Strings(lines)
	return lines
}

func formatPostureValue(v reflect.Value) string {
	if v.Kind() == reflect.Slice {
		if v.Len() == 0 {
			return "[]"
		}
		parts := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts = append(parts, fmt.Sprint(v.Index(i).Interface()))
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return fmt.Sprint(v.Interface())
}

// observePosture boots a real application and measures what it puts on the
// wire: security headers on a same-origin GET, CORS headers on a
// cross-origin GET, and the Set-Cookie attributes the CSRF middleware
// emits. Nothing here reads the framework's intentions — only its output.
func observePosture(t *testing.T, env string) (headers, cors map[string]string, csrfCookies []string) {
	t.Helper()

	cfg := app.DefaultConfig()
	cfg.Env = env
	cfg.CSRFEnabled = true
	// A production boot must not demand real infrastructure just to answer
	// a probe: the defaults already keep sessions and mail in-process.
	cfg.JWTSecret = strings.Repeat("posture-probe-secret", 2)

	srv := nucleustest.StartApp(t, nucleus.App{Config: cfg})
	t.Cleanup(srv.Stop)

	headers = probeHeaders(t, srv, securityHeaderNames, nil)
	cors = probeHeaders(t, srv, corsHeaderNames, map[string]string{
		"Origin": "https://evil.example",
	})
	csrfCookies = probeCSRFCookies(t, srv)
	return headers, cors, csrfCookies
}

func probeHeaders(t *testing.T, srv *nucleustest.Server, names []string, reqHeaders map[string]string) map[string]string {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, srv.URL("/healthz"), nil)
	if err != nil {
		t.Fatalf("build probe request: %v", err)
	}
	for k, v := range reqHeaders {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	defer resp.Body.Close()

	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = resp.Header.Get(name)
	}
	return out
}

// probeCSRFCookies records the ATTRIBUTES of every cookie the CSRF
// middleware sets — never the values, which are per-request secrets.
func probeCSRFCookies(t *testing.T, srv *nucleustest.Server) []string {
	t.Helper()

	resp, err := srv.Client().Get(srv.URL("/healthz"))
	if err != nil {
		t.Fatalf("csrf probe request: %v", err)
	}
	defer resp.Body.Close()

	lines := make([]string, 0, 2)
	for _, c := range resp.Cookies() {
		lines = append(lines, fmt.Sprintf("%s = secure=%v httponly=%v samesite=%s path=%s",
			c.Name, c.Secure, c.HttpOnly, sameSiteName(c.SameSite), valueOrAbsent(c.Path)))
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		lines = append(lines, "(no cookies set)")
	}
	return lines
}

func sameSiteName(s http.SameSite) string {
	switch s {
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteNoneMode:
		return "none"
	default:
		return "default"
	}
}

func valueOrAbsent(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(absent)"
	}
	return v
}

// firstDifference reports the first differing line with its neighbours —
// enough to see what moved without printing two full documents.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		w, g := lineAt(wantLines, i), lineAt(gotLines, i)
		if w == g {
			continue
		}
		return fmt.Sprintf("first difference at line %d:\n  baseline: %s\n  observed: %s", i+1, w, g)
	}
	return "documents differ only in trailing content"
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(end of file)"
}
