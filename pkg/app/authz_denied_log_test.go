// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression test for DX-2 (DX audit 2026-08-16, §3.2): a request denied by
// the framework's default-deny middleware produced ONE log line —
// `msg=http_request … status=403` — even at debug level. The good line
// already existed (pkg/authz.Enforcer.Middleware logs subject/resource/
// action) but lived on the path the framework does not mount. The
// default-deny mount must say who was denied what, and hand the operator
// the exact CSV row that would allow it.
package app

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
)

func TestDefaultDenyLogsActionableDenial(t *testing.T) {
	enf, err := authz.New(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	mw := buildDefaultAuthzMiddleware(enf, logger)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/notes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	logged := logBuf.String()
	for _, want := range []string{
		"authz denied",
		"subject=anonymous",
		"resource=/notes",
		"action=read",
		"p, anonymous, /notes, read, allow",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("denial log missing %q:\n%s", want, logged)
		}
	}
}

// Same family (DX-2 corollary, app audit §3.2): an EXPLICIT
// rbac_policy_file pointing at a missing file used to boot the app into
// total default-deny with a WARN telling the operator to set the very key
// they had already set. A typo in a filename must fail startup naming it.
func TestExplicitMissingPolicyFileFailsStartup(t *testing.T) {
	cfg := testAppConfig()
	cfg.RBACPolicyFile = "/tmp/definitely-missing-rbac.csv"

	_, err := New(cfg)
	if err == nil {
		t.Fatal("New must fail when the explicit rbac_policy_file does not exist")
	}
	if !strings.Contains(err.Error(), "definitely-missing-rbac.csv") {
		t.Errorf("the error must name the missing policy file, got: %v", err)
	}
}
