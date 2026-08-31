// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package otlp links the OpenTelemetry OTLP exporter into a Nucleus
// application, sending traces and metrics to a collector over HTTP.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/exporters/otlp"
//
// The configuration does not change: observability.otlp_endpoint still
// selects and addresses this exporter. The import is the only new part, and
// an application that sets the endpoint without it refuses to start with a
// message naming this line — rather than starting up and exporting nothing.
//
// It ships separately because of what it weighs: the OTLP exporter pulls gRPC
// and protobuf even in this HTTP flavour, since the exporter's internal
// configuration package imports gRPC regardless of transport. That was 82
// packages every application carried whether or not it ever spoke to a
// collector.
package otlp

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/jcsvwinston/nucleus/pkg/observe/exporter"
)

// exportInterval is how often metrics are pushed. It matches what the
// framework used before this exporter moved out, so an upgrade changes
// nothing an operator would see in their dashboards.
const exportInterval = 15 * time.Second

func init() {
	exporter.MustRegister("otlp", newExporter)
}

func newExporter(ctx context.Context, cfg exporter.Config) (exporter.Exporter, error) {
	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}

	spans, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return exporter.Exporter{}, err
	}
	metrics, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		// The trace exporter is already open. Closing it here keeps a
		// failed startup from leaving a live connection behind, which
		// matters when the caller retries.
		_ = spans.Shutdown(ctx)
		return exporter.Exporter{}, err
	}

	return exporter.Exporter{
		Spans:  spans,
		Reader: sdkmetric.NewPeriodicReader(metrics, sdkmetric.WithInterval(exportInterval)),
	}, nil
}
