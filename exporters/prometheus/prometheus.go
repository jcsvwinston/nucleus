// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package prometheus links the Prometheus exporter into a Nucleus
// application, serving OpenMetrics output for scraping.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/exporters/prometheus"
//
// The configuration does not change: observability.prometheus_enabled still
// turns it on, and the framework still mounts the handler at the configured
// metrics path. The import is the only new part.
//
// It ships separately because client_golang pulls protobuf — 37 packages that
// every application carried, including the ones that are never scraped. That
// dependency is Prometheus's, not OTLP's, which is why removing the OTLP
// exporter alone did not remove protobuf.
package prometheus

import (
	"context"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"

	"github.com/jcsvwinston/nucleus/pkg/observe/exporter"
)

func init() {
	exporter.MustRegister("prometheus", newExporter)
}

func newExporter(_ context.Context, _ exporter.Config) (exporter.Exporter, error) {
	// A private registry rather than the package-global default: the global
	// one collects whatever any dependency happens to register into it, and
	// what this endpoint serves should be what the application asked for.
	registry := promclient.NewRegistry()
	reader, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return exporter.Exporter{}, err
	}
	return exporter.Exporter{
		Reader: reader,
		Handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
			Registry:          registry,
		}),
	}, nil
}
