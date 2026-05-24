package db

import (
	"database/sql"
	"strings"
)

// sqlExecer is the subset of *sql.DB / *sql.Tx that ExecScript needs. Both the
// AutoMigrate path (which holds a *sql.DB) and the file Migrator (which holds a
// *sql.Tx) satisfy it, so ExecScript works inside or outside a transaction.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// oracleStatementSeparator is the line that separates independently-executable
// units in an Oracle migration script. It is the SQL*Plus / SQLcl PL/SQL
// terminator: a single `/` on its own line. ExecScript splits on it and strips
// it before Exec — see ExecScript for why the marker is never sent to the
// driver.
const oracleStatementSeparator = "/"

// ExecScript executes a (possibly multi-statement) migration script on execer,
// splitting it into individually-executable units per the SQL dialect.
//
// Oracle (`oracle`): the go-ora driver executes exactly one statement / PL/SQL
// block per Exec and raises ORA-06550 on a SQL*Plus `/` terminator. Framework
// scaffolds (and idiomatic hand-written Oracle migrations) therefore separate
// PL/SQL blocks with a `/` on its own line. ExecScript splits on those `/`
// lines, drops the marker, and Execs each block in order — so the `/` is a
// split directive that is never sent to the driver. (Oracle DDL auto-commits,
// so per-block execution is the only correct path regardless of any
// surrounding transaction.)
//
// All other dialects: the script is sent as-is in a single Exec — the
// pq / mysql / sqlserver drivers accept multiple `;`-separated statements, and
// this preserves the established behaviour exactly. (A general splitter for the
// pure-Go SQLite/modernc multi-statement limitation is a possible future
// extension; it is out of scope here.)
//
// execer is satisfied by `*sql.DB` and `*sql.Tx` (the interface is unexported
// because those are the only intended arguments). Splitting is line-oriented
// on the SQL*Plus convention: a `/` alone on its own line inside a multi-line
// PL/SQL string literal would be mistaken for a separator — the same
// constraint SQL*Plus itself has. Framework scaffolds never emit such a line.
func ExecScript(execer sqlExecer, system, script string) error {
	if system != "oracle" {
		_, err := execer.Exec(script)
		return err
	}
	for _, stmt := range splitOracleStatements(script) {
		if _, err := execer.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// splitOracleStatements splits an Oracle migration script into executable units
// on lines containing only `/` (after trimming surrounding whitespace), and
// returns the trimmed, non-empty units WITHOUT the `/` markers. A script with
// no `/` lines yields a single unit (the whole trimmed script) — the common
// case for a one-statement migration, unchanged from a plain Exec.
func splitOracleStatements(script string) []string {
	// Normalise CRLF so a Windows-authored migration file does not leave a
	// stray `\r` on every block line (the `/` separator survives TrimSpace
	// either way, but the block content sent to go-ora must be clean).
	script = strings.ReplaceAll(script, "\r\n", "\n")
	var out []string
	var b strings.Builder
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
		b.Reset()
	}
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(line) == oracleStatementSeparator {
			flush()
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	return out
}
