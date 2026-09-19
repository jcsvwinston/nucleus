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

// An application installs its meter provider during boot, which is AFTER
// whatever ran first touched these instruments. Binding them once and for all
// would leave them pointing at the no-op provider that was global at the time,
// and the application would record nothing for the rest of its life.
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
