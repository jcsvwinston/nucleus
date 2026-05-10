package observability

import (
	"log/slog"
	"testing"
	"time"
)

// BenchmarkHasSubscribers_Idle measures the cost of the hot-path gate when
// no subscribers exist. Phase-2 design target: < 5 ns on x86. This is the
// primary benchmark gating the "zero cost when nobody is watching" property.
func BenchmarkHasSubscribers_Idle(b *testing.B) {
	bus := NewBus(slog.New(slog.DiscardHandler))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.HasSubscribers(KindHTTPRequest)
	}
}

// BenchmarkHasSubscribers_Active measures the same call with one
// subscription live. Should be the same cost (still one atomic load).
func BenchmarkHasSubscribers_Active(b *testing.B) {
	bus := NewBus(slog.New(slog.DiscardHandler))
	_, cancel := bus.Subscribe(Filter{}, nil)
	defer cancel()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.HasSubscribers(KindHTTPRequest)
	}
}

// BenchmarkEmit_NoSubscribers measures the cost of a complete Emit call
// when nobody is subscribed. This is the secondary "if a hook ignores
// HasSubscribers and emits anyway" cost. Should still be cheap because the
// activeCounts gate inside Emit short-circuits.
func BenchmarkEmit_NoSubscribers(b *testing.B) {
	bus := NewBus(slog.New(slog.DiscardHandler))
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := AcquireHTTPRequestEvent(now, "node-a")
		bus.Emit(e)
	}
}

// BenchmarkEmit_OneSubscriber measures the cost of a delivered Emit. The
// subscriber drains the channel in a separate goroutine. This is NOT the
// idle target — this is the "operator is watching" cost.
func BenchmarkEmit_OneSubscriber(b *testing.B) {
	bus := NewBus(slog.New(slog.DiscardHandler))
	sub, cancel := bus.Subscribe(Filter{}, &SubscribeOptions{ChannelSize: 1024})
	defer cancel()

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case ev, ok := <-sub.Ch():
				if !ok {
					return
				}
				ev.Release()
			case <-stop:
				return
			}
		}
	}()
	defer close(stop)

	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := AcquireHTTPRequestEvent(now, "node-a")
		bus.Emit(e)
	}
}

// BenchmarkAcquireRelease_NoEmit measures the bare cost of the sync.Pool
// round-trip without going through the bus. Helps interpret the deltas of
// the other benchmarks.
func BenchmarkAcquireRelease_NoEmit(b *testing.B) {
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := AcquireHTTPRequestEvent(now, "node-a")
		e.Release()
	}
}
