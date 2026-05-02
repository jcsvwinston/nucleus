package quark

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

// SQLQueryLogger is a QueryObserver that logs SQL statements using slog.
type SQLQueryLogger struct {
	logger *slog.Logger
}

func NewSQLQueryLogger(l *slog.Logger) *SQLQueryLogger {
	if l == nil {
		l = slog.Default()
	}
	return &SQLQueryLogger{logger: l}
}

func (o *SQLQueryLogger) ObserveQuery(e QueryEvent) {
	o.logger.Info("SQL Execution",
		"op", e.Operation,
		"sql", e.SQL,
		"args", e.Args,
		"duration", e.Duration,
		"rows", e.Rows,
		"error", e.Error,
	)
}

func TestSQLLogging(t *testing.T) {
	// 1. Create a logger (here we use a text handler to see it clearly in console)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()

	// 2. Inject the logger as a QueryObserver
	sqlLogger := NewSQLQueryLogger(logger)
	client, _ := New(db, WithDialect(SQLite()), WithQueryObserver(sqlLogger))

	ctx := context.Background()
	type LogUser struct {
		ID   int64  `db:"id" pk:"true"`
		Name string `db:"name"`
	}

	client.Migrate(ctx, &LogUser{})

	// 3. Perform operations and watch the console
	For[LogUser](ctx, client).Create(&LogUser{Name: "Loggy"})
	For[LogUser](ctx, client).Where("name", "=", "Loggy").List()
}
