package observe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TelemetryConfig configures OpenTelemetry initialization.
type TelemetryConfig struct {
	ServiceName       string
	OTLPEndpoint      string
	PrometheusEnabled bool
}

// SetupOpenTelemetry initializes the global OpenTelemetry providers
// and, when PrometheusEnabled is set, returns an http.Handler that
// serves OpenMetrics-compatible Prometheus output for /metrics
// scraping.
//
// Trace + metric exporters target OTLPEndpoint when configured;
// otherwise that side is a no-op. The Prometheus reader is independent
// — it is attached to the MeterProvider regardless of OTLP, so a
// deployment can scrape locally without an OTLP collector.
//
// Return values:
//
//   - shutdown is always non-nil. It must be called on app teardown to
//     flush remaining telemetry; the no-op case (no exporters
//     configured) returns immediately.
//   - metricsHandler is non-nil only when PrometheusEnabled is true.
//     Callers should mount it at the configured /metrics path.
func SetupOpenTelemetry(ctx context.Context, cfg TelemetryConfig, logger *slog.Logger) (shutdown func(context.Context) error, metricsHandler http.Handler, err error) {
	noop := func(context.Context) error { return nil }

	otlpEnabled := strings.TrimSpace(cfg.OTLPEndpoint) != ""
	if !otlpEnabled && !cfg.PrometheusEnabled {
		return noop, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "nucleus"
	}

	res, rerr := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if rerr != nil {
		return noop, nil, fmt.Errorf("observe.SetupOpenTelemetry resource: %w", rerr)
	}

	meterOptions := []metric.Option{metric.WithResource(res)}
	var traceProvider *trace.TracerProvider

	if otlpEnabled {
		endpoint, insecure, perr := parseOTLPEndpoint(cfg.OTLPEndpoint)
		if perr != nil {
			return noop, nil, fmt.Errorf("observe.SetupOpenTelemetry parse endpoint: %w", perr)
		}

		traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
		metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(endpoint)}
		if insecure {
			traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
			metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
		}

		traceExporter, terr := otlptracehttp.New(ctx, traceOpts...)
		if terr != nil {
			return noop, nil, fmt.Errorf("observe.SetupOpenTelemetry trace exporter: %w", terr)
		}
		metricExporter, merr := otlpmetrichttp.New(ctx, metricOpts...)
		if merr != nil {
			return noop, nil, fmt.Errorf("observe.SetupOpenTelemetry metric exporter: %w", merr)
		}
		traceProvider = trace.NewTracerProvider(
			trace.WithBatcher(traceExporter),
			trace.WithResource(res),
		)
		meterOptions = append(meterOptions, metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(15*time.Second))))
	}

	var promHandler http.Handler
	if cfg.PrometheusEnabled {
		registry := prometheus.NewRegistry()
		promReader, perr := otelprom.New(otelprom.WithRegisterer(registry))
		if perr != nil {
			return noop, nil, fmt.Errorf("observe.SetupOpenTelemetry prometheus exporter: %w", perr)
		}
		meterOptions = append(meterOptions, metric.WithReader(promReader))
		promHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
			Registry:          registry,
		})
	}

	mp := metric.NewMeterProvider(meterOptions...)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if traceProvider != nil {
		otel.SetTracerProvider(traceProvider)
	}
	otel.SetMeterProvider(mp)

	logger.Info("otel initialized",
		"service", serviceName,
		"otlp_enabled", otlpEnabled,
		"prometheus_enabled", cfg.PrometheusEnabled,
	)

	shutdownFn := func(shutdownCtx context.Context) error {
		if shutdownCtx == nil {
			shutdownCtx = context.Background()
		}
		var errs []error
		if err := mp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
		if traceProvider != nil {
			if err := traceProvider.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, fmt.Errorf("tracer provider shutdown: %w", err))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}

	return shutdownFn, promHandler, nil
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
