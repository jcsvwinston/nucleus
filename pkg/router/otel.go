package router

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	telemetryOnce sync.Once
	reqCounter    metric.Int64Counter
	reqDurationMs metric.Float64Histogram
	inFlight      metric.Int64UpDownCounter
)

// TelemetryMiddleware records OpenTelemetry spans and metrics for HTTP requests.
func TelemetryMiddleware(next http.Handler) http.Handler {
	initTelemetryInstruments()
	tracer := otel.Tracer("goframe/router")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		routeAttrs := []attribute.KeyValue{
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
		}

		if inFlight != nil {
			inFlight.Add(ctx, 1, metric.WithAttributes(routeAttrs...))
			defer inFlight.Add(ctx, -1, metric.WithAttributes(routeAttrs...))
		}

		next.ServeHTTP(ww, r.WithContext(ctx))

		durationMs := float64(time.Since(start).Nanoseconds()) / 1e6
		status := ww.Status()
		attrs := append(routeAttrs, attribute.Int("http.status_code", status))

		if reqCounter != nil {
			reqCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		if reqDurationMs != nil {
			reqDurationMs.Record(ctx, durationMs, metric.WithAttributes(attrs...))
		}

		span.SetAttributes(attrs...)
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	})
}

func initTelemetryInstruments() {
	telemetryOnce.Do(func() {
		m := otel.Meter("goframe/router")
		reqCounter, _ = m.Int64Counter("http.server.requests")
		reqDurationMs, _ = m.Float64Histogram("http.server.request.duration.ms")
		inFlight, _ = m.Int64UpDownCounter("http.server.in_flight")
	})
}
