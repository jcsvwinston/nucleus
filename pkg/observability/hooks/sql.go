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

// NewSQLObserver returns a model.SQLQueryObserver that emits a
// SQLStatementEvent for every observed CRUD query, gated on
// HasSubscribers(KindSQLStatement).
//
// The observer pre-sanitizes argument values: strings and bytes become
// "type(len):***" markers; primitives are formatted "type:value"; times
// are RFC3339; nils are "null". The raw argument values are NEVER shipped.
func NewSQLObserver(cfg SQLObserverConfig) model.SQLQueryObserver {
	if cfg.Bus == nil {
		// No-op observer.
		return func(_ context.Context, _ model.SQLQueryEvent) {}
	}

	return func(ctx context.Context, q model.SQLQueryEvent) {
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
