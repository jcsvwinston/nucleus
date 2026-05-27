package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// captureExecer records each Exec call's query and can be primed to fail.
type captureExecer struct {
	calls   []string
	failOn  int // 1-based index of the call that returns failErr; 0 = never
	failErr error
}

func (c *captureExecer) Exec(query string, args ...any) (sql.Result, error) {
	c.calls = append(c.calls, query)
	if c.failOn != 0 && len(c.calls) == c.failOn {
		return nil, c.failErr
	}
	return nil, nil
}

// twoBlockOracleScript mirrors the exact shape `model.writeOraclePLSQLBlock`
// emits (multi-line EXCEPTION/WHEN OTHERS + a trailing `/` per block), kept in
// sync by hand to avoid a pkg/db→pkg/model import cycle in tests.
const twoBlockOracleScript = "BEGIN\n\tEXECUTE IMMEDIATE 'CREATE TABLE t (id NUMBER)';\nEXCEPTION\n\tWHEN OTHERS THEN\n\t\tIF SQLCODE != -955 THEN RAISE; END IF; -- table already exists\nEND;\n/\n" +
	"BEGIN\n\tEXECUTE IMMEDIATE 'CREATE INDEX ix_t ON t (id)';\nEXCEPTION\n\tWHEN OTHERS THEN\n\t\tIF SQLCODE != -955 THEN RAISE; END IF; -- index already exists\nEND;\n/\n"

func TestExecScript_OracleSplitsOnSlash(t *testing.T) {
	ex := &captureExecer{}
	if err := ExecScript(ex, "oracle", twoBlockOracleScript); err != nil {
		t.Fatalf("ExecScript: %v", err)
	}
	if len(ex.calls) != 2 {
		t.Fatalf("expected 2 Exec calls (one per block), got %d: %#v", len(ex.calls), ex.calls)
	}
	for i, call := range ex.calls {
		if strings.Contains(call, "\n/") || strings.TrimSpace(call) == "/" {
			t.Errorf("call %d still contains a `/` separator (go-ora rejects it):\n%s", i, call)
		}
		if !strings.HasPrefix(call, "BEGIN") || !strings.HasSuffix(call, "END;") {
			t.Errorf("call %d is not a clean BEGIN…END; block:\n%s", i, call)
		}
	}
	if !strings.Contains(ex.calls[0], "CREATE TABLE") || !strings.Contains(ex.calls[1], "CREATE INDEX") {
		t.Errorf("blocks executed out of order or malformed: %#v", ex.calls)
	}
}

func TestExecScript_OracleSingleBlock(t *testing.T) {
	ex := &captureExecer{}
	script := "BEGIN\n\tEXECUTE IMMEDIATE 'CREATE TABLE t (id NUMBER)';\nEND;\n/\n"
	if err := ExecScript(ex, "oracle", script); err != nil {
		t.Fatalf("ExecScript: %v", err)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(ex.calls))
	}
}

func TestExecScript_OracleNoSeparator(t *testing.T) {
	// A one-statement script with no `/` is executed as a single unit.
	ex := &captureExecer{}
	if err := ExecScript(ex, "oracle", "CREATE TABLE t (id NUMBER)"); err != nil {
		t.Fatalf("ExecScript: %v", err)
	}
	if len(ex.calls) != 1 || !strings.Contains(ex.calls[0], "CREATE TABLE") {
		t.Fatalf("expected single passthrough call, got %#v", ex.calls)
	}
}

func TestExecScript_PostgresAndMSSQLPassthrough(t *testing.T) {
	// PostgreSQL and SQL Server accept a multi-statement batch in a single
	// Exec, so ExecScript must NOT split for them — the whole script passes
	// through verbatim in one round trip.
	script := "CREATE TABLE t (id INT);\nCREATE INDEX ix_t ON t (id);\n"
	for _, system := range []string{"postgresql", "mssql"} {
		ex := &captureExecer{}
		if err := ExecScript(ex, system, script); err != nil {
			t.Fatalf("ExecScript(%s): %v", system, err)
		}
		if len(ex.calls) != 1 {
			t.Fatalf("%s must be a single passthrough Exec, got %d calls", system, len(ex.calls))
		}
		if ex.calls[0] != script {
			t.Errorf("%s script must pass through verbatim", system)
		}
	}
}

func TestExecScript_MySQLAndSQLiteSplitStatements(t *testing.T) {
	// go-sql-driver/mysql (no multiStatements) and the modernc SQLite driver
	// reject a multi-statement batch, so ExecScript must split a CREATE TABLE
	// + CREATE INDEX scaffold into one Exec per statement, in order, with the
	// terminating `;` stripped.
	script := "CREATE TABLE `t` (id INT);\nCREATE INDEX `ix_t` ON `t` (id);\n"
	for _, system := range []string{"mysql", "sqlite"} {
		ex := &captureExecer{}
		if err := ExecScript(ex, system, script); err != nil {
			t.Fatalf("ExecScript(%s): %v", system, err)
		}
		if len(ex.calls) != 2 {
			t.Fatalf("%s must split into 2 Execs, got %d: %#v", system, len(ex.calls), ex.calls)
		}
		if !strings.HasPrefix(ex.calls[0], "CREATE TABLE") || !strings.HasPrefix(ex.calls[1], "CREATE INDEX") {
			t.Errorf("%s statements out of order: %#v", system, ex.calls)
		}
		for i, call := range ex.calls {
			if strings.Contains(call, ";") {
				t.Errorf("%s call %d retained a `;` terminator: %q", system, i, call)
			}
		}
	}
}

func TestExecScript_MySQLStopsOnError(t *testing.T) {
	ex := &captureExecer{failOn: 1, failErr: errors.New("Error 1064")}
	script := "CREATE TABLE t (id INT);\nCREATE INDEX ix_t ON t (id);\n"
	if err := ExecScript(ex, "mysql", script); err == nil {
		t.Fatal("expected the first statement's error to propagate")
	}
	if len(ex.calls) != 1 {
		t.Fatalf("execution must stop after the failing statement; got %d calls", len(ex.calls))
	}
}

// TestExecScript_SQLiteRealMultiStatement is the regression test for the live
// MySQL AutoMigrate failure (Error 1064 on a CREATE TABLE + CREATE INDEX
// batch). The modernc SQLite driver has the same one-statement-per-Exec
// limitation, so it reproduces the bug on a real driver without a container:
// before the fix this errored; after it, both statements apply.
func TestExecScript_SQLiteRealMultiStatement(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	script := "CREATE TABLE t (id INTEGER PRIMARY KEY, email TEXT);\n" +
		"CREATE INDEX idx_t_email ON t (email);\n"
	if err := ExecScript(sqlDB, "sqlite", script); err != nil {
		t.Fatalf("ExecScript on live sqlite: %v", err)
	}

	var name string
	if err := sqlDB.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_t_email'`,
	).Scan(&name); err != nil {
		t.Fatalf("expected the secondary index to be created: %v", err)
	}
	if name != "idx_t_email" {
		t.Fatalf("unexpected index name: %q", name)
	}
}

func TestSplitSQLStatements(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		{"two statements", "CREATE TABLE t (id INT);\nCREATE INDEX ix ON t (id);\n",
			[]string{"CREATE TABLE t (id INT)", "CREATE INDEX ix ON t (id)"}},
		{"trailing statement without semicolon", "SELECT 1;\nSELECT 2",
			[]string{"SELECT 1", "SELECT 2"}},
		{"semicolon inside single-quoted literal", "INSERT INTO t VALUES ('a;b');\nSELECT 1;",
			[]string{"INSERT INTO t VALUES ('a;b')", "SELECT 1"}},
		{"escaped quote then semicolon", "INSERT INTO t VALUES ('it''s; fine');\nSELECT 1;",
			[]string{"INSERT INTO t VALUES ('it''s; fine')", "SELECT 1"}},
		{"backslash-escaped quote (mysql)", "INSERT INTO t VALUES ('a\\'; b');\nSELECT 1;",
			[]string{"INSERT INTO t VALUES ('a\\'; b')", "SELECT 1"}},
		{"semicolon in line comment", "SELECT 1; -- drop ; here\nSELECT 2;",
			[]string{"SELECT 1", "-- drop ; here\nSELECT 2"}},
		{"semicolon in block comment", "SELECT 1 /* a ; b */;\nSELECT 2;",
			[]string{"SELECT 1 /* a ; b */", "SELECT 2"}},
		{"semicolon in backtick identifier", "CREATE TABLE `we;ird` (id INT);",
			[]string{"CREATE TABLE `we;ird` (id INT)"}},
		{"CRLF normalised inside statement", "CREATE TABLE t (\r\n id INT\r\n);\r\nSELECT 1;",
			[]string{"CREATE TABLE t (\n id INT\n)", "SELECT 1"}},
		{"empty and whitespace only", "  ;\n\n;  ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSQLStatements(tc.script)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements %#v, want %d %#v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("statement %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestExecScript_OracleStopsOnError(t *testing.T) {
	ex := &captureExecer{failOn: 1, failErr: errors.New("ORA-boom")}
	err := ExecScript(ex, "oracle", twoBlockOracleScript)
	if err == nil {
		t.Fatal("expected the first block's error to propagate")
	}
	if len(ex.calls) != 1 {
		t.Fatalf("execution must stop after the failing block; got %d calls", len(ex.calls))
	}
}

func TestSplitOracleStatements(t *testing.T) {
	got := splitOracleStatements(twoBlockOracleScript)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
	// Trailing/leading whitespace trimmed; empty trailing unit (after the last
	// `/`) dropped; no `/` markers retained.
	for _, s := range got {
		if strings.Contains(s, "\n/\n") || s == "/" {
			t.Errorf("split unit retained a `/` marker:\n%s", s)
		}
	}
	// Empty / whitespace-only scripts yield no statements.
	if n := len(splitOracleStatements("\n  \n/\n\n")); n != 0 {
		t.Errorf("expected 0 statements from an empty script, got %d", n)
	}
}

func TestSplitOracleStatements_CRLF(t *testing.T) {
	// A Windows-authored file (CRLF) must split identically and leave no `\r`
	// in the emitted block content.
	crlf := strings.ReplaceAll(twoBlockOracleScript, "\n", "\r\n")
	got := splitOracleStatements(crlf)
	if len(got) != 2 {
		t.Fatalf("CRLF: expected 2 statements, got %d", len(got))
	}
	for i, s := range got {
		if strings.Contains(s, "\r") {
			t.Errorf("CRLF: statement %d retained a carriage return:\n%q", i, s)
		}
	}
}

func TestSplitOracleStatements_SlashInLongerLineNotASeparator(t *testing.T) {
	// A `/` that is part of a longer line (e.g. an arithmetic expression) is
	// NOT a block separator — only a `/` alone on its own line is.
	script := "BEGIN\n\tEXECUTE IMMEDIATE 'CREATE TABLE t (ratio NUMBER)'; -- 1/2 default\n\tx := a / b;\nEND;\n/\n"
	got := splitOracleStatements(script)
	if len(got) != 1 {
		t.Fatalf("expected 1 statement (inline `/` must not split), got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "a / b") || !strings.Contains(got[0], "1/2") {
		t.Errorf("inline slashes were dropped or mis-split:\n%s", got[0])
	}
}
