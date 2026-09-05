package router

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"
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
	tracer := otel.Tracer("nucleus/router")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer))
		if traceID := span.SpanContext().TraceID(); traceID.IsValid() {
			ctx = observe.CtxWithTraceID(ctx, traceID.String())
		}
		defer span.End()

		ww := NewWrapResponseWriter(w, r.ProtoMajor)
		// In flight is counted by method only: the route is not known until
		// the mux has matched, and an attribute that changes between Add(+1)
		// and Add(-1) never balances.
		methodAttrs := []attribute.KeyValue{attribute.String("http.method", r.Method)}
		if inFlight != nil {
			inFlight.Add(ctx, 1, metric.WithAttributes(methodAttrs...))
			defer inFlight.Add(ctx, -1, metric.WithAttributes(methodAttrs...))
		}

		// The route is the pattern that matched, not the URL path (NU-30):
		// the mux records it into this holder when it dispatches, and the
		// holder travels in the context, so the copies of *http.Request
		// that http.TimeoutHandler and WithContext make on the way down
		// do not lose it. It is read AFTER the handler.
		rh := &routeHolder{}
		ctx = context.WithValue(ctx, routeCtxKey{}, rh)
		next.ServeHTTP(ww, r.WithContext(ctx))

		route := rh.route()
		span.SetName(r.Method + " " + route)
		durationMs := float64(time.Since(start).Nanoseconds()) / 1e6
		status := ww.Status()
		attrs := []attribute.KeyValue{
			attribute.String("http.method", r.Method),
			attribute.String("http.route", route),
			attribute.Int("http.status_code", status),
		}

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

// routeHolder receives the matched route template from the mux. The raw
// URL path used to be the http.route value, which put every id and every
// probe of a scanner into the metric label set (unbounded cardinality); an
// unmatched request now reports one shared value.
type routeHolder struct {
	prefix  string // accumulated by Mount as the request descends
	pattern string // set by the route that finally served it
}

type routeCtxKey struct{}

func (h *routeHolder) route() string {
	if h == nil || strings.TrimSpace(h.pattern) == "" {
		return "unmatched"
	}
	return h.pattern
}

// RouteFromContext returns the route template ("/users/{id}") the mux
// matched for this request, or "" when nothing has matched yet — inside a
// handler it is always set. It is what the telemetry reports as http.route.
func RouteFromContext(ctx context.Context) string {
	if h, ok := ctx.Value(routeCtxKey{}).(*routeHolder); ok {
		return h.pattern
	}
	return ""
}

func initTelemetryInstruments() {
	telemetryOnce.Do(func() {
		m := otel.Meter("nucleus/router")
		reqCounter, _ = m.Int64Counter("http.server.requests")
		reqDurationMs, _ = m.Float64Histogram("http.server.request.duration.ms")
		inFlight, _ = m.Int64UpDownCounter("http.server.in_flight")
	})
}
