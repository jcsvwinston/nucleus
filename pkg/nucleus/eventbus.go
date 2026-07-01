package nucleus

import (
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// EventBus is a first-party, stable view of the framework's in-process
// observability bus (pkg/observability, classified experimental), for a module
// that renders a live activity feed — e.g. orbit's live SQL/HTTP view.
//
// It exposes the subscribe operations a consumer needs plus a narrow SQL ingest
// (EmitSQL) for external producers, and moves nucleus-owned event VALUES
// (SQLEvent/HTTPEvent), not the bus's pooled, refcounted event objects. So a
// module never imports the experimental package, is insulated from its pre-v1.0
// churn, and is freed from the bus's Release discipline — the adapter performs
// the required Release internally and hands the consumer a detached copy it owns
// outright (subscribe) or copies the producer's values in (emit).
//
// Each Subscribe* method returns a receive-only channel and a cancel func. The
// caller MUST call cancel when finished, to unsubscribe and stop the backing
// goroutine; the channel is closed once cancel has run. A slow consumer drops
// events — the same backpressure the underlying bus applies — rather than
// blocking producers.
type EventBus interface {
	// SubscribeSQL streams SQL-statement events until the returned cancel runs.
	SubscribeSQL() (<-chan SQLEvent, func())
	// SubscribeHTTP streams HTTP-request events until the returned cancel runs.
	SubscribeHTTP() (<-chan HTTPEvent, func())

	// EmitSQL publishes a SQL-statement event onto the bus so it reaches every
	// SubscribeSQL consumer. It is the emit counterpart to SubscribeSQL: an
	// external producer that runs SQL outside the framework's own CRUD layer
	// (e.g. an ORM bridge) can surface those statements in the same live feed
	// without importing the experimental pkg/observability package.
	//
	// The adapter converts the first-party SQLEvent value into the bus's
	// pooled, refcounted event and owns the Release discipline internally; the
	// caller keeps ownership of ev and its Args slice (they are copied, not
	// aliased). The caller sets EmittedAt (typically the query's completion
	// time) and, when correlation is wanted, RequestID/TraceID/UserID. Args
	// SHOULD already be sanitized by the producer — the bus does not redact on
	// emit; see SQLEvent.Args.
	EmitSQL(ev SQLEvent)
}

// SQLEvent is a detached, first-party copy of a SQL-statement observability
// event. All fields are plain values the consumer owns; there is no Release
// obligation.
type SQLEvent struct {
	EmittedAt time.Time
	NodeID    string
	ModelName string
	Operation string
	Query     string
	// Args are the bound arguments AFTER the observability layer's emit-time
	// sanitization: string and []byte values are replaced with a "type(len):***"
	// marker, but numeric, bool, time.Time and nil args are kept verbatim (so a
	// "WHERE id = ?" key appears as e.g. "42"). Operator-only; see EventBus.
	Args      []string
	Duration  time.Duration
	Err       string
	RequestID string
	TraceID   string
	UserID    string
}

// HTTPEvent is a detached, first-party copy of an HTTP-request observability
// event.
type HTTPEvent struct {
	EmittedAt time.Time
	NodeID    string
	Method    string
	Path      string
	Status    int
	Duration  time.Duration
	RequestID string
	TraceID   string
	UserID    string
	RemoteIP  string
	UserAgent string
	// PayloadPreview is an emit-time-sanitized preview: GET/DELETE show redacted
	// query params (keys containing KEY/SECRET/PASSWORD/TOKEN are masked — note
	// OAuth code/state are not), other methods show "body:redacted (...)". The
	// raw request body is never captured.
	PayloadPreview string
}

// busAdapter adapts a *observability.Bus to the first-party EventBus. It is
// unexported: the observability type never appears on the public surface.
type busAdapter struct{ bus *observability.Bus }

func (a busAdapter) SubscribeSQL() (<-chan SQLEvent, func()) {
	return subscribeTyped(a.bus, observability.KindSQLStatement, toSQLEvent)
}

func (a busAdapter) SubscribeHTTP() (<-chan HTTPEvent, func()) {
	return subscribeTyped(a.bus, observability.KindHTTPRequest, toHTTPEvent)
}

func (a busAdapter) EmitSQL(ev SQLEvent) {
	a.bus.Emit(fromSQLEvent(ev))
}

// fromSQLEvent is the inverse of toSQLEvent: it builds a pooled bus event from a
// detached first-party SQLEvent. Ownership of the returned event transfers to
// Bus.Emit. Args is copied into the pooled event's spare-capacity slice so the
// caller keeps ownership of its own slice (mirroring toSQLEvent, which copies the
// bus's backing array out).
func fromSQLEvent(ev SQLEvent) *observability.SQLStatementEvent {
	e := observability.AcquireSQLStatementEvent(ev.EmittedAt, ev.NodeID)
	e.ModelName = ev.ModelName
	e.Operation = ev.Operation
	e.Query = ev.Query
	e.Args = append(e.Args, ev.Args...)
	e.Duration = ev.Duration
	e.Err = ev.Err
	e.RequestID = ev.RequestID
	e.TraceID = ev.TraceID
	e.UserID = ev.UserID
	return e
}

// toSQLEvent copies a bus event into a detached SQLEvent. Args is copied because
// the bus reuses the event's backing array on Release.
func toSQLEvent(ev observability.Event) (SQLEvent, bool) {
	e, ok := ev.(*observability.SQLStatementEvent)
	if !ok {
		return SQLEvent{}, false
	}
	return SQLEvent{
		EmittedAt: e.EmittedAt(),
		NodeID:    e.NodeID(),
		ModelName: e.ModelName,
		Operation: e.Operation,
		Query:     e.Query,
		Args:      append([]string(nil), e.Args...),
		Duration:  e.Duration,
		Err:       e.Err,
		RequestID: e.RequestID,
		TraceID:   e.TraceID,
		UserID:    e.UserID,
	}, true
}

// toHTTPEvent copies a bus event into a detached HTTPEvent (no slice fields, so
// no backing-array aliasing concern, unlike toSQLEvent).
func toHTTPEvent(ev observability.Event) (HTTPEvent, bool) {
	e, ok := ev.(*observability.HTTPRequestEvent)
	if !ok {
		return HTTPEvent{}, false
	}
	return HTTPEvent{
		EmittedAt:      e.EmittedAt(),
		NodeID:         e.NodeID(),
		Method:         e.Method,
		Path:           e.Path,
		Status:         e.Status,
		Duration:       e.Duration,
		RequestID:      e.RequestID,
		TraceID:        e.TraceID,
		UserID:         e.UserID,
		RemoteIP:       e.RemoteIP,
		UserAgent:      e.UserAgent,
		PayloadPreview: e.PayloadPreview,
	}, true
}

// subscribeTyped subscribes to one event kind on the bus and pumps translated,
// detached values onto a fresh channel. It owns the Release discipline: every
// event received (or left buffered at cancel) is Released exactly once. cancel
// removes the subscription (the bus, by design, does NOT close its channel) and
// stops the pump, which then drains any buffered events to honour their Release
// obligation before closing the output channel.
func subscribeTyped[T any](bus *observability.Bus, kind observability.EventKind, conv func(observability.Event) (T, bool)) (<-chan T, func()) {
	sub, cancel := bus.Subscribe(
		observability.Filter{Kinds: []observability.EventKind{kind}},
		nil,
	)
	out := make(chan T, observability.DefaultSubscriberChannelSize)
	done := make(chan struct{})

	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				drainAndRelease(sub)
				return
			case ev, ok := <-sub.Ch():
				if !ok {
					return
				}
				v, keep := conv(ev)
				ev.Release()
				if !keep {
					continue
				}
				select {
				case out <- v:
				case <-done:
					drainAndRelease(sub)
					return
				}
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			close(done)
		})
	}
	return out, stop
}

// drainAndRelease Releases any events already buffered on the cancelled
// subscription's channel, so events the bus delivered before cancel do not leak
// their pool references. Note: an Emit that snapshotted this subscription before
// Cancel returned can still deliver after this drain runs; those few refs are
// bounded (≤1 per concurrent Emit) and GC-collected — they cannot cause a panic
// or unbounded leak. This mirrors the observability bus's own subscription drain.
func drainAndRelease(sub *observability.Subscription) {
	for {
		select {
		case ev, ok := <-sub.Ch():
			if !ok {
				return
			}
			ev.Release()
		default:
			return
		}
	}
}
