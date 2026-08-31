// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package exporter is the contract a telemetry exporter module implements to
// plug into pkg/observe.
//
// The framework keeps the OpenTelemetry SDK — the tracer and meter providers,
// the resource, the propagators — because that is what instrumentation all
// over the code calls into, and it is small. What it no longer keeps is the
// EXPORTERS, which are the expensive half: the OTLP exporter pulls gRPC and
// protobuf even in its HTTP flavour (its internal config package imports gRPC
// regardless), and the Prometheus exporter pulls client_golang and, through
// it, protobuf again. Together they were 118 packages of a 519-package
// hello-world that every application paid whether or not it exported
// anything.
//
// A module registers its exporter in init(); the application imports it for
// the side effect:
//
//	import _ "github.com/jcsvwinston/nucleus/exporters/otlp"
//
// The configuration does NOT change. An application that already sets
// observability.otlp_endpoint keeps that file exactly as it is — the endpoint
// still selects OTLP, and the only new thing is the import. When the setting
// is present and the module is not, pkg/observe says so at startup and names
// the import, rather than starting up and exporting nothing.
package exporter

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config is what the framework knows at startup and an exporter may need.
type Config struct {
	// ServiceName identifies this application in the telemetry backend.
	ServiceName string

	// Endpoint is the collector address, for exporters that talk to one.
	// Empty for exporters that are scraped instead, such as Prometheus.
	Endpoint string

	// Insecure requests plaintext transport, derived from the endpoint's
	// scheme by the framework so every exporter reads it the same way.
	Insecure bool
}

// Exporter is what a module hands back. Every field is optional, and an
// exporter supplies the ones it has: OTLP returns spans and a metric reader,
// Prometheus returns a reader and the handler that serves /metrics.
type Exporter struct {
	// Spans receives finished spans. Nil for an exporter that does not
	// handle traces.
	Spans sdktrace.SpanExporter

	// Reader is attached to the MeterProvider. Nil for an exporter that does
	// not handle metrics.
	Reader sdkmetric.Reader

	// Handler is served at the configured metrics path. Only a scraped
	// exporter returns one.
	Handler http.Handler

	// Shutdown releases whatever the exporter holds. It may be nil; the
	// framework already shuts down the providers it built.
	Shutdown func(context.Context) error
}

// Factory builds an exporter. It is called once, at startup, and an error it
// returns stops the application: telemetry that was asked for and silently
// did not happen is worse than a failure to start, because nothing later
// distinguishes it from a system that is simply quiet.
type Factory func(ctx context.Context, cfg Config) (Exporter, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register records the factory for name. Names are the ones that appear in
// configuration: "otlp", "prometheus".
func Register(name string, f Factory) error {
	if name == "" {
		return fmt.Errorf("observe/exporter: name is required")
	}
	if f == nil {
		return fmt.Errorf("observe/exporter: %s: factory is required", name)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[name]; dup {
		return fmt.Errorf("observe/exporter: %s is already registered", name)
	}
	factories[name] = f
	return nil
}

// MustRegister is Register for use in an init(), where there is no caller to
// hand an error back to.
func MustRegister(name string, f Factory) {
	if err := Register(name, f); err != nil {
		panic(err)
	}
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[name]
	return f, ok
}

// Registered returns the exporter names linked into this binary, sorted.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(factories))
	for n := range factories {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
