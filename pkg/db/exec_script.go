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
// MySQL (`mysql`) and SQLite (`sqlite`): the go-sql-driver/mysql driver (unless
// the DSN sets multiStatements=true) and the pure-Go modernc SQLite driver
// execute exactly one statement per Exec and reject a multi-statement batch
// (MySQL fails the second statement with Error 1064). A scaffold for a model
// with a secondary index emits CREATE TABLE plus one CREATE INDEX per index,
// so ExecScript splits the script into its `;`-terminated statements and Execs
// each in order. The split is quote- and comment-aware (see splitSQLStatements)
// so a `;` inside a string literal or comment is not a false boundary.
//
// PostgreSQL (`postgresql`) and SQL Server (`mssql`): the script is sent as-is
// in a single Exec — the pgx/lib-pq and go-mssqldb drivers accept multiple
// `;`-separated statements in one round trip, and this preserves the
// established behaviour exactly.
//
// execer is satisfied by `*sql.DB` and `*sql.Tx` (the interface is unexported
// because those are the only intended arguments). Oracle splitting is
// line-oriented on the SQL*Plus convention: a `/` alone on its own line inside
// a multi-line PL/SQL string literal would be mistaken for a separator — the
// same constraint SQL*Plus itself has. Framework scaffolds never emit such a
// line.
func ExecScript(execer sqlExecer, system, script string) error {
	switch system {
	case "oracle":
		return execEach(execer, splitOracleStatements(script))
	case "mysql", "sqlite":
		return execEach(execer, splitSQLStatements(script))
	default:
		_, err := execer.Exec(script)
		return err
	}
}

// execEach Execs each statement in order, stopping on the first error.
func execEach(execer sqlExecer, stmts []string) error {
	for _, stmt := range stmts {
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

// splitSQLStatements splits a `;`-terminated SQL script into individually
// executable statements for drivers that reject a multi-statement batch
// (go-sql-driver/mysql without multiStatements, and the modernc SQLite
// driver). It is quote- and comment-aware: a `;` inside a single-quoted
// string, a "double-quoted" or `backtick`-quoted identifier, a `--` line
// comment, or a `/* */` block comment is NOT a statement boundary. Returned
// statements are trimmed and non-empty, with the terminating `;` removed.
//
// Delimiters and comment markers are all ASCII, so the scan is byte-oriented;
// multi-byte UTF-8 inside a literal is copied through unchanged. Single-quoted
// strings honour both the SQL-standard doubled-single-quote escape and the
// MySQL backslash escape, so a `;` inside such a literal is never mistaken for
// a terminator.
func splitSQLStatements(script string) []string {
	// Normalise CRLF so a Windows-authored migration file does not leave a
	// stray `\r` inside a split statement (harmless to the driver, but keeps
	// the statement text clean and matches splitOracleStatements).
	script = strings.ReplaceAll(script, "\r\n", "\n")
	var out []string
	var b strings.Builder
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
		b.Reset()
	}

	var inSingle, inDouble, inBacktick, inLine, inBlock bool
	for i := 0; i < len(script); i++ {
		c := script[i]
		var next byte
		if i+1 < len(script) {
			next = script[i+1]
		}

		switch {
		case inLine:
			b.WriteByte(c)
			if c == '\n' {
				inLine = false
			}
		case inBlock:
			b.WriteByte(c)
			if c == '*' && next == '/' {
				b.WriteByte(next)
				i++
				inBlock = false
			}
		case inSingle:
			b.WriteByte(c)
			if c == '\\' && next != 0 {
				b.WriteByte(next)
				i++
			} else if c == '\'' {
				if next == '\'' {
					b.WriteByte(next)
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				if next == '"' {
					b.WriteByte(next)
					i++
				} else {
					inDouble = false
				}
			}
		case inBacktick:
			b.WriteByte(c)
			if c == '`' {
				inBacktick = false
			}
		default:
			// For the two-byte openers below we write only the first byte
			// here; the second byte is written on the next iteration by the
			// now-active inLine/inBlock branch.
			switch {
			case c == '-' && next == '-':
				inLine = true
				b.WriteByte(c)
			case c == '/' && next == '*':
				inBlock = true
				b.WriteByte(c)
			case c == '\'':
				inSingle = true
				b.WriteByte(c)
			case c == '"':
				inDouble = true
				b.WriteByte(c)
			case c == '`':
				inBacktick = true
				b.WriteByte(c)
			case c == ';':
				flush()
			default:
				b.WriteByte(c)
			}
		}
	}
	flush()
	return out
}
