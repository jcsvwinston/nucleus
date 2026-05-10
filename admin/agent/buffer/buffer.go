// Package buffer holds the per-event-kind drop-oldest ring buffer the
// agent uses to bridge brief disconnects from the admin server.
//
// The buffer captures events that arrived during a microcorte (a network
// blip, a server reconnect, etc.) so that when the stream comes back up
// the agent can flush a few seconds of recent activity instead of leaving
// a hole in the panel. Buffers are bounded; on overflow the oldest entry
// is evicted (drop-oldest), and the agent's events_dropped_total{
// reason="buffer_full"} counter is bumped by the agent layer.
//
// This is NOT a replay log. It does not persist across restarts and does
// not survive process death.
package buffer

import (
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// Buffer is an opaque per-kind drop-oldest store. Use Set to provision a
// per-kind buffer at agent boot, Push when the stream is down, Drain when
// the stream comes back. Snapshot is a read-only view that does not
// consume the buffer (used by metrics).
type Buffer struct {
	mu      sync.Mutex
	cap     int
	items   []*adminv1.Event
	head    int
	size    int
	dropped uint64
}

// New constructs a buffer with the given capacity. Capacity ≤ 0 falls
// back to the default of 64 entries.
func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 64
	}
	return &Buffer{
		cap:   capacity,
		items: make([]*adminv1.Event, capacity),
	}
}

// Push adds e at the head, evicting the oldest entry if the buffer is
// full. Returns true when the push displaced an older entry. The caller
// (agent) maps that to "events_dropped_total{reason=buffer_full}".
func (b *Buffer) Push(e *adminv1.Event) (evicted bool) {
	if b == nil || e == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == b.cap {
		evicted = true
		b.dropped++
	}
	b.items[b.head] = e
	b.head = (b.head + 1) % b.cap
	if b.size < b.cap {
		b.size++
	}
	return evicted
}

// Drain removes and returns up to limit entries, oldest first. Pass a
// negative or zero limit to drain everything currently buffered. Drain
// does not block — it is a snapshot operation done under the buffer's
// own lock.
func (b *Buffer) Drain(limit int) []*adminv1.Event {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil
	}
	want := b.size
	if limit > 0 && limit < want {
		want = limit
	}

	out := make([]*adminv1.Event, 0, want)
	tail := (b.head - b.size + b.cap) % b.cap
	for i := 0; i < want; i++ {
		idx := (tail + i) % b.cap
		out = append(out, b.items[idx])
		b.items[idx] = nil
	}
	b.size -= want
	if b.size == 0 {
		b.head = 0
	}
	return out
}

// Len returns the current number of buffered events.
func (b *Buffer) Len() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// Capacity returns the configured maximum.
func (b *Buffer) Capacity() int {
	if b == nil {
		return 0
	}
	return b.cap
}

// Dropped returns the cumulative number of evictions since construction.
func (b *Buffer) Dropped() uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// PerKind groups four buffers (one per EventKind) so callers can route by
// kind without an extra map lookup at the call site.
type PerKind struct {
	HTTP    *Buffer
	SQL     *Buffer
	Session *Buffer
	Custom  *Buffer
}

// NewPerKind constructs four buffers with the given capacities. Pass
// capacities ≤ 0 to use the per-kind default.
func NewPerKind(capacities map[observability.EventKind]int) *PerKind {
	get := func(k observability.EventKind) int {
		if capacities == nil {
			return 0
		}
		return capacities[k]
	}
	return &PerKind{
		HTTP:    New(get(observability.KindHTTPRequest)),
		SQL:     New(get(observability.KindSQLStatement)),
		Session: New(get(observability.KindSessionChange)),
		Custom:  New(get(observability.KindCustom)),
	}
}

// For returns the buffer for the given kind, or nil for unknown kinds.
func (p *PerKind) For(k observability.EventKind) *Buffer {
	if p == nil {
		return nil
	}
	switch k {
	case observability.KindHTTPRequest:
		return p.HTTP
	case observability.KindSQLStatement:
		return p.SQL
	case observability.KindSessionChange:
		return p.Session
	case observability.KindCustom:
		return p.Custom
	default:
		return nil
	}
}

// DrainAll empties every buffer and returns the events oldest-first,
// interleaved by kind in declaration order (HTTP, SQL, Session, Custom).
// Used during graceful drain on shutdown and on reconnect.
func (p *PerKind) DrainAll() []*adminv1.Event {
	if p == nil {
		return nil
	}
	out := make([]*adminv1.Event, 0)
	for _, b := range []*Buffer{p.HTTP, p.SQL, p.Session, p.Custom} {
		out = append(out, b.Drain(0)...)
	}
	return out
}

// LenSnapshot returns the per-kind count, intended for periodic Prometheus
// gauge updates.
func (p *PerKind) LenSnapshot() map[observability.EventKind]int {
	if p == nil {
		return nil
	}
	return map[observability.EventKind]int{
		observability.KindHTTPRequest:   p.HTTP.Len(),
		observability.KindSQLStatement:  p.SQL.Len(),
		observability.KindSessionChange: p.Session.Len(),
		observability.KindCustom:        p.Custom.Len(),
	}
}

// _unused keeps "time" referenced once in case future evolution wants to
// add per-entry timestamps; the proto Event already carries them.
var _ = time.Time{}
