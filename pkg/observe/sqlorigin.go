package observe

import "context"

// The "model-observed" context marker is the de-duplication signal between
// the two SQL observation layers:
//
//   - pkg/model.CRUD already emits a SQLEvent for every statement it runs,
//     enriched with the ModelName it knows.
//   - Optional driver-level instrumentation (pkg/db) wraps the raw
//     database/sql driver to catch statements that bypass CRUD entirely
//     (direct db.QueryContext/ExecContext from outbox, session stores,
//     migrations, etc.) — traffic CRUD never sees.
//
// Both layers observe the same physical query when the statement originates
// in CRUD (CRUD's exec/query call the wrapped driver). To avoid recording
// it twice, CRUD stamps the context it passes to database/sql with this
// marker, and the driver wrapper skips emission when the marker is present.
// A statement without the marker is a genuine bypass — exactly what the
// driver layer exists to surface — so it is emitted.
//
// This mirrors ADR-018's "skip-when-connected" precedent: rather than
// de-duplicating downstream, the producer that would otherwise double-record
// suppresses its second write at the source.

// CtxWithModelObserved returns a context marked as already observed by the
// model/CRUD SQL layer, so driver-level instrumentation skips re-emitting the
// same statement. CRUD stamps this on the context it hands to database/sql.
func CtxWithModelObserved(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyModelObserved, true)
}

// IsModelObserved reports whether the context was marked by CtxWithModelObserved
// — i.e. the statement is CRUD-originated and already observed at the model
// layer. Driver-level instrumentation checks this to avoid double-recording.
func IsModelObserved(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyModelObserved).(bool)
	return v
}
