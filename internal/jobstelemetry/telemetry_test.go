// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobstelemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The FIRST provider an application installs needs no help from us: OTel's
// global provider delegates instruments created before it. This test says so —
// it would pass without any rebinding at all — and exists to pin that the
// ordinary boot order records something.
func TestInstrumentsFollowAProviderInstalledLater(t *testing.T) {
	// Something records before any real provider exists.
	Enqueued(context.Background(), "memory", "default", "early.job")

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	// And now the application's own work.
	ctx := context.Background()
	Enqueued(ctx, "memory", "default", "real.job")
	Started(ctx, "memory", "default", "real.job")
	Succeeded(ctx, "memory", "default", "real.job", 12*time.Millisecond)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var names []string
	for _, scope := range collected.ScopeMetrics {
		for _, series := range scope.Metrics {
			names = append(names, series.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("no series recorded: the instruments stayed bound to the provider that was global before boot")
	}
	t.Logf("series after installing a provider later: %v", names)
}

// And this is the one the rebinding is FOR. OTel delegates once, so a second
// provider never reaches instruments built before it: everything the process
// records from then on is scraped by nobody. It is every test suite that stands
// an application up twice — and it is how the jobs bench came to measure zero
// series depending on which probe ran first. Reverting bind() to a sync.Once
// fails this test and passes the one above, which is the whole point of having
// both.
func TestInstrumentsFollowASecondProvider(t *testing.T) {
	ctx := context.Background()
	previous := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	firstReader := sdkmetric.NewManualReader()
	first := sdkmetric.NewMeterProvider(sdkmetric.WithReader(firstReader))
	otel.SetMeterProvider(first)
	Enqueued(ctx, "memory", "default", "first.job")

	if err := first.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown the first provider: %v", err)
	}
	secondReader := sdkmetric.NewManualReader()
	second := sdkmetric.NewMeterProvider(sdkmetric.WithReader(secondReader))
	otel.SetMeterProvider(second)
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })

	Enqueued(ctx, "memory", "default", "second.job")
	Started(ctx, "memory", "default", "second.job")

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
		t.Fatal("the second provider received nothing: the instruments are still bound to the first")
	}
	t.Logf("series in the second provider: %v", names)
}
