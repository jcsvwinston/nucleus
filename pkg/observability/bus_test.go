package observability

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain wraps every test in this package with a goroutine-leak check.
// The bus does not spawn goroutines, so the leak set should always be
// empty after a test returns.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func newTestBus(t *testing.T) *Bus {
	t.Helper()
	return NewBus(slog.New(slog.DiscardHandler))
}

// TestBus_HasSubscribers_NoSubscribers verifies the hot-path gate returns
// false when there are no subscribers, for every kind.
func TestBus_HasSubscribers_NoSubscribers(t *testing.T) {
	bus := newTestBus(t)

	for _, k := range []EventKind{
		KindHTTPRequest,
		KindSQLStatement,
		KindSessionChange,
		KindCustom,
	} {
		if bus.HasSubscribers(k) {
			t.Fatalf("HasSubscribers(%s) = true on empty bus", k)
		}
	}
}

// TestBus_Emit_NoSubscribers_ReleasesEvent verifies that Emit releases the
// producer's reference when nobody is subscribed (so the event is reusable).
func TestBus_Emit_NoSubscribers_ReleasesEvent(t *testing.T) {
	bus := newTestBus(t)

	e := AcquireHTTPRequestEvent(time.Now(), "node-a")
	e.Method = "GET"
	e.Path = "/healthz"

	if got := e.refsLoad(); got != 1 {
		t.Fatalf("fresh event refs = %d, want 1", got)
	}
	bus.Emit(e)
	if got := e.refsLoad(); got != 0 {
		t.Fatalf("after Emit with no subscribers refs = %d, want 0", got)
	}
	if bus.Stats(KindHTTPRequest).Emitted != 1 {
		t.Fatalf("emitted count not bumped")
	}
}

// TestBus_Subscribe_DeliversEvent verifies a single matching subscriber
// receives the event and that releasing it returns the event to the pool.
func TestBus_Subscribe_DeliversEvent(t *testing.T) {
	bus := newTestBus(t)

	sub, cancel := bus.Subscribe(Filter{Kinds: []EventKind{KindHTTPRequest}}, nil)
	defer cancel()

	e := AcquireHTTPRequestEvent(time.Now(), "node-a")
	e.Method = "GET"
	e.Path = "/api/things"
	e.Status = 200
	bus.Emit(e)

	select {
	case got := <-sub.Ch():
		if got.Kind() != KindHTTPRequest {
			t.Fatalf("kind = %s, want http_request", got.Kind())
		}
		http, ok := got.(*HTTPRequestEvent)
		if !ok {
			t.Fatalf("got %T, want *HTTPRequestEvent", got)
		}
		if http.Method != "GET" || http.Path != "/api/things" || http.Status != 200 {
			t.Fatalf("unexpected fields: %+v", http)
		}
		if r := http.refsLoad(); r < 1 {
			t.Fatalf("refs = %d, want >=1 (consumer holds the only ref)", r)
		}
		http.Release()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for delivery")
	}
}

// TestBus_FilterByKind_NarrowsDelivery verifies that a kind-narrowed filter
// drops irrelevant events without ever waking the consumer.
func TestBus_FilterByKind_NarrowsDelivery(t *testing.T) {
	bus := newTestBus(t)

	sub, cancel := bus.Subscribe(Filter{Kinds: []EventKind{KindSQLStatement}}, nil)
	defer cancel()

	httpEv := AcquireHTTPRequestEvent(time.Now(), "node-a")
	bus.Emit(httpEv)

	sqlEv := AcquireSQLStatementEvent(time.Now(), "node-a")
	sqlEv.Query = "SELECT 1"
	bus.Emit(sqlEv)

	select {
	case got := <-sub.Ch():
		if got.Kind() != KindSQLStatement {
			t.Fatalf("got %s, want sql_statement", got.Kind())
		}
		got.Release()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for sql delivery")
	}

	// No more events should be queued.
	select {
	case got := <-sub.Ch():
		got.Release()
		t.Fatalf("unexpected delivery: %s", got.Kind())
	case <-time.After(50 * time.Millisecond):
	}
}

// TestBus_FilterByNode_NarrowsDelivery verifies the NodeID dimension of
// the filter.
func TestBus_FilterByNode_NarrowsDelivery(t *testing.T) {
	bus := newTestBus(t)

	sub, cancel := bus.Subscribe(Filter{NodeIDs: []string{"node-a"}}, nil)
	defer cancel()

	bSide := AcquireHTTPRequestEvent(time.Now(), "node-b")
	bus.Emit(bSide)

	aSide := AcquireHTTPRequestEvent(time.Now(), "node-a")
	bus.Emit(aSide)

	select {
	case got := <-sub.Ch():
		if got.NodeID() != "node-a" {
			t.Fatalf("got node %s, want node-a", got.NodeID())
		}
		got.Release()
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// TestBus_FullChannel_DropsAndReleases verifies the drop-newest behaviour
// when a subscriber falls behind, and that the dropped event is returned
// to the pool (refs drop to zero).
func TestBus_FullChannel_DropsAndReleases(t *testing.T) {
	bus := newTestBus(t)

	// Channel of size 1; we never read from it.
	sub, cancel := bus.Subscribe(Filter{}, &SubscribeOptions{ChannelSize: 1})
	defer cancel()

	// First emit fills the channel.
	first := AcquireHTTPRequestEvent(time.Now(), "node-a")
	bus.Emit(first)

	// Second emit must drop. We track its refs before/after.
	second := AcquireHTTPRequestEvent(time.Now(), "node-a")
	bus.Emit(second)

	// Give the bus a moment (Emit is synchronous, but be defensive).
	time.Sleep(10 * time.Millisecond)

	if got := second.refsLoad(); got != 0 {
		t.Fatalf("dropped event refs = %d, want 0 (returned to pool)", got)
	}
	stats := bus.Stats(KindHTTPRequest)
	if stats.Dropped != 1 {
		t.Fatalf("dropped = %d, want 1", stats.Dropped)
	}
	if stats.Emitted != 2 {
		t.Fatalf("emitted = %d, want 2", stats.Emitted)
	}

	// Drain the queued event so we don't end the test holding pool refs.
	// Done synchronously; spawning a drainer goroutine would leak (cancel
	// does not close the channel — see Subscription.Cancel doc).
	select {
	case ev := <-sub.Ch():
		ev.Release()
	default:
		t.Fatal("expected the first emit to be queued")
	}
}

// TestBus_FanOutRefcount checks that with N subscribers, the bus increments
// refs N times and that the original ref is released.
func TestBus_FanOutRefcount(t *testing.T) {
	bus := newTestBus(t)

	const subs = 5
	subscriptions := make([]*Subscription, subs)
	cancels := make([]func(), subs)
	for i := 0; i < subs; i++ {
		subscriptions[i], cancels[i] = bus.Subscribe(Filter{}, nil)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	e := AcquireHTTPRequestEvent(time.Now(), "node-a")
	bus.Emit(e)

	// Each subscriber should hold one reference. The original was released
	// by the bus. Total live refs = subs.
	if got := e.refsLoad(); got != int32(subs) {
		t.Fatalf("after fan-out refs = %d, want %d", got, subs)
	}

	// Drain every subscription and Release. Refs should hit zero.
	var wg sync.WaitGroup
	for i := 0; i < subs; i++ {
		wg.Add(1)
		go func(s *Subscription) {
			defer wg.Done()
			ev := <-s.Ch()
			ev.Release()
		}(subscriptions[i])
	}
	wg.Wait()

	if got := e.refsLoad(); got != 0 {
		t.Fatalf("after all releases refs = %d, want 0", got)
	}
}

// TestBus_Cancel_RemovesSubscriberAndDecrementsCounters verifies cancel is
// idempotent and that HasSubscribers reflects the change.
func TestBus_Cancel_RemovesSubscriberAndDecrementsCounters(t *testing.T) {
	bus := newTestBus(t)

	sub, cancel := bus.Subscribe(Filter{Kinds: []EventKind{KindHTTPRequest}}, nil)

	if !bus.HasSubscribers(KindHTTPRequest) {
		t.Fatal("expected HTTP subscribers after Subscribe")
	}
	if bus.HasSubscribers(KindSQLStatement) {
		t.Fatal("did not expect SQL subscribers (filter narrowed to HTTP)")
	}

	cancel()
	cancel() // idempotent

	if bus.HasSubscribers(KindHTTPRequest) {
		t.Fatal("HasSubscribers true after cancel")
	}
	if bus.SubscriberCount() != 0 {
		t.Fatalf("count = %d, want 0", bus.SubscriberCount())
	}

	// Use the local sub var so the linter is happy.
	if sub == nil {
		t.Fatal("nil sub")
	}
}

// TestBus_NilSafe verifies that the nil bus is safe — necessary because
// the framework constructs the agent path even when observability is off.
func TestBus_NilSafe(t *testing.T) {
	var bus *Bus

	if bus.HasSubscribers(KindHTTPRequest) {
		t.Fatal("nil bus reports subscribers")
	}
	bus.Emit(AcquireHTTPRequestEvent(time.Now(), "n")) // must not panic

	sub, cancel := bus.Subscribe(Filter{}, nil)
	defer cancel()

	// The returned sub from a nil bus is a closed channel. Reading must
	// return immediately with the zero value.
	select {
	case _, ok := <-sub.Ch():
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timeout reading from nil-bus subscription")
	}
}

// TestBus_ConcurrentEmit_RaceFree exercises the bus under load to surface
// any data race when run with `go test -race`.
func TestBus_ConcurrentEmit_RaceFree(t *testing.T) {
	bus := newTestBus(t)

	const subs = 4
	subscriptions := make([]*Subscription, subs)
	cancels := make([]func(), subs)
	for i := 0; i < subs; i++ {
		subscriptions[i], cancels[i] = bus.Subscribe(Filter{}, &SubscribeOptions{ChannelSize: 64})
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	// Drain every subscription.
	var drained atomic.Int64
	var drainWG sync.WaitGroup
	for _, s := range subscriptions {
		drainWG.Add(1)
		go func(s *Subscription) {
			defer drainWG.Done()
			for ev := range s.Ch() {
				drained.Add(1)
				ev.Release()
			}
		}(s)
	}

	const producers = 8
	const perProducer = 250
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				e := AcquireHTTPRequestEvent(time.Now(), "node-a")
				e.Method = "GET"
				bus.Emit(e)
			}
		}()
	}
	wg.Wait()

	// Cancel everyone; this stops Bus from finding them but does not close
	// channels (by design, see doc.go). To unblock our drainers we need to
	// close channels manually here for the test.
	for _, c := range cancels {
		c()
	}
	for _, s := range subscriptions {
		// Closing here is safe because we are in the test and we know no
		// emits remain in flight (wg.Wait above). DO NOT do this in
		// production code.
		close(s.ch)
	}
	drainWG.Wait()

	got := bus.Stats(KindHTTPRequest)
	if got.Emitted != producers*perProducer {
		t.Fatalf("emitted = %d, want %d", got.Emitted, producers*perProducer)
	}
}
