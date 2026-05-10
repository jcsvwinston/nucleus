package observability

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// DefaultSubscriberChannelSize is the default per-subscription channel
// capacity. Subscribers that fall behind by more than this many events see
// their excess events dropped (counted in Bus.Stats(kind).Dropped).
const DefaultSubscriberChannelSize = 256

// Bus is the in-process observability fan-out. It is safe for concurrent
// use and never spawns goroutines. See doc.go for the full ownership and
// threading model.
type Bus struct {
	logger *slog.Logger

	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]*Subscription

	// Per-kind atomic counters. Indexed by EventKind value.
	activeCounts  [numEventKinds]atomic.Int64
	emittedCount  [numEventKinds]atomic.Uint64
	droppedCount  [numEventKinds]atomic.Uint64
}

// NewBus returns a fresh Bus. The logger is used only for unexpected
// non-fatal conditions (a Subscribe with a closed bus, etc.). Pass nil to
// silence; the bus will fall back to slog.Default on the rare occasions it
// logs.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		logger:      logger,
		subscribers: make(map[uint64]*Subscription),
	}
}

// HasSubscribers returns true if at least one subscription matches events of
// the given kind. This is the hot-path gate that hooks call BEFORE
// constructing an event. It is one atomic load on the read side and is
// designed to be inlined by the compiler.
//
// HasSubscribers is conservative: it answers "would this kind have any
// subscriber" without applying NodeID filters. That is fine — false
// positives only cost the producer the construction of an event that the
// bus then drops cheaply via Filter.Matches.
func (b *Bus) HasSubscribers(kind EventKind) bool {
	if b == nil {
		return false
	}
	if int(kind) >= numEventKinds {
		return false
	}
	return b.activeCounts[kind].Load() > 0
}

// Emit publishes the event to every matching subscriber. Ownership of e
// transfers to the bus on entry; the caller MUST NOT touch the event after
// Emit returns. See doc.go for the full lifecycle.
//
// Emit is safe to call from any goroutine.
func (b *Bus) Emit(e Event) {
	if b == nil || e == nil {
		return
	}

	kind := e.Kind()
	if int(kind) >= numEventKinds {
		// Defensive: an event with an unknown kind cannot be routed.
		// Release and drop it.
		e.Release()
		return
	}

	b.emittedCount[kind].Add(1)

	// Fast path: no subscribers for this kind. Release the producer's
	// reference and return.
	if b.activeCounts[kind].Load() == 0 {
		e.Release()
		return
	}

	// Snapshot subscribers under RLock. The select-default sends are bounded
	// (non-blocking) so holding RLock for the duration of the fan-out is
	// acceptable; we never block on a slow consumer.
	b.mu.RLock()
	matched := make([]*Subscription, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		if !sub.filter.Matches(e) {
			continue
		}
		matched = append(matched, sub)
	}
	b.mu.RUnlock()

	if len(matched) == 0 {
		// No NodeID/Kind match after filtering. Release and exit.
		e.Release()
		return
	}

	// Pre-acquire one ref per delivery target. Each successful send transfers
	// that ref to the consumer; each drop is balanced by an immediate
	// Release here.
	for range matched {
		e.acquireRef()
	}

	for _, sub := range matched {
		select {
		case sub.ch <- e:
			// Delivered. The consumer now owns one reference.
		default:
			// Channel full; drop this delivery for this subscriber.
			b.droppedCount[kind].Add(1)
			e.Release()
		}
	}

	// Balance the original reference the producer transferred to us.
	e.Release()
}

// Subscribe returns a new Subscription. The caller drains sub.Ch() from a
// dedicated goroutine and calls sub.Cancel() (or its returned cancel
// function) exactly once when done.
//
// SubscribeOptions tune buffer size and other knobs. Pass nil for defaults.
func (b *Bus) Subscribe(filter Filter, opts *SubscribeOptions) (*Subscription, func()) {
	if b == nil {
		// Return a closed subscription so callers don't have to nil-check.
		closed := make(chan Event)
		close(closed)
		return &Subscription{ch: closed, filter: filter, dead: true}, func() {}
	}

	channelSize := DefaultSubscriberChannelSize
	if opts != nil && opts.ChannelSize > 0 {
		channelSize = opts.ChannelSize
	}

	sub := &Subscription{
		bus:    b,
		filter: filter,
		ch:     make(chan Event, channelSize),
	}

	b.mu.Lock()
	b.nextID++
	sub.id = b.nextID
	b.subscribers[sub.id] = sub
	b.mu.Unlock()

	// Bump per-kind counters. If Filter.Kinds is empty, every kind is live.
	if len(filter.Kinds) == 0 {
		for k := EventKind(1); int(k) < numEventKinds; k++ {
			b.activeCounts[k].Add(1)
		}
	} else {
		for _, k := range filter.Kinds {
			if int(k) < numEventKinds {
				b.activeCounts[k].Add(1)
			}
		}
	}

	return sub, sub.Cancel
}

// SubscribeOptions tunes how a subscription buffers and behaves under
// pressure. Zero value is safe (means defaults).
type SubscribeOptions struct {
	// ChannelSize is the per-subscriber channel capacity. Larger buffers
	// tolerate slower consumers but trade memory and worst-case latency.
	// Default: DefaultSubscriberChannelSize.
	ChannelSize int
}

// Stats summarizes runtime counters for a single kind.
type Stats struct {
	Subscribers int
	Emitted     uint64
	Dropped     uint64
}

// Stats returns a point-in-time snapshot of counters for the given kind.
func (b *Bus) Stats(kind EventKind) Stats {
	if b == nil || int(kind) >= numEventKinds {
		return Stats{}
	}
	return Stats{
		Subscribers: int(b.activeCounts[kind].Load()),
		Emitted:     b.emittedCount[kind].Load(),
		Dropped:     b.droppedCount[kind].Load(),
	}
}

// SubscriberCount returns the total number of live subscriptions across all
// kinds. Use Stats(kind) for per-kind counts.
func (b *Bus) SubscriberCount() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	n := len(b.subscribers)
	b.mu.RUnlock()
	return n
}

// Subscription is a handle to a live subscription. It carries the channel
// the consumer drains and a Cancel that removes the subscription from the
// bus. Cancel is safe to call multiple times; subsequent calls are no-ops.
type Subscription struct {
	bus    *Bus
	id     uint64
	filter Filter
	ch     chan Event

	cancelOnce sync.Once
	dead       bool
}

// Ch returns the channel the subscriber drains. Each event read from Ch
// MUST be Released by the consumer exactly once.
func (s *Subscription) Ch() <-chan Event { return s.ch }

// Filter returns a copy of the filter this subscription was created with.
func (s *Subscription) Filter() Filter { return s.filter }

// Cancel removes the subscription from the bus and decrements the per-kind
// counters. It does NOT close the channel: closing while Bus.Emit may be
// holding a reference would race; instead the channel is leaked and GC'd
// once nothing holds it.
//
// Pending events still buffered in the channel can be drained or ignored at
// the consumer's discretion. If they are drained, each one MUST still be
// Released to return the underlying memory to its pool.
func (s *Subscription) Cancel() {
	s.cancelOnce.Do(func() {
		if s.dead || s.bus == nil {
			return
		}

		s.bus.mu.Lock()
		delete(s.bus.subscribers, s.id)
		s.bus.mu.Unlock()

		// Decrement per-kind counters to mirror Subscribe's increments.
		if len(s.filter.Kinds) == 0 {
			for k := EventKind(1); int(k) < numEventKinds; k++ {
				s.bus.activeCounts[k].Add(-1)
			}
		} else {
			for _, k := range s.filter.Kinds {
				if int(k) < numEventKinds {
					s.bus.activeCounts[k].Add(-1)
				}
			}
		}

		s.dead = true
	})
}
