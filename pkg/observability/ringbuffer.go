package observability

import (
	"sync"
	"time"
)

// RingBuffer is a bounded, drop-oldest buffer for retained event copies.
// It is the per-kind "replay" store the agent and admin server use so a
// freshly opened panel sees recent activity instead of an empty stream.
//
// RingBuffer holds COPIES of event payloads, not references to pooled
// events. It therefore does NOT participate in the refcount lifecycle: a
// caller who wants to retain an event past Bus.Emit must copy the relevant
// fields out into a RingBufferEntry-shaped value and pass that to Push.
//
// RingBuffer is goroutine-safe. Push and Snapshot may be called from
// different goroutines.
type RingBuffer[T any] struct {
	mu       sync.RWMutex
	items    []ringEntry[T]
	head     int
	size     int
	capacity int
	dropped  uint64
}

type ringEntry[T any] struct {
	at   time.Time
	data T
}

// NewRingBuffer constructs an empty ring buffer with the given capacity.
// Capacity must be > 0; non-positive values default to 64.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		capacity = 64
	}
	return &RingBuffer[T]{
		items:    make([]ringEntry[T], capacity),
		capacity: capacity,
	}
}

// Push records the data at the given timestamp. If the buffer is full the
// oldest entry is dropped (drop-oldest), and the dropped counter is bumped.
// Push never blocks.
func (r *RingBuffer[T]) Push(at time.Time, data T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size == r.capacity {
		r.dropped++
	}
	r.items[r.head] = ringEntry[T]{at: at, data: data}
	r.head = (r.head + 1) % r.capacity
	if r.size < r.capacity {
		r.size++
	}
}

// Snapshot returns up to limit items, newest first. It allocates a fresh
// slice; callers may retain the result without further synchronization.
// limit <= 0 returns an empty slice.
func (r *RingBuffer[T]) Snapshot(limit int) []T {
	if limit <= 0 {
		return []T{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.size == 0 {
		return []T{}
	}
	if limit > r.size {
		limit = r.size
	}

	out := make([]T, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		out = append(out, r.items[idx].data)
	}
	return out
}

// Len returns the current number of buffered entries.
func (r *RingBuffer[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}

// Capacity returns the configured maximum number of entries.
func (r *RingBuffer[T]) Capacity() int { return r.capacity }

// Dropped returns the cumulative number of entries that were overwritten
// because the buffer was full at the time of Push.
func (r *RingBuffer[T]) Dropped() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dropped
}
