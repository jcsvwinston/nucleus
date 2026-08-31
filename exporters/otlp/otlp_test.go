// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package otlp

import (
	"context"
	"slices"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe/exporter"
)

// The point of the module is the side effect: a build that imports it and
// still cannot resolve "otlp" has registered nothing.
func TestRegisters(t *testing.T) {
	if !slices.Contains(exporter.Registered(), "otlp") {
		t.Errorf("importing this module must register the otlp exporter; registered: %v", exporter.Registered())
	}
}

// The exporter must supply BOTH halves. A version that returned only spans
// would leave metrics silently unexported while the endpoint looked
// configured — the failure mode this whole arc has to avoid.
func TestSuppliesTracesAndMetrics(t *testing.T) {
	factory, ok := exporter.Lookup("otlp")
	if !ok {
		t.Fatal("otlp is not registered")
	}
	// The HTTP exporters do not dial at construction, so this needs no
	// collector.
	exp, err := factory(context.Background(), exporter.Config{
		ServiceName: "test",
		Endpoint:    "localhost:4318",
		Insecure:    true,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if exp.Spans == nil {
		t.Error("no span exporter: traces would be dropped with the endpoint configured")
	}
	if exp.Reader == nil {
		t.Error("no metric reader: metrics would be dropped with the endpoint configured")
	}
	if exp.Handler != nil {
		t.Error("OTLP pushes to a collector; it must not claim a scrape handler")
	}
	if exp.Reader != nil {
		_ = exp.Reader.Shutdown(context.Background())
	}
	if exp.Spans != nil {
		_ = exp.Spans.Shutdown(context.Background())
	}
}
