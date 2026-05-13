// Package circuit provides a small circuit-breaker primitive for
// wrapping calls to external dependencies (mail, storage, plugin
// bridges, third-party APIs) in a deterministic failure-isolation
// pattern. The design follows the standard closed / open / half-open
// state machine: the breaker passes calls through while closed, fails
// fast while open, and admits a small number of probe calls while
// half-open to test whether the dependency has recovered.
//
// pkg/app automatically wires circuit breakers for mail.Sender.Send
// and remote storage.Store operations (Put, Get, Delete, Exists, List,
// Copy, SignedURL). Operators only need to set
// circuit_breaker.enabled=false (or tune thresholds via
// mail_circuit_breaker.* / storage.circuit_breaker.* in nucleus.yml)
// to opt out or adjust behavior. Additional external calls — plugin
// bridges, third-party APIs — can be wrapped manually using New.
//
// The package is intentionally minimal — no event bus, no metrics
// surface, no per-call timeout. Callers compose those with whatever
// instrumentation they already use (pkg/observe for logging, the
// /metrics MeterProvider for counters, etc.).
package circuit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State enumerates the circuit-breaker states.
type State int

const (
	// StateClosed is the normal pass-through state.
	StateClosed State = iota
	// StateOpen short-circuits every call with ErrOpen until the
	// cooldown elapses.
	StateOpen
	// StateHalfOpen lets a bounded number of probe calls through.
	// A success transitions back to closed; any failure re-opens.
	StateHalfOpen
)

// String returns a human-readable state label suitable for logging.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrOpen is returned by Do when the breaker is open (and the cooldown
// has not yet elapsed) or when the half-open probe budget is exhausted.
var ErrOpen = errors.New("circuit breaker is open")

// Config configures a Breaker. Zero values fall back to conservative
// defaults: a single failure trips the breaker, cooldown is 30s, and
// the half-open probe budget is 1.
type Config struct {
	// FailureThreshold is the number of consecutive failures in the
	// closed state required to trip the breaker open. Must be >= 1;
	// non-positive values default to 1.
	FailureThreshold int

	// Cooldown is the duration the breaker stays open before admitting
	// half-open probes. Non-positive values default to 30 seconds.
	Cooldown time.Duration

	// HalfOpenMaxConcurrent is the maximum number of in-flight probe
	// calls allowed in the half-open state. Non-positive values default
	// to 1 (single-shot probe).
	HalfOpenMaxConcurrent int

	// Now is a time source override used by tests. Production callers
	// should leave it nil; the breaker uses time.Now() by default.
	Now func() time.Time
}

// Breaker is a circuit breaker. The zero value is not usable — use New.
type Breaker struct {
	mu               sync.Mutex
	state            State
	failureCount     int
	openedAt         time.Time
	halfOpenInFlight int

	failureThreshold      int
	cooldown              time.Duration
	halfOpenMaxConcurrent int
	now                   func() time.Time
}

// New constructs a Breaker from a Config. Zero/negative numeric fields
// fall back to safe defaults documented on Config.
func New(cfg Config) *Breaker {
	threshold := cfg.FailureThreshold
	if threshold < 1 {
		threshold = 1
	}
	cooldown := cfg.Cooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	halfOpen := cfg.HalfOpenMaxConcurrent
	if halfOpen < 1 {
		halfOpen = 1
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Breaker{
		state:                 StateClosed,
		failureThreshold:      threshold,
		cooldown:              cooldown,
		halfOpenMaxConcurrent: halfOpen,
		now:                   now,
	}
}

// State returns the current breaker state. Useful for instrumentation
// and tests; production code typically does not need to inspect this
// — Do enforces the state machine internally.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Do executes fn while respecting the breaker state.
//
// When the breaker is closed, fn runs and its error (or success)
// drives the state machine. When the breaker is open and the cooldown
// has not elapsed, ErrOpen is returned without invoking fn. When the
// cooldown has elapsed, the breaker transitions to half-open and
// admits up to HalfOpenMaxConcurrent calls; a success resets to
// closed, any failure (or exceeded budget) returns to open with a
// fresh cooldown.
//
// The context is forwarded to fn and is not inspected by the breaker
// itself — context cancellation is the caller's responsibility within
// fn.
func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := b.beforeCall(); err != nil {
		return err
	}

	err := fn(ctx)
	b.afterCall(err == nil)
	return err
}

func (b *Breaker) beforeCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return nil

	case StateOpen:
		if b.now().Sub(b.openedAt) < b.cooldown {
			return ErrOpen
		}
		// Cooldown elapsed — transition to half-open and admit this
		// call as the first probe.
		b.state = StateHalfOpen
		b.halfOpenInFlight = 1
		return nil

	case StateHalfOpen:
		if b.halfOpenInFlight >= b.halfOpenMaxConcurrent {
			return ErrOpen
		}
		b.halfOpenInFlight++
		return nil
	}
	return nil
}

func (b *Breaker) afterCall(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		if !success {
			b.failureCount++
			if b.failureCount >= b.failureThreshold {
				b.trip()
			}
		} else {
			b.failureCount = 0
		}

	case StateHalfOpen:
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		if !success {
			b.trip()
			return
		}
		// Single successful probe is enough to close the breaker.
		// Callers that want N consecutive successes can model that by
		// raising HalfOpenMaxConcurrent and gating externally.
		b.state = StateClosed
		b.failureCount = 0
	}
}

// trip moves the breaker to the open state with a fresh cooldown.
// Caller must hold b.mu.
func (b *Breaker) trip() {
	b.state = StateOpen
	b.failureCount = 0
	b.halfOpenInFlight = 0
	b.openedAt = b.now()
}
