// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package asynqprovider

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The instruments used to be bound under a sync.Once. OTel's global provider
// covers the obvious case on its own — instruments created before the first
// SetMeterProvider are delegated to it — but it delegates ONCE, so a second
// provider never reaches instruments that were built before it. That is not a
// hypothetical: it is every test suite that stands an application up twice, and
// it is how the jobs bench measured zero series depending on which probe ran
// first. The other two providers rebind (internal/jobstelemetry); this one has
// to as well.
func TestInstrumentsFollowASecondProvider(t *testing.T) {
	ctx := context.Background()
	previous := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	// The first application's provider, and a job recorded into it.
	firstReader := sdkmetric.NewManualReader()
	first := sdkmetric.NewMeterProvider(sdkmetric.WithReader(firstReader))
	otel.SetMeterProvider(first)
	taskTelemetry().enqueueTotal.Add(ctx, 1, metric.WithAttributes())

	// It shuts down, and a second one takes its place — another test, another
	// application in the same process.
	if err := first.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown the first provider: %v", err)
	}
	secondReader := sdkmetric.NewManualReader()
	second := sdkmetric.NewMeterProvider(sdkmetric.WithReader(secondReader))
	otel.SetMeterProvider(second)
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })

	taskTelemetry().enqueueTotal.Add(ctx, 1, metric.WithAttributes())

	var collected metricdata.ResourceMetrics
	if err := secondReader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var names []string
	for _, scope := range collected.ScopeMetrics {
		for _, series := range scope.Metrics {
			names = append(names, series.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("the second provider received nothing: the instruments are still bound to the first, so everything this process records from here on is scraped by nobody")
	}
	t.Logf("series in the second provider: %v", names)
}

// The names are a contract with whoever scrapes them, so they are asserted
// literally rather than read back from the instruments that produced them.
func TestInstrumentNamesAreTheOnesPublished(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	ctx := context.Background()
	tel := taskTelemetry()
	tel.enqueueTotal.Add(ctx, 1, metric.WithAttributes())
	tel.enqueueErrors.Add(ctx, 1, metric.WithAttributes())
	tel.started.Add(ctx, 1, metric.WithAttributes())
	tel.succeeded.Add(ctx, 1, metric.WithAttributes())
	tel.retried.Add(ctx, 1, metric.WithAttributes())
	tel.failed.Add(ctx, 1, metric.WithAttributes())
	tel.duration.Record(ctx, 1, metric.WithAttributes())

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	seen := map[string]bool{}
	for _, scope := range collected.ScopeMetrics {
		for _, series := range scope.Metrics {
			seen[series.Name] = true
		}
	}
	for _, want := range []string{
		"jobs.enqueue.total",
		"jobs.enqueue.errors",
		"jobs.process.started",
		"jobs.process.succeeded",
		"jobs.process.retried",
		"jobs.process.failed",
		"jobs.process.duration.ms",
	} {
		if !seen[want] {
			t.Errorf("the provider stopped publishing %q", want)
		}
	}
}
