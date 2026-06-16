package admin

import (
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// ConsumeObservability wires the live view's SQL feed to the application
// observability bus. The bus receives EVERY model.CRUD query across the whole
// application — the framework's default SQL observer (installed in pkg/app)
// emits to it — not just the queries issued by the admin's own Data Studio
// CRUDs. This is the "bus becomes the single SQL feed" step: application
// queries (REST resources, app-side CRUD) now surface in the live view, which
// previously only saw the admin panel's own browsing.
//
// It marks the panel as bus-connected so getCRUD skips the now-redundant
// per-CRUD observer (avoiding double-recording), starts a goroutine draining
// the subscription into the SQL ring buffer + live stream, and returns a stop
// function (also invoked from Close). Safe no-op when p, p.live, or bus is nil.
func (p *Panel) ConsumeObservability(bus *observability.Bus) func() {
	if p == nil || p.live == nil || bus == nil {
		return func() {}
	}
	// Default channel size (256) is sized for live SQL bursts; a slow live
	// view drops deliveries (counted by the bus) rather than blocking Emit.
	sub, cancel := bus.Subscribe(
		observability.Filter{Kinds: []observability.EventKind{observability.KindSQLStatement}},
		nil,
	)
	p.observConnected.Store(true)

	// Cancel() removes the subscription from the bus but, by design, does NOT
	// close sub.Ch() (closing while Emit may hold a reference would race). So
	// the drain goroutine is stopped via `done`, and after that it drains any
	// already-buffered events to honour their Release obligations.
	done := make(chan struct{})
	stop := func() {
		p.observStopOnce.Do(func() {
			cancel()
			close(done)
		})
	}
	p.observCancel = stop

	go func() {
		for {
			select {
			case ev, ok := <-sub.Ch():
				if !ok {
					return
				}
				if sqlEv, isSQL := ev.(*observability.SQLStatementEvent); isSQL {
					p.recordBusSQL(sqlEv)
				}
				ev.Release()
			case <-done:
				for {
					select {
					case ev := <-sub.Ch():
						ev.Release()
					default:
						return
					}
				}
			}
		}
	}()
	return stop
}

// recordBusSQL converts a bus SQL event into the live view's shape and records
// it in the ring buffer + the live event stream (mirrors onModelSQLQuery, but
// sourced from the bus so it covers the whole application). The hook that
// produced the event already truncated/sanitised Query and Args.
func (p *Panel) recordBusSQL(e *observability.SQLStatementEvent) {
	if p == nil || p.live == nil || e == nil {
		return
	}
	event := liveSQLEvent{
		NodeID:     strings.TrimSpace(e.NodeID()),
		Timestamp:  e.EmittedAt().UTC().Format(time.RFC3339),
		ModelName:  strings.TrimSpace(e.ModelName),
		Operation:  truncateText(strings.TrimSpace(e.Operation), 64),
		Query:      truncateText(compactSQL(e.Query), 640),
		Args:       append([]string(nil), e.Args...),
		DurationMS: e.Duration.Milliseconds(),
		Error:      truncateText(strings.TrimSpace(e.Err), 220),
		RequestID:  strings.TrimSpace(e.RequestID),
		TraceID:    strings.TrimSpace(e.TraceID),
		UserID:     strings.TrimSpace(e.UserID),
	}
	if event.NodeID == "" {
		event.NodeID = p.liveNodeID()
	}
	p.live.sql.push(event)
	envelope := liveEventEnvelope{
		NodeID:    event.NodeID,
		Type:      "db.query",
		Timestamp: event.Timestamp,
		SQL:       &event,
	}
	p.live.bus.publish(envelope)
	p.publishLiveClusterEvent(envelope)
}
