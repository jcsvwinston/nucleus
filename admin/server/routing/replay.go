package routing

import (
	"sync"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// Replay is the per-kind drop-oldest ring buffer the server keeps so a
// freshly opened UI panel sees a few seconds of recent activity instead
// of an empty stream.
//
// The buffer holds proto Event copies — they are already off the agent's
// hot path by the time they reach here, so reuse / pooling is not worth
// the complexity.
type Replay struct {
	http    *bucket
	sql     *bucket
	session *bucket
	custom  *bucket
}

// ReplayCapacities tunes per-kind capacity. Pass zero on a kind to
// disable replay for that kind.
type ReplayCapacities struct {
	HTTP    int
	SQL     int
	Session int
	Custom  int
}

// NewReplay constructs a Replay with the given capacities.
func NewReplay(c ReplayCapacities) *Replay {
	return &Replay{
		http:    newBucket(c.HTTP),
		sql:     newBucket(c.SQL),
		session: newBucket(c.Session),
		custom:  newBucket(c.Custom),
	}
}

// Push records the event at the appropriate per-kind bucket. Events of
// unknown kind are dropped silently.
func (r *Replay) Push(e *adminv1.Event) {
	if r == nil || e == nil {
		return
	}
	switch e.GetBody().(type) {
	case *adminv1.Event_HttpRequest:
		r.http.push(e)
	case *adminv1.Event_SqlStatement:
		r.sql.push(e)
	case *adminv1.Event_SessionChange:
		r.session.push(e)
	case *adminv1.Event_Custom:
		r.custom.push(e)
	}
}

// Snapshot returns up to limit events that match the filter, oldest
// first. limit <= 0 returns everything currently buffered.
func (r *Replay) Snapshot(filter *adminv1.Filter, limit int) []*adminv1.Event {
	if r == nil {
		return nil
	}
	out := make([]*adminv1.Event, 0)
	out = appendIfWanted(out, r.http.snapshot(), filter)
	out = appendIfWanted(out, r.sql.snapshot(), filter)
	out = appendIfWanted(out, r.session.snapshot(), filter)
	out = appendIfWanted(out, r.custom.snapshot(), filter)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// LenSnapshot returns the per-kind sizes; useful for /metrics gauges.
func (r *Replay) LenSnapshot() map[adminv1.EventType]int {
	return map[adminv1.EventType]int{
		adminv1.EventType_EVENT_TYPE_HTTP_REQUEST:   r.http.len(),
		adminv1.EventType_EVENT_TYPE_SQL_STATEMENT:  r.sql.len(),
		adminv1.EventType_EVENT_TYPE_SESSION_CHANGE: r.session.len(),
		adminv1.EventType_EVENT_TYPE_CUSTOM:         r.custom.len(),
	}
}

func appendIfWanted(out, src []*adminv1.Event, f *adminv1.Filter) []*adminv1.Event {
	for _, e := range src {
		if !replayMatches(f, e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// replayMatches is a copy of EventSubscription.matches without sampling;
// the replay buffer should respect filters but not stochastic sampling
// (the events buffered are already a curated set).
func replayMatches(f *adminv1.Filter, e *adminv1.Event) bool {
	if f == nil {
		return true
	}

	if len(f.Types) > 0 {
		eventType := eventTypeOf(e)
		ok := false
		for _, t := range f.Types {
			if t == eventType {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}

	if len(f.NodeIds) > 0 {
		ok := false
		for _, n := range f.NodeIds {
			if n == e.NodeId {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}

	if http := e.GetHttpRequest(); http != nil {
		if !methodMatches(f.HttpMethods, http.Method) {
			return false
		}
		if !pathMatches(f.HttpPathGlobs, http.Path) {
			return false
		}
		if !statusClassMatches(f.HttpStatusClasses, int(http.Status)) {
			return false
		}
	}
	if sql := e.GetSqlStatement(); sql != nil {
		if !sqlModelMatches(f.SqlModels, sql.ModelName) {
			return false
		}
	}
	return true
}

// bucket is a per-kind drop-oldest ring buffer.
type bucket struct {
	mu    sync.RWMutex
	cap   int
	items []*adminv1.Event
	head  int
	size  int
}

func newBucket(cap int) *bucket {
	if cap <= 0 {
		// disabled
		return &bucket{}
	}
	return &bucket{cap: cap, items: make([]*adminv1.Event, cap)}
}

func (b *bucket) push(e *adminv1.Event) {
	if b == nil || b.cap == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[b.head] = e
	b.head = (b.head + 1) % b.cap
	if b.size < b.cap {
		b.size++
	}
}

func (b *bucket) snapshot() []*adminv1.Event {
	if b == nil || b.cap == 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.size == 0 {
		return nil
	}
	out := make([]*adminv1.Event, 0, b.size)
	tail := (b.head - b.size + b.cap) % b.cap
	for i := 0; i < b.size; i++ {
		out = append(out, b.items[(tail+i)%b.cap])
	}
	return out
}

func (b *bucket) len() int {
	if b == nil || b.cap == 0 {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}
