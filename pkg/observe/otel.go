package observe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/observe/exporter"
)

// TelemetryConfig configures OpenTelemetry initialization.
type TelemetryConfig struct {
	ServiceName       string
	OTLPEndpoint      string
	PrometheusEnabled bool

	// PrometheusRequested distinguishes an operator who asked for metrics
	// from the default that turns them on for everybody.
	//
	// It exists because the exporter now ships as its own module, and the
	// two cases deserve opposite answers. An operator who wrote
	// metrics_path into their configuration and did not link the exporter
	// has a broken deployment, and startup says so. An application that
	// never mentioned metrics and is simply carrying the default must not
	// be stopped from booting by a module it never asked for — it logs an
	// INFO line naming the import, and runs.
	PrometheusRequested bool
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

	var promHandler http.Handler
	var built []exporter.Exporter

	if otlpEnabled {
		endpoint, insecure, perr := parseOTLPEndpoint(cfg.OTLPEndpoint)
		if perr != nil {
			return noop, nil, fmt.Errorf("observe.SetupOpenTelemetry parse endpoint: %w", perr)
		}
		exp, eerr := buildExporter(ctx, "otlp", exporter.Config{
			ServiceName: serviceName,
			Endpoint:    endpoint,
			Insecure:    insecure,
		})
		if eerr != nil {
			return noop, nil, eerr
		}
		built = append(built, exp)
		if exp.Spans != nil {
			traceProvider = trace.NewTracerProvider(
				trace.WithBatcher(exp.Spans),
				trace.WithResource(res),
			)
		}
		if exp.Reader != nil {
			meterOptions = append(meterOptions, metric.WithReader(exp.Reader))
		}
	}

	if cfg.PrometheusEnabled {
		exp, eerr := buildExporter(ctx, "prometheus", exporter.Config{ServiceName: serviceName})
		switch {
		case eerr == nil:
			built = append(built, exp)
			if exp.Reader != nil {
				meterOptions = append(meterOptions, metric.WithReader(exp.Reader))
			}
			promHandler = exp.Handler

		case !cfg.PrometheusRequested && errors.Is(eerr, errExporterNotLinked):
			// Nobody asked for this; the default did. An application that
			// never mentioned metrics must not be stopped from booting by a
			// module it never chose — and a scaffold that has done nothing
			// wrong must not boot with a WARN either, which is why this is
			// INFO: the state is expected, not degraded. The line still
			// names the fix, because an endpoint going quiet is the kind of
			// thing noticed at three in the morning. An operator who WROTE
			// metrics_path lands in the default branch below and startup
			// stops with the same recipe.
			logger.Info("metrics are not being served: the Prometheus exporter is not linked into this binary",
				"fix", "run `nucleus add prometheus`, or go get github.com/jcsvwinston/nucleus/exporters/prometheus and import it for its side effect",
				"why", "the exporter moved to its own module so applications that are never scraped stop carrying it",
			)

		default:
			return noop, nil, eerr
		}
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
		for _, exp := range built {
			if exp.Shutdown == nil {
				continue
			}
			if err := exp.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, fmt.Errorf("exporter shutdown: %w", err))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}

	return shutdownFn, promHandler, nil
}

// buildExporter resolves an exporter the configuration asked for, and turns
// its absence into the one message that helps: which module to import.
//
// Refusing to start is deliberate. The alternative — logging a warning and
// carrying on — produces an application that looks healthy and exports
// nothing, and the gap is noticed when someone goes looking for a trace that
// was never sent, usually during an incident.
// errExporterNotLinked marks the one failure the caller may choose to
// tolerate: the module is missing, as opposed to the exporter being present
// and failing to start.
var errExporterNotLinked = errors.New("exporter module not linked")

func buildExporter(ctx context.Context, name string, cfg exporter.Config) (exporter.Exporter, error) {
	factory, ok := exporter.Lookup(name)
	if !ok {
		linked := "none"
		if reg := exporter.Registered(); len(reg) > 0 {
			linked = strings.Join(reg, ", ")
		}
		if p, ours := knownproviders.TelemetryExporter(name); ours {
			return exporter.Exporter{}, fmt.Errorf("%w\n\nobserve: the configuration asks for the %s exporter, which ships as its own module and is not imported yet.\n\n"+
				"\tAdd it to your build:\n\n%s\n\n"+
				"\tOr let the CLI do it:\n\n\t\tnucleus add %s\n\n"+
				"\t(linked right now: %s)",
				errExporterNotLinked, p.Name, p.InstallHint(), p.Name, linked)
		}
		return exporter.Exporter{}, fmt.Errorf("%w: no exporter registered under %q (linked right now: %s)", errExporterNotLinked, name, linked)
	}
	exp, err := factory(ctx, cfg)
	if err != nil {
		return exporter.Exporter{}, fmt.Errorf("observe.SetupOpenTelemetry %s exporter: %w", name, err)
	}
	return exp, nil
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
