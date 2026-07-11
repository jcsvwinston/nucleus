---
sidebar_position: 2
title: Compatibility policy
covers: []
config_keys: []
---

# Compatibility policy

Nucleus governs three stable surfaces by contract:

1. **Public Go API** — exported symbols in `pkg/*`.
2. **CLI** — registered commands, flags, and the JSON shape of their
   structured output.
3. **Configuration** — registered keys in `nucleus.yml`.

Each surface is pinned by automated tests under
[`contracts/`](https://github.com/jcsvwinston/nucleus/tree/main/contracts).
The freeze tests refuse to remove an entry without a deprecation record.

## Pre-1.0 vs. v1.x

| Period            | What you can rely on                                  |
| ----------------- | ----------------------------------------------------- |
| pre-1.0 (`v0.x`)  | The contracts exist and prevent silent regressions, but minor versions may make documented breaking changes. Read `CHANGELOG.md` between upgrades. |
| `v1.x`            | The full Compatibility SLO applies. See [`docs/governance/COMPATIBILITY_SLO.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/governance/COMPATIBILITY_SLO.md). |

The clock on the SLO started at `v1.0.0` (tagged 2026-07-10): the `v1.x`
row is the one that applies today. The pre-1.0 row remains as history of
how the contracts were run before the major.

## What "frozen" means

For each surface, the corresponding baseline lives under
`contracts/baseline/`:

| Baseline                          | What it pins                              |
| --------------------------------- | ----------------------------------------- |
| `api_exported_symbols.txt`        | The list of exported symbols in `pkg/*`.  |
| `cli_primary_commands.txt`        | The list of stable CLI commands.          |
| `cli_json_status_keys.txt`        | The JSON keys returned by status commands. |
| `config_key_patterns.txt`         | The valid `nucleus.yml` key paths.        |

The freeze tests fail loudly on any net removal. The baselines are
**never edited to make a test pass** — they are regenerated only as
part of an explicit, documented contract change.

## How a deprecation works

A deprecation goes through three stages:

1. **Marked** — the symbol / command / key is annotated as deprecated;
   it still works. A row is added to
   [`docs/governance/DEPRECATION_TEMPLATE.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/governance/DEPRECATION_TEMPLATE.md)
   with the planned removal release.
2. **Warned** — the runtime / CLI emits a structured warning when the
   deprecated entry is used.
3. **Removed** — at the planned release. The freeze test now passes
   without the entry. The CHANGELOG records the removal under the
   appropriate version heading.

A migration assistant accompanies any v1.x → v(1+N) deprecation that
affects user code, per
[`docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md).

## Type firewall

Critical third-party dependencies are kept behind framework interfaces.
A dedicated test (`contracts/firewall_test.go`) AST-parses `pkg/*` and
fails if a non-stdlib type leaks into a stable signature.

That is what makes the SQL-driver swap drill possible: switching from
SQLite to PostgreSQL to MySQL is a config change, not a code change,
because no `*sql.DB`-adjacent third-party type appears in any exported
signature.

## Reading the registry

Rather than reading the contract baselines directly, three documents in
`docs/reference/` are kept canonical:

- [`API_CONTRACT_INVENTORY.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/API_CONTRACT_INVENTORY.md)
- [`CLI_CONTRACT_MATRIX.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CLI_CONTRACT_MATRIX.md)
- [`CONFIG_KEY_REGISTRY.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md)

These are kept in lockstep with the baselines by the iteration workflow
described in [`CLAUDE.md`](https://github.com/jcsvwinston/nucleus/blob/main/CLAUDE.md).
