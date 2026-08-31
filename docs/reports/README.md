# Reports Archive

Generated compatibility / dependency / stability reports, frozen as of
their date. Dated files are actas and are never edited; the drift guard
excludes this directory on purpose. Report generation moved with the
release flow — current runs are produced by
`scripts/release/rehearse_rc.sh` into `dist/reports/` and only promoted
here when a round wants a permanent record.

**Read this one first:**

- `compatibility_harness_latest.md` — the most recent promoted
  compatibility-harness run.
- `mssql_oracle_stability_report.md` — the consolidated (latest) MSSQL /
  Oracle stability report; the two dated `mssql_oracle_stability_2026-05-*`
  files are earlier snapshots of the same investigation, kept as record.

**Dated snapshots:** `compatibility_report_*.md`,
`dependency_impact_*.md`, `dependency_critical_review_*.md`,
`dependency_impact_aws_sdk_2026-05-14.md` — one per release rehearsal or
review round, newest date wins.
