package cli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
)

func splitSQLStatements(script string) []string {
	var (
		statements []string
		buf        strings.Builder
		inSingle   bool
		inDouble   bool
		inLineCmt  bool
		inBlockCmt bool
	)

	for i := 0; i < len(script); i++ {
		ch := script[i]

		if inLineCmt {
			buf.WriteByte(ch)
			if ch == '\n' {
				inLineCmt = false
			}
			continue
		}
		if inBlockCmt {
			buf.WriteByte(ch)
			if ch == '*' && i+1 < len(script) && script[i+1] == '/' {
				buf.WriteByte('/')
				i++
				inBlockCmt = false
			}
			continue
		}
		if inSingle {
			buf.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(script) && script[i+1] == '\'' {
					buf.WriteByte('\'')
					i++
				} else {
					inSingle = false
				}
			}
			continue
		}
		if inDouble {
			buf.WriteByte(ch)
			if ch == '"' {
				if i+1 < len(script) && script[i+1] == '"' {
					buf.WriteByte('"')
					i++
				} else {
					inDouble = false
				}
			}
			continue
		}

		if ch == '-' && i+1 < len(script) && script[i+1] == '-' {
			buf.WriteString("--")
			i++
			inLineCmt = true
			continue
		}
		if ch == '/' && i+1 < len(script) && script[i+1] == '*' {
			buf.WriteString("/*")
			i++
			inBlockCmt = true
			continue
		}
		if ch == '\'' {
			buf.WriteByte(ch)
			inSingle = true
			continue
		}
		if ch == '"' {
			buf.WriteByte(ch)
			inDouble = true
			continue
		}
		if ch == ';' {
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			buf.Reset()
			continue
		}
		buf.WriteByte(ch)
	}

	last := strings.TrimSpace(buf.String())
	if last != "" {
		statements = append(statements, last)
	}
	return statements
}

func executeSQLScript(db *sql.DB, script string) error {
	statements := splitSQLStatements(script)
	return executeSQLStatements(db, statements)
}

func executeSQLStatements(db *sql.DB, statements []string) error {
	if len(statements) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func executeSQLStatement(ctx context.Context, db *sql.DB, statement string, out io.Writer) error {
	stmt := strings.TrimSpace(statement)
	if stmt == "" {
		return nil
	}

	if isQueryStatement(stmt) {
		rows, err := db.QueryContext(ctx, stmt)
		if err != nil {
			return err
		}
		defer rows.Close()
		return printRows(rows, out)
	}

	res, err := db.ExecContext(ctx, stmt)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	fmt.Fprintf(out, "OK (%d rows affected)\n", affected)
	return nil
}

func printRows(rows *sql.Rows, out io.Writer) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		fmt.Fprintln(out, "(no columns)")
		return nil
	}

	fmt.Fprintln(out, strings.Join(cols, "\t"))

	count := 0
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		cells := make([]string, len(cols))
		for i, v := range values {
			switch vv := v.(type) {
			case nil:
				cells[i] = "NULL"
			case []byte:
				cells[i] = string(vv)
			default:
				cells[i] = fmt.Sprint(vv)
			}
		}
		fmt.Fprintln(out, strings.Join(cells, "\t"))
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Fprintf(out, "(%d rows)\n", count)
	return nil
}

func isQueryStatement(stmt string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(stmt))
	if trimmed == "" {
		return false
	}

	keywords := []string{"select", "with", "show", "describe", "desc", "pragma", "explain"}
	for _, kw := range keywords {
		if strings.HasPrefix(trimmed, kw+" ") || trimmed == kw {
			return true
		}
	}
	return false
}
