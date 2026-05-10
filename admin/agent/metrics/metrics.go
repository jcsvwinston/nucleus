// Package metrics exposes the agent's Prometheus collectors and a small
// HTTP server that serves /metrics. The collectors live in their own
// Registry rather than the global default so multiple agents in the same
// test process do not collide.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds every collector the agent updates. Construct once via New
// and pass it down. Concurrency-safe by design (Prometheus collectors are).
type Metrics struct {
	registry *prometheus.Registry

	Connected         *prometheus.GaugeVec   // labels: endpoint
	ReconnectsTotal   prometheus.Counter
	EventsEmittedTotal *prometheus.CounterVec // labels: type
	EventsDroppedTotal *prometheus.CounterVec // labels: type, reason
	BufferSize         *prometheus.GaugeVec   // labels: type
	ActiveSubscriptions prometheus.Gauge
	HeartbeatsSent     prometheus.Counter
	StreamErrorsTotal  *prometheus.CounterVec // labels: stage
}

// New returns a fully wired Metrics with all collectors registered in a
// fresh registry.
func New() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		Connected: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "admin_agent_connected",
			Help: "1 if the agent is currently connected to the admin server endpoint, else 0.",
		}, []string{"endpoint"}),
		ReconnectsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "admin_agent_reconnects_total",
			Help: "Number of times the agent has re-established the bidi stream.",
		}),
		EventsEmittedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "admin_agent_events_emitted_total",
			Help: "Number of events successfully forwarded to the admin server, by event type.",
		}, []string{"type"}),
		EventsDroppedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "admin_agent_events_dropped_total",
			Help: "Number of events dropped by the agent before reaching the admin server, by type and reason.",
		}, []string{"type", "reason"}),
		BufferSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "admin_agent_buffer_size",
			Help: "Current number of events stored in the agent's per-type ring buffer.",
		}, []string{"type"}),
		ActiveSubscriptions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "admin_agent_active_subscriptions",
			Help: "Current number of subscriptions the admin server has open against this agent.",
		}),
		HeartbeatsSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "admin_agent_heartbeats_sent_total",
			Help: "Total heartbeats the agent has sent to the admin server.",
		}),
		StreamErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "admin_agent_stream_errors_total",
			Help: "Stream-level errors observed by the agent, grouped by stage.",
		}, []string{"stage"}),
	}

	registry.MustRegister(
		m.Connected,
		m.ReconnectsTotal,
		m.EventsEmittedTotal,
		m.EventsDroppedTotal,
		m.BufferSize,
		m.ActiveSubscriptions,
		m.HeartbeatsSent,
		m.StreamErrorsTotal,
	)

	return m
}

// Registry returns the underlying Prometheus registry. Useful for tests
// and for callers that want to expose admin-agent metrics from their own
// HTTP server (the framework's main /metrics, for example) instead of
// running the agent's standalone server.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// Handler returns a stand-alone /metrics http.Handler. Use Server to mount
// it on a private port; use Handler directly to embed it in another mux.
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Server is a tiny HTTP server that serves /metrics and /healthz.
type Server struct {
	addr   string
	logger *slog.Logger
	srv    *http.Server
}

// NewServer constructs a metrics server. addr is a [host]:port string
// (e.g. ":9101", "127.0.0.1:9101"). Empty addr disables the server (Run
// returns immediately).
func NewServer(addr string, m *Metrics, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return &Server{addr: "", logger: logger}
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return &Server{
		addr:   addr,
		logger: logger,
		srv: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

// Run starts the server and blocks until ctx is cancelled. It returns nil
// on graceful shutdown, non-nil on listen errors. Safe to call when the
// server has no addr — then it is a no-op that just waits on ctx.
func (s *Server) Run(ctx context.Context) error {
	if s == nil || s.addr == "" || s.srv == nil {
		<-ctx.Done()
		return nil
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("admin agent metrics: listen %s: %w", s.addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		err := s.srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}
