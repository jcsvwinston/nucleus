# Exploratory SQL Stability Report

Reference date: 2026-04-23.
Status: Current.

This report documents the stability of exploratory SQL drivers (MSSQL and Oracle) in the CI matrix.

> **Note:** Since 2026-04-23, MSSQL and Oracle drivers are behind build tags.
> Tests require `-tags mssql` or `-tags oracle` respectively.

## Summary

| Database | Runs | Success Rate | Status |
|----------|------|-------------|--------|
| MS SQL Server | 10 | 100% | Stable |
| Oracle | 10 | 100% | Stable |

## Details

Both MSSQL and Oracle exploratory drivers have passed all 10 stability runs with 100% success rate.

### Test Coverage

The following CLI commands have been validated across both exploratory engines:

- `createcachetable` — idempotency validated
- `sqlflush` and `flush --dry-run` — output validated
- `sqlsequencereset` — output validated (Oracle emits concrete reset SQL for `<table>_SEQ` and `<table>_ID_SEQ` patterns)
- `migrate status` — migration tracking validated
- `shell --sandbox` — read-only SQL execution validated

### Promotion Criteria

Exploratory drivers may be promoted to first-class stable contracts when:

1. 100% success rate maintained over 30 consecutive CI runs.
2. All core CLI commands pass without engine-specific workarounds.
3. No unresolved issues in the driver's GitHub tracker.

## Historical Notes

- Original exploratory_stability.md (2026-04-07): Oracle showed 0% success rate due to connection string formatting issues.
- Post-fix (2026-04-08): Both MSSQL and Oracle achieved 100% success across 10 runs.
- 2026-04-23: MSSQL and Oracle drivers moved behind build tags (`-tags mssql`, `-tags oracle`). Tests and CI lanes updated to require explicit tags.
- This report supersedes the original exploratory_stability.md and the postfix variants.

