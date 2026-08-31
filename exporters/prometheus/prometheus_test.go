// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe/exporter"
)

func TestRegisters(t *testing.T) {
	if !slices.Contains(exporter.Registered(), "prometheus") {
		t.Errorf("importing this module must register the prometheus exporter; registered: %v", exporter.Registered())
	}
}

// Prometheus is scraped, not pushed: it must hand back a handler and no span
// exporter. A handler that is nil would leave the framework mounting nothing
// at the metrics path while the setting reads as enabled.
func TestServesOpenMetrics(t *testing.T) {
	factory, ok := exporter.Lookup("prometheus")
	if !ok {
		t.Fatal("prometheus is not registered")
	}
	exp, err := factory(context.Background(), exporter.Config{ServiceName: "test"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if exp.Handler == nil {
		t.Fatal("no handler: nothing would be served at the metrics path")
	}
	if exp.Reader == nil {
		t.Error("no metric reader: the handler would serve an empty registry")
	}
	if exp.Spans != nil {
		t.Error("Prometheus carries no traces; it must not claim a span exporter")
	}

	rec := httptest.NewRecorder()
	exp.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /metrics = %d, want 200", rec.Code)
	}
	// The exposition format is the contract with whatever scrapes it, so it
	// is worth asserting rather than trusting.
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Errorf("Content-Type = %q, want a Prometheus exposition format", ct)
	}
	if exp.Reader != nil {
		_ = exp.Reader.Shutdown(context.Background())
	}
}
