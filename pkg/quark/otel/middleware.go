package otel

import (
	"context"
	"database/sql"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/quark"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/jcsvwinston/GoFrame/pkg/quark"

// Middleware implements quark.Middleware to provide native OpenTelemetry tracing.
type Middleware struct {
	quark.BaseMiddleware
	tracer trace.Tracer
}

// NewMiddleware creates a new OTel middleware for Quark.
func NewMiddleware() *Middleware {
	return &Middleware{
		tracer: otel.GetTracerProvider().Tracer(tracerName),
	}
}

func (m *Middleware) WrapExec(next quark.ExecFunc) quark.ExecFunc {
	return func(ctx context.Context, exec quark.Executor, sqlStr string, args []any) (res sql.Result, err error) {
		ctx, span := m.tracer.Start(ctx, "quark.exec", 
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.statement", sqlStr),
				attribute.String("db.operation", "EXEC"),
			),
		)
		defer func() {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}()

		return next(ctx, exec, sqlStr, args)
	}
}

func (m *Middleware) WrapQuery(next quark.QueryFunc) quark.QueryFunc {
	return func(ctx context.Context, exec quark.Executor, sqlStr string, args []any) (rows *sql.Rows, err error) {
		ctx, span := m.tracer.Start(ctx, "quark.query", 
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.statement", sqlStr),
				attribute.String("db.operation", "SELECT"),
			),
		)
		defer func() {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}()

		return next(ctx, exec, sqlStr, args)
	}
}

func (m *Middleware) WrapQueryRow(next quark.QueryRowFunc) quark.QueryRowFunc {
	return func(ctx context.Context, exec quark.Executor, sqlStr string, args []any) *sql.Row {
		ctx, span := m.tracer.Start(ctx, "quark.query_row", 
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.statement", sqlStr),
				attribute.String("db.operation", "SELECT_ROW"),
			),
		)
		// Note: spans for QueryRow are tricky as we don't see the error until Scan.
		defer span.End()

		return next(ctx, exec, sqlStr, args)
	}
}
