# Behaviour-Change Notice: SQLite stores `time.Time` in a format its date functions read, at v2.0.0

- ID: `DEP-2026-026`
- Status: `active`
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`; decision D8 of the owner, 2026-10-16)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is a
  **change of what the driver writes to the database**, the only one of the
  2.0 list that touches data already stored: no `// Deprecated:` marker
  carries it.
- Scope: `config` (driver behaviour)
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

A `time.Time` bound to a statement through `drivers/sqlite` (modernc.org/sqlite
with its default DSN) is stored as the text Go's `time.Time.String()`
produces — `2026-10-16 12:30:00 +0000 UTC` — which SQLite's own date
functions do not parse: `date(ts)` over the column is `NULL`, and a query
that filters or groups by date cannot be written in SQL against what the
framework stored (NU-110, seen by Orbit's Data Studio in A11 `O5`). The
driver reads its own text back correctly, so the defect is invisible until
SQL looks at the column.

What changes and when:

- **This release:** nothing in the framework. The way out exists in the
  driver: `_time_format=sqlite` on the DSN (`sqlite://app.db?_time_format=sqlite`)
  makes it write `2026-10-16 12:30:00+00:00`, which `date()` and `strftime()`
  read. This notice and the pinned test say so.
- **v2.0.0:** the framework sets the SQLite format by default on every SQLite
  URL it opens, and reads both forms — rows written before the upgrade stay
  readable. A documented, optional data migration (one `UPDATE` per table
  with a time column) rewrites existing rows for applications that want to
  query them by date.

## Affected Surfaces

- Applications on `drivers/sqlite` that store `time.Time` values and query
  them with SQLite's date functions, or that compare the stored text with
  values written by other tools.
- Not affected: PostgreSQL, MySQL/MariaDB, SQL Server and Oracle, whose
  drivers use native date types; applications that already set
  `_time_format=sqlite`.

## Migration Path

- Replacement: now, add `?_time_format=sqlite` to the SQLite URL in the
  configuration. New rows take the SQLite format; existing rows keep Go's and
  are still read back by the driver. To query old rows by date, rewrite them
  once:

  ```sql
  UPDATE t SET ts = strftime('%Y-%m-%d %H:%M:%f', substr(ts, 1, 19)) || '+00:00'
   WHERE ts LIKE '% +0000 UTC';
  ```

  (adapted to the zone the rows were written in).
- Behavior differences: at v2.0.0 the stored text changes shape for new rows;
  reading does not change; a tool that parsed the Go text itself must accept
  the SQLite text.
- Required app changes: one DSN parameter now, none at the upgrade; the data
  migration only where date queries over old rows matter.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-026-sqlite-time-format.md`
- Detection rule: `SELECT count(*) FROM t WHERE ts LIKE '% +0000 UTC'` (or
  the zone used) on each table with a time column; a SQLite URL without
  `_time_format`.
- Suggested rewrite: manual — the DSN parameter, then the optional
  `UPDATE`.

## Validation

- Compatibility tests updated: `yes` — `TestTimeFormat_GoTextUntilV2`
  (`drivers/sqlite/time_format_v1_test.go`) pins that the default DSN stores
  Go's text with `date()` returning `NULL`, that the value round-trips, and
  that `_time_format=sqlite` stores what `date()` reads. The 2.0 PR changes
  the first subtest in the same commit as the default.
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — before v2.0.0 nothing changes without the
  DSN parameter; after it, the driver reads both forms, so nothing written
  earlier is lost.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The umbrella guard reads this notice through its `Status` and «no earlier
than» lines and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 28).
