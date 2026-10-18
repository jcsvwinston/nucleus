# Migration Assistant: SQLite `time.Time` storage from Go's text to the SQLite format

- ID: `MA-2026-026`
- Pairs with: `docs/deprecations/DEP-2026-026-sqlite-time-format.md`
- Severity: `high` (it changes what is written to the database; existing rows
  stay readable, the optional data migration rewrites them).
- Status: `current`

---

## Scope

Applications on `drivers/sqlite` that store `time.Time` values. Since
v2.0.0 the framework opens every SQLite URL with `_time_format=sqlite`
unless the URL names a format itself, so the DSN step below is only for an
application still on v1; the data migration is the same on both sides. Out
of scope: every other engine; SQLite URLs that already carry
`_time_format`.

## Detection

```bash
grep -n 'sqlite://' nucleus.yml config/*.yml 2>/dev/null | grep -v '_time_format'
sqlite3 app.db "SELECT name FROM sqlite_master WHERE type='table'" | while read -r t; do
  sqlite3 app.db "SELECT '$t', count(*) FROM $t WHERE typeof(created_at)='text' AND created_at LIKE '% +0000 UTC'" 2>/dev/null
done
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `sqlite://app.db` on v1 | `sqlite://app.db?_time_format=sqlite` — new rows take the SQLite format now, old rows keep Go's and are still read; on v2.0.0 the plain URL already does this | manual (one DSN parameter, v1 only) |
| rows in Go's text, to be queried by date | one `UPDATE` per time column, adapted to the zone the rows were written in | manual, optional |

```sql
UPDATE t SET ts = strftime('%Y-%m-%d %H:%M:%f', substr(ts, 1, 19)) || '+00:00'
 WHERE ts LIKE '% +0000 UTC';
```

Take a copy of the file before the `UPDATE` (`cp app.db app.db.bak`): it
rewrites text in place.

## Verification

```bash
sqlite3 app.db "SELECT date(ts) FROM t LIMIT 3"     # dates, not NULL
go test ./...
```

## Rollback

Restore the copy taken before the `UPDATE`. On v1, drop the DSN parameter
to write Go's text again; on v2.0.0, `?_time_format=datetime` or any other
format the driver names is kept as written, and the driver reads both forms,
so a downgrade reads the SQLite-format rows too.
