package mail

import (
	"context"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/circuit"
)

// CircuitBreakerConfig configures the optional circuit breaker that
// wraps mail sender Send calls. Zero values are not used directly;
// pkg/app applies framework defaults before constructing the breaker.
//
// The breaker is wrapped around Send only. Healthy (when the underlying
// sender implements HealthChecker) bypasses the breaker so /healthz can
// observe a recovering dependency even while Send is short-circuited.
type CircuitBreakerConfig struct {
	// Enabled turns on circuit-breaker wrapping around Send.
	Enabled bool `koanf:"enabled"`

	// FailureThreshold is the number of consecutive Send failures
	// required to trip the breaker open. Non-positive falls back to
	// pkg/circuit's default (1).
	FailureThreshold int `koanf:"failure_threshold"`

	// Cooldown is the duration the breaker stays open before admitting
	// half-open probes. Non-positive falls back to pkg/circuit's
	// default (30s).
	Cooldown time.Duration `koanf:"cooldown"`

	// HalfOpenMaxConcurrent caps in-flight probes in the half-open
	// state. Non-positive falls back to pkg/circuit's default (1).
	HalfOpenMaxConcurrent int `koanf:"half_open_max_concurrent"`
}

// wrapWithBreaker decorates a Sender with a circuit breaker. When the
// underlying sender also implements HealthChecker, the returned value
// preserves that interface so /healthz probing still type-asserts.
// Healthy is forwarded without going through the breaker — probes
// must remain independent of the breaker state.
func wrapWithBreaker(inner Sender, cfg CircuitBreakerConfig) Sender {
	br := circuit.New(circuit.Config{
		FailureThreshold:      cfg.FailureThreshold,
		Cooldown:              cfg.Cooldown,
		HalfOpenMaxConcurrent: cfg.HalfOpenMaxConcurrent,
	})
	base := breakerSender{inner: inner, breaker: br}
	if hc, ok := inner.(HealthChecker); ok {
		return &breakerSenderWithHealth{breakerSender: base, hc: hc}
	}
	return &base
}

type breakerSender struct {
	inner   Sender
	breaker *circuit.Breaker
}

func (b *breakerSender) Send(ctx context.Context, msg Message) error {
	return b.breaker.Do(ctx, func(ctx context.Context) error {
		return b.inner.Send(ctx, msg)
	})
}

type breakerSenderWithHealth struct {
	breakerSender
	hc HealthChecker
}

func (b *breakerSenderWithHealth) Healthy(ctx context.Context) error {
	return b.hc.Healthy(ctx)
}
