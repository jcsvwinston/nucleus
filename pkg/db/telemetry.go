package db

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	dbTelemetryOnce sync.Once

	dbQueryTotal      metric.Int64Counter
	dbQueryErrors     metric.Int64Counter
	dbQueryDurationMs metric.Float64Histogram

	dbPoolOpenConnections   metric.Int64ObservableGauge
	dbPoolIdleConnections   metric.Int64ObservableGauge
	dbPoolInUseConnections  metric.Int64ObservableGauge
	dbPoolWaitCount         metric.Int64ObservableCounter
	dbPoolWaitDurationMs    metric.Float64ObservableCounter
	dbPoolCallbackRegister  metric.Registration
	dbPoolCallbackInitError error

	dbPoolsMu sync.Mutex
	dbPools   = make(map[*sql.DB][]attribute.KeyValue)
)

func initDBTelemetry() {
	dbTelemetryOnce.Do(func() {
		meter := otel.Meter("goframe/db")

		dbQueryTotal, _ = meter.Int64Counter("db.client.query.total")
		dbQueryErrors, _ = meter.Int64Counter("db.client.query.errors")
		dbQueryDurationMs, _ = meter.Float64Histogram("db.client.query.duration.ms")

		dbPoolOpenConnections, _ = meter.Int64ObservableGauge("db.client.pool.connections.open")
		dbPoolIdleConnections, _ = meter.Int64ObservableGauge("db.client.pool.connections.idle")
		dbPoolInUseConnections, _ = meter.Int64ObservableGauge("db.client.pool.connections.in_use")
		dbPoolWaitCount, _ = meter.Int64ObservableCounter("db.client.pool.wait.count")
		dbPoolWaitDurationMs, _ = meter.Float64ObservableCounter("db.client.pool.wait.duration.ms")

		dbPoolCallbackRegister, dbPoolCallbackInitError = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
			dbPoolsMu.Lock()
			snapshot := make([]struct {
				sqlDB *sql.DB
				attrs []attribute.KeyValue
			}, 0, len(dbPools))
			for sqlDB, attrs := range dbPools {
				copied := make([]attribute.KeyValue, len(attrs))
				copy(copied, attrs)
				snapshot = append(snapshot, struct {
					sqlDB *sql.DB
					attrs []attribute.KeyValue
				}{
					sqlDB: sqlDB,
					attrs: copied,
				})
			}
			dbPoolsMu.Unlock()

			for _, entry := range snapshot {
				stats := entry.sqlDB.Stats()
				opts := []metric.ObserveOption{metric.WithAttributes(entry.attrs...)}

				if dbPoolOpenConnections != nil {
					observer.ObserveInt64(dbPoolOpenConnections, int64(stats.OpenConnections), opts...)
				}
				if dbPoolIdleConnections != nil {
					observer.ObserveInt64(dbPoolIdleConnections, int64(stats.Idle), opts...)
				}
				if dbPoolInUseConnections != nil {
					observer.ObserveInt64(dbPoolInUseConnections, int64(stats.InUse), opts...)
				}
				if dbPoolWaitCount != nil {
					observer.ObserveInt64(dbPoolWaitCount, stats.WaitCount, opts...)
				}
				if dbPoolWaitDurationMs != nil {
					observer.ObserveFloat64(dbPoolWaitDurationMs, float64(stats.WaitDuration.Nanoseconds())/1e6, opts...)
				}
			}

			return nil
		}, dbPoolOpenConnections, dbPoolIdleConnections, dbPoolInUseConnections, dbPoolWaitCount, dbPoolWaitDurationMs)
	})
}

func dbTelemetryAttrs(dbSystem, dbEngine, operation string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	if strings.TrimSpace(dbSystem) != "" {
		attrs = append(attrs, attribute.String("db.system", dbSystem))
	}
	if strings.TrimSpace(dbEngine) != "" {
		attrs = append(attrs, attribute.String("db.engine", dbEngine))
	}
	if strings.TrimSpace(operation) != "" {
		attrs = append(attrs, attribute.String("db.operation", strings.ToLower(operation)))
	}
	return attrs
}

func recordDBQueryTelemetry(ctx context.Context, dbSystem, dbEngine, operation string, duration time.Duration, err error) {
	initDBTelemetry()
	if ctx == nil {
		ctx = context.Background()
	}

	attrs := dbTelemetryAttrs(dbSystem, dbEngine, normalizeSQLOperation(operation))
	addOpts := []metric.AddOption{metric.WithAttributes(attrs...)}
	recordOpts := []metric.RecordOption{metric.WithAttributes(attrs...)}

	if dbQueryTotal != nil {
		dbQueryTotal.Add(ctx, 1, addOpts...)
	}
	if dbQueryDurationMs != nil {
		dbQueryDurationMs.Record(ctx, float64(duration.Nanoseconds())/1e6, recordOpts...)
	}
	if err != nil && dbQueryErrors != nil {
		dbQueryErrors.Add(ctx, 1, addOpts...)
	}
}

func registerDBPoolTelemetry(sqlDB *sql.DB, dbSystem, dbEngine string) func() {
	if sqlDB == nil {
		return nil
	}
	initDBTelemetry()
	if dbPoolCallbackInitError != nil {
		return nil
	}

	attrs := dbTelemetryAttrs(dbSystem, dbEngine, "")
	dbPoolsMu.Lock()
	dbPools[sqlDB] = attrs
	dbPoolsMu.Unlock()

	return func() {
		dbPoolsMu.Lock()
		delete(dbPools, sqlDB)
		dbPoolsMu.Unlock()
	}
}

func dbSystemFromURL(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return "postgresql"
	case strings.HasPrefix(lower, "mysql://"):
		return "mysql"
	case strings.HasPrefix(lower, "sqlite://"), strings.HasSuffix(lower, ".db"), strings.HasSuffix(lower, ".sqlite"), lower == ":memory:":
		return "sqlite"
	default:
		return "unknown"
	}
}

func normalizeSQLOperation(raw string) string {
	op := strings.ToLower(strings.TrimSpace(raw))
	if op == "" {
		return "unknown"
	}
	switch op {
	case "select", "insert", "update", "delete", "create", "drop", "alter", "truncate", "with", "explain", "show", "pragma":
		return op
	default:
		return classifySQLOperation(op)
	}
}

func classifySQLOperation(sqlText string) string {
	trimmed := strings.TrimSpace(strings.ToLower(sqlText))
	if trimmed == "" {
		return "unknown"
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "unknown"
	}

	switch parts[0] {
	case "select", "insert", "update", "delete", "create", "drop", "alter", "truncate", "with", "explain", "show", "pragma":
		return parts[0]
	default:
		return "other"
	}
}

type bunTelemetryHook struct {
	dbSystem string
	dbEngine string
}

func newBunTelemetryHook(rawURL, dbEngine string) *bunTelemetryHook {
	return &bunTelemetryHook{
		dbSystem: dbSystemFromURL(rawURL),
		dbEngine: dbEngine,
	}
}

func (h *bunTelemetryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (h *bunTelemetryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	if event == nil {
		return
	}
	recordDBQueryTelemetry(
		ctx,
		h.dbSystem,
		h.dbEngine,
		event.Operation(),
		time.Since(event.StartTime),
		event.Err,
	)
}
