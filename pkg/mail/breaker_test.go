package mail

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/circuit"
)

// stubSender is a Sender that returns a configurable error and counts
// invocations. Used by the breaker tests.
type stubSender struct {
	err   error
	calls int
}

func (s *stubSender) Send(_ context.Context, _ Message) error {
	s.calls++
	return s.err
}

// stubSenderWithHealth mirrors stubSender but also implements
// HealthChecker so we can verify the wrapper preserves the interface.
type stubSenderWithHealth struct {
	stubSender
	healthErr      error
	healthCalls    int
	healthCallTime time.Time
}

func (s *stubSenderWithHealth) Healthy(_ context.Context) error {
	s.healthCalls++
	s.healthCallTime = time.Now()
	return s.healthErr
}

func validMessage() Message {
	return Message{
		From:    "from@example.com",
		To:      []string{"to@example.com"},
		Subject: "test",
		Body:    "body",
	}
}

func TestWrapWithBreaker_PassesThroughOnSuccess(t *testing.T) {
	inner := &stubSender{}
	w := wrapWithBreaker(inner, CircuitBreakerConfig{FailureThreshold: 2})

	if err := w.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected 1 call, got %d", inner.calls)
	}
}

func TestWrapWithBreaker_TripsAfterFailureThreshold(t *testing.T) {
	innerErr := errors.New("smtp boom")
	inner := &stubSender{err: innerErr}
	w := wrapWithBreaker(inner, CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         time.Hour,
	})

	// 2 failures trip the breaker.
	if err := w.Send(context.Background(), validMessage()); !errors.Is(err, innerErr) {
		t.Fatalf("first call: want innerErr, got %v", err)
	}
	if err := w.Send(context.Background(), validMessage()); !errors.Is(err, innerErr) {
		t.Fatalf("second call: want innerErr, got %v", err)
	}

	// Third call short-circuits with ErrOpen — inner is NOT invoked.
	err := w.Send(context.Background(), validMessage())
	if !errors.Is(err, circuit.ErrOpen) {
		t.Fatalf("third call: want ErrOpen, got %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("expected inner to be called 2x (then short-circuited), got %d", inner.calls)
	}
}

func TestWrapWithBreaker_PreservesHealthChecker(t *testing.T) {
	// Sanity: a sender without HealthChecker should NOT satisfy it
	// after wrapping.
	noHC := &stubSender{}
	w := wrapWithBreaker(noHC, CircuitBreakerConfig{Enabled: true})
	if _, ok := w.(HealthChecker); ok {
		t.Fatalf("wrapper around non-HealthChecker should not implement HealthChecker")
	}

	// And a sender that does implement HealthChecker should keep
	// satisfying it after wrapping.
	withHC := &stubSenderWithHealth{}
	w2 := wrapWithBreaker(withHC, CircuitBreakerConfig{Enabled: true})
	hc, ok := w2.(HealthChecker)
	if !ok {
		t.Fatalf("wrapper around HealthChecker must preserve the interface")
	}
	if err := hc.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy returned %v, want nil", err)
	}
	if withHC.healthCalls != 1 {
		t.Fatalf("expected 1 health call, got %d", withHC.healthCalls)
	}
}

func TestWrapWithBreaker_HealthyBypassesBreaker(t *testing.T) {
	innerErr := errors.New("smtp boom")
	inner := &stubSenderWithHealth{
		stubSender: stubSender{err: innerErr},
		healthErr:  nil,
	}
	w := wrapWithBreaker(inner, CircuitBreakerConfig{
		FailureThreshold: 1,
		Cooldown:         time.Hour,
	})

	// One Send failure trips the breaker.
	if err := w.Send(context.Background(), validMessage()); !errors.Is(err, innerErr) {
		t.Fatalf("first send: want innerErr, got %v", err)
	}
	if err := w.Send(context.Background(), validMessage()); !errors.Is(err, circuit.ErrOpen) {
		t.Fatalf("second send: want ErrOpen, got %v", err)
	}

	// Healthy must still pass through and reach the underlying sender —
	// /healthz needs to be able to observe a recovering dependency even
	// while Send is short-circuited.
	hc := w.(HealthChecker)
	if err := hc.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy returned %v, want nil (breaker must not gate Healthy)", err)
	}
	if inner.healthCalls != 1 {
		t.Fatalf("expected 1 health call (bypass), got %d", inner.healthCalls)
	}
}

func TestNewSender_BreakerWrapsRegisteredProvider(t *testing.T) {
	// Register a stub provider so NewSender exercises the wrap path
	// without needing a real SMTP server.
	driver := "breaker-stub-success"
	defer unregisterProviderForTest(driver)
	failingDriver := "breaker-stub-failing"
	defer unregisterProviderForTest(failingDriver)

	innerErr := errors.New("provider failure")
	if err := RegisterProvider(driver, func(_ Config) (Sender, error) {
		return &stubSender{}, nil
	}); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := RegisterProvider(failingDriver, func(_ Config) (Sender, error) {
		return &stubSender{err: innerErr}, nil
	}); err != nil {
		t.Fatalf("RegisterProvider failing: %v", err)
	}

	// Enabled → breaker wraps.
	s, err := NewSender(Config{
		Driver: failingDriver,
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 1,
			Cooldown:         time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := s.Send(context.Background(), validMessage()); !errors.Is(err, innerErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
	if err := s.Send(context.Background(), validMessage()); !errors.Is(err, circuit.ErrOpen) {
		t.Fatalf("expected ErrOpen after trip, got %v", err)
	}

	// Disabled → no wrapping; same sender keeps returning innerErr without tripping.
	s2, err := NewSender(Config{
		Driver: failingDriver,
		// CircuitBreaker zero value → disabled.
	})
	if err != nil {
		t.Fatalf("NewSender disabled: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := s2.Send(context.Background(), validMessage()); !errors.Is(err, innerErr) {
			t.Fatalf("call %d: want innerErr, got %v", i, err)
		}
	}
}

func TestNewSender_BreakerSkipsNoopDriver(t *testing.T) {
	s, err := NewSender(Config{
		Driver: "noop",
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 1,
		},
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	// noop is never wrapped — confirm by checking the concrete type
	// would have been our wrapper if it were wrapped.
	if _, isBreaker := s.(*breakerSender); isBreaker {
		t.Fatalf("noop driver should not be wrapped with breaker")
	}
	if _, isBreaker := s.(*breakerSenderWithHealth); isBreaker {
		t.Fatalf("noop driver should not be wrapped with breaker (with health)")
	}
}

// unregisterProviderForTest removes a registered provider so tests
// don't leak state across runs. mail.go intentionally doesn't expose
// this — the test helper lives here.
func unregisterProviderForTest(name string) {
	providersMu.Lock()
	defer providersMu.Unlock()
	delete(providers, name)
}
