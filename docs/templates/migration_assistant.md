# Migration Assistant: <title>

- ID: `MA-YYYY-NNN`
- Related deprecation: `DEP-YYYY-NNN` (optional)
- Severity: `low|medium|high`
- Scope: `api|cli|config|plugin|mixed`
- Owner: `<team or maintainer>`

## Scope

<describe what needs migration>

## Detection

- Pattern 1: `<pattern>`
- Pattern 2: `<pattern>`

Example detection commands:

```bash
rg -n "<old symbol or key>" .
goframe diffsettings --all --json
```

## Rewrite Plan

| Old | New | Mode | Notes |
| --- | --- | --- | --- |
| `<old>` | `<new>` | `auto|manual` | `<details>` |

## Verification

```bash
go test ./...
bash scripts/release/generate_compatibility_report.sh --enforce-threshold
```

## Rollback

<how to revert safely>
