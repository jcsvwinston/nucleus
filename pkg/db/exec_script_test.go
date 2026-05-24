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

func TestExecScript_NonOraclePassthrough(t *testing.T) {
	// Non-Oracle dialects send the whole (possibly multi-statement) script in
	// one Exec — the driver handles `;`-separation. ExecScript must not split.
	ex := &captureExecer{}
	script := "CREATE TABLE t (id INT);\nCREATE INDEX ix_t ON t (id);\n"
	if err := ExecScript(ex, "postgresql", script); err != nil {
		t.Fatalf("ExecScript: %v", err)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("non-oracle must be a single passthrough Exec, got %d calls", len(ex.calls))
	}
	if ex.calls[0] != script {
		t.Errorf("non-oracle script must pass through verbatim")
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
