package observe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TelemetryConfig configures OpenTelemetry initialization.
type TelemetryConfig struct {
	ServiceName  string
	OTLPEndpoint string
}

// SetupOpenTelemetry initializes global OpenTelemetry trace and metric providers.
// If OTLPEndpoint is empty, it returns a no-op shutdown function.
func SetupOpenTelemetry(ctx context.Context, cfg TelemetryConfig, logger *slog.Logger) (func(context.Context) error, error) {
	if strings.TrimSpace(cfg.OTLPEndpoint) == "" {
		return func(context.Context) error { return nil }, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "goframe"
	}

	endpoint, insecure, err := parseOTLPEndpoint(cfg.OTLPEndpoint)
	if err != nil {
		return nil, fmt.Errorf("observe.SetupOpenTelemetry parse endpoint: %w", err)
	}

	resource, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("observe.SetupOpenTelemetry resource: %w", err)
	}

	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(endpoint)}
	if insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}

	traceExporter, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("observe.SetupOpenTelemetry trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("observe.SetupOpenTelemetry metric exporter: %w", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(resource),
	)
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(15*time.Second))),
		metric.WithResource(resource),
	)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	logger.Info("otel initialized", "endpoint", endpoint, "service", serviceName, "insecure", insecure)

	return func(shutdownCtx context.Context) error {
		if shutdownCtx == nil {
			shutdownCtx = context.Background()
		}
		var errs []error
		if err := mp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
		if err := tp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider shutdown: %w", err))
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}, nil
}

func parseOTLPEndpoint(raw string) (endpoint string, insecure bool, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, fmt.Errorf("endpoint is required")
	}

	if !strings.Contains(trimmed, "://") {
		return trimmed, true, nil
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false, err
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("missing host in endpoint %q", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return u.Host, true, nil
	case "https":
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
	}
}
