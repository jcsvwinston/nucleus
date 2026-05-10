# ADR-003: Project Identity — Nucleus

**Status:** Accepted
**Date:** 2026-05-09
**Superseded:** No

## Context

The framework was developed under the working name `GoFrame`. It has never been published to a module registry, no external user has adopted it, no third-party plugin has been distributed against its plugin contract, and no compatibility SLO has begun to accrue. The repository is at pre-v1 stage (`v0.5.x`), and `docs/governance/COMPATIBILITY_SLO.md` does not yet apply — its clock starts at v1.0.

Two factors converge to make a rename uniquely cheap right now:

1. **No legacy to preserve.** Without external consumers, every "compatibility" mechanism the framework already carries — Django-style command aliases (ADR-002), the legacy `goframe-mail-<driver>` plugin discovery fallback, the freeze tests in `contracts/freeze_test.go` — defends a pre-launch surface, not a published one. There is nothing to be backward-compatible *with*.

2. **Contract baselines can be re-cut for free.** The four files under `contracts/baseline/` (`api_exported_symbols.txt`, `cli_primary_commands.txt`, `cli_json_status_keys.txt`, `config_key_patterns.txt`) define the surfaces the freeze test refuses to remove. Once v1.0 ships, regenerating them becomes a breaking-change event. Today, it is a build step.

The framework's identity is now consolidating around a single brand — **Nucleus** — distinct from the migrated-out `quark` ORM module (`github.com/jcsvwinston/quark`), and aligned with the framework's role as the structural core of an application.

## Decision

The framework is renamed from `GoFrame` to `Nucleus`. The rename is executed as a single coordinated change with no transition aliases.

**Naming surface:**

| Surface | Before | After |
|---|---|---|
| Module path | `github.com/jcsvwinston/GoFrame` | `github.com/jcsvwinston/nucleus` |
| Binary / CLI command | `goframe` | `nucleus` |
| Binary directory | `cmd/goframe/` | `cmd/nucleus/` |
| Public fluent package | `pkg/fluent` (entry: `fluent.New()`) | `pkg/nucleus` (entry: `nucleus.New()`) |
| Canonical config file | `goframe.yaml` | `nucleus.yml` (extension changes from `.yaml` to `.yml`) |
| Test config file | `goframe-test.yaml` | `nucleus-test.yml` |
| Plugin discovery prefix | `goframe-plugin-<provider>` | `nucleus-plugin-<provider>` |
| Legacy plugin fallback | `goframe-mail-<driver>` (transitional) | **Removed** |
| Brand (docs, UI titles) | `GoFrame` | `Nucleus` |
| Lowercase identifier (paths, packages, env prefixes) | `goframe` | `nucleus` |

**Compatibility policy:**

- No transition aliases. The CLI does not accept `goframe ...`, the loader does not look for `goframe.yaml`, the plugin host does not probe `goframe-plugin-*`. A user with the previous source tree must rename, not coexist.
- Django-style CLI aliases unrelated to the rename (`runserver`, `startproject`, `makemigrations`, etc.) are preserved as defined by ADR-002.

**Contract baseline policy:**

- The four files in `contracts/baseline/` are regenerated from the post-rename code. The freeze test inaugurates its meaningful life against the Nucleus baseline. The baseline reset is recorded in this ADR and in `CHANGELOG.md`.

**Governance side-effects bundled with this change:**

- ADR-001 (stdlib-first runtime design) and ADR-002 (Django-inspired CLI design) are extracted from `docs/adrs/README.md` into standalone files (`ADR-001-stdlib-first.md`, `ADR-002-django-cli.md`), restoring the convention the README already advertises.
- A `MIGRATION_ASSISTANT` artifact is **not** produced for this rename. Migration assistants exist to help external consumers; there are none.
- The release that incorporates this rename is tagged `v0.1.0` as the first release under the Nucleus name. The pre-rename `v0.5.x` series is left in commit history without further tags.

## Consequences

### Positive

- A single, consistent identity across module path, binary, package, config, plugin convention, and brand. Today's drift (e.g. README importing a non-existent `pkg/goframe`) is resolved by construction.
- Compatibility SLO and contract-freeze infrastructure begin operating against the actual published surface, not against a pre-publication scaffold.
- Removal of the `goframe-mail-<driver>` legacy fallback simplifies `pkg/plugins` and removes a transitional concept that never had real users.
- The `docs/adrs/`, `docs/deprecations/`, and `docs/migration_assistants/` directories begin to fill with real artifacts (this ADR, plus extracted ADR-001/002), addressing the governance gap noted in the pre-rename audit.

### Negative

- The pre-rename git history references `GoFrame`. This is accepted; commit messages are not rewritten.
- The repository name on GitHub may need to change separately. GitHub auto-redirects clones, but `pkg.go.dev/github.com/jcsvwinston/GoFrame` will remain visible as a stale entry until search indexes refresh. No action is taken to remove it.
- The CHANGELOG retains the pre-rename entries under `v0.5.x` for historical reference, with a single top-level "Renamed: GoFrame → Nucleus" entry under the new `v0.1.0` heading.

### Neutral

- Three packages that the modularization effort (Phase 4, deferred) hoped to split into independent modules — `pkg/storage`, `pkg/tasks`, `pkg/authz` — remain inside the renamed module. The rename does not advance or block that future split.

## Compliance

After this ADR is accepted:

1. No code, documentation, configuration, or scaffold template under the new repository state references `GoFrame` or `goframe` except in:
   - `CHANGELOG.md` historical entries
   - This ADR
   - The release entry that records the rename
2. `pkg/plugins` recognizes only `nucleus-plugin-*` prefixes.
3. `internal/cli` scaffold templates emit `nucleus.yml` and a `nucleus`-shaped project.
4. `contracts/baseline/*` is regenerated and the freeze test passes against the new baseline before the rename PR is merged.
5. CI passes on the new module path: `go vet`, `go build ./...`, `go test ./...`, race tests, admin UI build, freeze + firewall, compatibility harness.

## Related

- ADR-001: stdlib-First Runtime Design
- ADR-002: Django-Inspired CLI Design
- `SPEC.md` (rewritten as part of the rename)
- `docs/governance/COMPATIBILITY_SLO.md`
- `contracts/baseline/`
- Pre-rename audit findings (drift on README, Quark residue, empty ADR/DEP/MA directories) — addressed in the same change set.
