// Package routing implements the server-side fanout: it receives proto
// events from agents (via the AgentService handler) and republishes them
// to UI subscribers (via the ControlService.StreamEvents handler).
//
// The fanout is intentionally simpler than pkg/observability.Bus:
//   - Producers send copies, not pooled refcounted events. Once an event
//     reaches the server, it's already off the agent's hot path; the
//     allocation cost is negligible compared to the network IO.
//   - Drop policy: per-subscription channel, drop-newest on overflow.
//     Counted in EventBus.Stats.
//   - Filter and per-kind sampling rate are evaluated at fanout time
//     (each subscription has its own filter). This keeps the AGENT-side
//     subscription a single union of all UI demands; the server narrows
//     per UI.
package routing

import (
	"strings"
	"sync"
	"sync/atomic"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// EventBus is the server-side fanout from agents to UIs.
type EventBus struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]*EventSubscription

	// kind activeCount: when zero we still admit events to the replay
	// buffer at the publisher side, but the per-kind subscriber demand
	// is what governs whether the agent should ship them. The server
	// queries this via HasDemand to compute the agent-side union filter.
	httpDemand    atomic.Int64
	sqlDemand     atomic.Int64
	sessionDemand atomic.Int64
	customDemand  atomic.Int64

	publishedTotal atomic.Uint64
	droppedTotal   atomic.Uint64
}

// EventSubscription is one UI-side subscription. The server creates one
// per ControlService.StreamEvents call.
type EventSubscription struct {
	id           uint64
	bus          *EventBus
	filter       *adminv1.Filter
	samplingRate map[string]float32
	ch           chan *adminv1.Event

	cancelOnce sync.Once
	closed     atomic.Bool
}

// NewEventBus constructs an EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[uint64]*EventSubscription),
	}
}

// Subscribe registers a new subscription. The caller drains sub.Ch and
// MUST call cancel exactly once to release resources.
//
// channelSize is the per-subscription buffer. Pass 0 for default 256.
func (b *EventBus) Subscribe(filter *adminv1.Filter, samplingRate map[string]float32, channelSize int) (*EventSubscription, func()) {
	if channelSize <= 0 {
		channelSize = 256
	}

	sub := &EventSubscription{
		bus:          b,
		filter:       filter,
		samplingRate: cloneFloatMap(samplingRate),
		ch:           make(chan *adminv1.Event, channelSize),
	}

	b.mu.Lock()
	b.nextID++
	sub.id = b.nextID
	b.subscribers[sub.id] = sub
	b.mu.Unlock()

	for _, k := range demandedKinds(filter) {
		b.demandFor(k).Add(1)
	}

	return sub, sub.Cancel
}

// Publish fans an event out to every matching subscription. Returns the
// number of subscribers the event was delivered to (excluding drops).
func (b *EventBus) Publish(e *adminv1.Event) int {
	if b == nil || e == nil {
		return 0
	}
	b.publishedTotal.Add(1)

	b.mu.RLock()
	matched := make([]*EventSubscription, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		if sub.matches(e) {
			matched = append(matched, sub)
		}
	}
	b.mu.RUnlock()

	delivered := 0
	for _, sub := range matched {
		select {
		case sub.ch <- e:
			delivered++
		default:
			b.droppedTotal.Add(1)
		}
	}
	return delivered
}

// HasDemand reports whether at least one subscription matches the given
// event kind. The agent-service writer uses this to decide whether to
// shut off ingress when no UI is watching.
func (b *EventBus) HasDemand(t adminv1.EventType) bool {
	if b == nil {
		return false
	}
	d := b.demandFor(t)
	if d == nil {
		return false
	}
	return d.Load() > 0
}

func (b *EventBus) demandFor(t adminv1.EventType) *atomic.Int64 {
	switch t {
	case adminv1.EventType_EVENT_TYPE_HTTP_REQUEST:
		return &b.httpDemand
	case adminv1.EventType_EVENT_TYPE_SQL_STATEMENT:
		return &b.sqlDemand
	case adminv1.EventType_EVENT_TYPE_SESSION_CHANGE:
		return &b.sessionDemand
	case adminv1.EventType_EVENT_TYPE_CUSTOM:
		return &b.customDemand
	default:
		return nil
	}
}

// SubscriberCount returns the total number of live subscriptions.
func (b *EventBus) SubscriberCount() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	n := len(b.subscribers)
	b.mu.RUnlock()
	return n
}

// Stats returns published / dropped totals.
type Stats struct {
	Published uint64
	Dropped   uint64
}

// Stats returns publish counters.
func (b *EventBus) Stats() Stats {
	if b == nil {
		return Stats{}
	}
	return Stats{
		Published: b.publishedTotal.Load(),
		Dropped:   b.droppedTotal.Load(),
	}
}

// AggregateFilter computes the union of every live subscription's Filter
// (Types and SqlModels are unioned). The server pushes this to each agent
// as a single Subscribe with id = "server-aggregate", so the agent only
// has to maintain one bus subscription regardless of how many UIs are
// open.
//
// HTTP/SQL-specific filters and NodeIDs are NOT aggregated server-side;
// the per-UI filter is enforced inside Publish via subscription.matches.
// The agent only needs to know "what kinds to ship and from which
// models", not how to route to specific UIs.
func (b *EventBus) AggregateFilter() *adminv1.Filter {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.subscribers) == 0 {
		return nil
	}

	out := &adminv1.Filter{}
	kindsSeen := map[adminv1.EventType]struct{}{}
	modelsSeen := map[string]struct{}{}
	anyOpen := false

	for _, s := range b.subscribers {
		f := s.filter
		if f == nil || len(f.Types) == 0 {
			anyOpen = true
		} else {
			for _, t := range f.Types {
				kindsSeen[t] = struct{}{}
			}
		}
		if f != nil {
			for _, m := range f.SqlModels {
				m = strings.TrimSpace(m)
				if m != "" {
					modelsSeen[m] = struct{}{}
				}
			}
		}
	}

	if !anyOpen {
		out.Types = make([]adminv1.EventType, 0, len(kindsSeen))
		for t := range kindsSeen {
			out.Types = append(out.Types, t)
		}
	}
	out.SqlModels = make([]string, 0, len(modelsSeen))
	for m := range modelsSeen {
		out.SqlModels = append(out.SqlModels, m)
	}
	return out
}

// Ch returns the subscription's event channel. Each event is owned by
// the consumer (no Release semantics; it's a proto value).
func (s *EventSubscription) Ch() <-chan *adminv1.Event {
	if s == nil {
		c := make(chan *adminv1.Event)
		close(c)
		return c
	}
	return s.ch
}

// Cancel removes the subscription from the bus. Idempotent. Does NOT
// close the channel (per pkg/observability convention) — pending events
// are GC'd once the consumer stops reading.
func (s *EventSubscription) Cancel() {
	if s == nil {
		return
	}
	s.cancelOnce.Do(func() {
		s.closed.Store(true)
		s.bus.mu.Lock()
		delete(s.bus.subscribers, s.id)
		s.bus.mu.Unlock()
		for _, k := range demandedKinds(s.filter) {
			s.bus.demandFor(k).Add(-1)
		}
	})
}

func (s *EventSubscription) matches(e *adminv1.Event) bool {
	if s == nil || e == nil {
		return false
	}
	f := s.filter
	if f == nil {
		return true
	}

	if len(f.Types) > 0 {
		eventType := eventTypeOf(e)
		found := false
		for _, t := range f.Types {
			if t == eventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(f.NodeIds) > 0 {
		ok := false
		target := strings.TrimSpace(e.NodeId)
		for _, want := range f.NodeIds {
			if strings.EqualFold(strings.TrimSpace(want), target) {
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

func eventTypeOf(e *adminv1.Event) adminv1.EventType {
	switch e.GetBody().(type) {
	case *adminv1.Event_HttpRequest:
		return adminv1.EventType_EVENT_TYPE_HTTP_REQUEST
	case *adminv1.Event_SqlStatement:
		return adminv1.EventType_EVENT_TYPE_SQL_STATEMENT
	case *adminv1.Event_SessionChange:
		return adminv1.EventType_EVENT_TYPE_SESSION_CHANGE
	case *adminv1.Event_Custom:
		return adminv1.EventType_EVENT_TYPE_CUSTOM
	default:
		return adminv1.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

func demandedKinds(f *adminv1.Filter) []adminv1.EventType {
	if f == nil || len(f.Types) == 0 {
		return []adminv1.EventType{
			adminv1.EventType_EVENT_TYPE_HTTP_REQUEST,
			adminv1.EventType_EVENT_TYPE_SQL_STATEMENT,
			adminv1.EventType_EVENT_TYPE_SESSION_CHANGE,
			adminv1.EventType_EVENT_TYPE_CUSTOM,
		}
	}
	return append([]adminv1.EventType(nil), f.Types...)
}

func cloneFloatMap(in map[string]float32) map[string]float32 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float32, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
