package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

const (
	maxSQLArgs       = 16
	maxSQLQueryBytes = 640
	maxSQLOperBytes  = 64
	maxSQLErrBytes   = 220
)

// SQLObserverConfig configures NewSQLObserver.
type SQLObserverConfig struct {
	// Bus is the observability bus events are emitted to. Required.
	Bus *observability.Bus

	// NodeID identifies this framework process.
	NodeID string
}

// SQLQueryEvent describes one SQL statement the framework's data layer ran.
// Values in Args are the raw runtime arguments of the call site; the observer
// sanitizes them before anything leaves the process.
//
// It has the same fields as model.SQLQueryEvent, so the two convert by
// assignment: the observer's contract lives here, with the observer, so an
// application can adopt it now and stop naming pkg/model, which leaves the
// public API at v2.0.0 (DEP-2026-024).
type SQLQueryEvent struct {
	ModelName string
	Operation string
	Query     string
	Args      []interface{}
	Duration  time.Duration
	Error     error
	// RowsAffected is the driver-reported row count for exec-style
	// operations (INSERT/UPDATE/DELETE). 0 means "not reported": SELECT
	// paths cannot know it without consuming the rows, and some drivers
	// do not support it.
	RowsAffected int64
}

// SQLQueryObserver receives one SQLQueryEvent per observed statement.
type SQLQueryObserver func(ctx context.Context, event SQLQueryEvent)

// NewSQLObserver returns a model.SQLQueryObserver that emits a
// SQLStatementEvent for every observed CRUD query, gated on
// HasSubscribers(KindSQLStatement). It is NewSQLQueryObserver typed for
// model.CRUD, which is what the framework's data layer calls it with today;
// the sanitization is the same.
//
// The observer pre-sanitizes argument values: strings and bytes become
// "type(len):***" markers; primitives are formatted "type:value"; times
// are RFC3339; nils are "null". The raw argument values are NEVER shipped.
//
// The signature names pkg/model, whose symbols are covered by DEP-2026-024
// and leave the public API at v2.0.0; the function keeps this signature
// until then. New code that does not go through model.CRUD takes
// NewSQLQueryObserver, whose types live in this package.
func NewSQLObserver(cfg SQLObserverConfig) model.SQLQueryObserver {
	observe := NewSQLQueryObserver(cfg)
	return func(ctx context.Context, q model.SQLQueryEvent) {
		observe(ctx, SQLQueryEvent(q))
	}
}

// NewSQLQueryObserver returns a SQLQueryObserver that emits a
// SQLStatementEvent for every observed query, gated on
// HasSubscribers(KindSQLStatement). It is the observer NewSQLObserver wraps,
// over this package's own event type.
//
// The observer pre-sanitizes argument values: strings and bytes become
// "type(len):***" markers; primitives are formatted "type:value"; times
// are RFC3339; nils are "null". The raw argument values are NEVER shipped.
func NewSQLQueryObserver(cfg SQLObserverConfig) SQLQueryObserver {
	if cfg.Bus == nil {
		// No-op observer.
		return func(_ context.Context, _ SQLQueryEvent) {}
	}

	return func(ctx context.Context, q SQLQueryEvent) {
		// Hot path gate.
		if !cfg.Bus.HasSubscribers(observability.KindSQLStatement) {
			return
		}

		ev := observability.AcquireSQLStatementEvent(time.Now().UTC(), cfg.NodeID)
		ev.ModelName = strings.TrimSpace(q.ModelName)
		ev.Operation = truncate(strings.TrimSpace(q.Operation), maxSQLOperBytes)
		ev.Query = truncate(compactSQL(q.Query), maxSQLQueryBytes)
		ev.Args = appendSanitizedArgs(ev.Args, q.Args)
		ev.Duration = q.Duration
		ev.RowsAffected = q.RowsAffected
		if q.Error != nil {
			ev.Err = truncate(q.Error.Error(), maxSQLErrBytes)
		}
		ev.RequestID = strings.TrimSpace(observe.RequestIDFromCtx(ctx))
		ev.TraceID = strings.TrimSpace(observe.TraceIDFromCtx(ctx))
		ev.UserID = strings.TrimSpace(observe.UserIDFromCtx(ctx))

		cfg.Bus.Emit(ev)
	}
}

func compactSQL(query string) string {
	if strings.TrimSpace(query) == "" {
		return ""
	}
	parts := strings.Fields(query)
	return strings.Join(parts, " ")
}

func appendSanitizedArgs(dst []string, args []interface{}) []string {
	if len(args) == 0 {
		return dst
	}
	limit := len(args)
	if limit > maxSQLArgs {
		limit = maxSQLArgs
	}
	for _, arg := range args[:limit] {
		dst = append(dst, sanitizeArg(arg))
	}
	if len(args) > limit {
		dst = append(dst, fmt.Sprintf("...(+%d more)", len(args)-limit))
	}
	return dst
}

func sanitizeArg(arg interface{}) string {
	switch v := arg.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "bool:true"
		}
		return "bool:false"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", v)
	case time.Time:
		return "time:" + v.UTC().Format(time.RFC3339)
	case []byte:
		return fmt.Sprintf("bytes(%d):***", len(v))
	case string:
		return fmt.Sprintf("string(%d):***", len(v))
	default:
		return "<redacted>"
	}
}
