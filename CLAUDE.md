# Nucleus / GoFrame — Claude Code Operating Manual

> Reference date: 2026-05-10.
> Status: Authoritative protocol for Claude Code iterations on this repository.
>
> This file is loaded automatically by Claude Code at the start of every
> session. Read it top-to-bottom before doing anything else.

---

## 0. TL;DR for Claude Code

Each time you start in this repo:

1. Run the **Session Start Protocol** in §2 (read handoff + current iteration).
2. Confirm the **active iteration goal** with the user before writing code.
3. Implement work in small, reviewable slices.
4. After every meaningful change, run the **Iteration Loop** in §4 — it
   delegates work to the subagents in `.claude/agents/`.
5. Before stopping, run the **Session End Protocol** in §5 to persist a clean
   handoff for the next session.

The slash commands `/resume`, `/iterate`, `/review`, `/sync-docs`,
`/release-prep`, and `/handoff` (in `.claude/commands/`) wrap these flows.

---

## 1. Project at a Glance

**Nucleus / GoFrame** is an enterprise-grade MVC + REST API framework written
in Go (`1.26+`, matching the `go 1.26.3` directive in `go.mod`). It targets
parity with frameworks such as Gin and Django and favours stdlib-first design.

Authoritative documents (precedence high → low, per `SPEC.md` §1):

1. `README.md`
2. Contract & governance docs:
   - `docs/reference/API_CONTRACT_INVENTORY.md`
   - `docs/reference/CLI_CONTRACT_MATRIX.md`
   - `docs/reference/CONFIG_KEY_REGISTRY.md`
   - `docs/governance/COMPATIBILITY_SLO.md`
3. `SPEC.md`
4. `docs/guides/*` and `docs/reference/DEVELOPER_MANUAL.md`

When two documents conflict, follow that order. Update the lower-precedence
document, never the contracts, unless we are deliberately changing a contract.

### Directory map (cheat sheet)

| Path                        | Role                                             |
|-----------------------------|--------------------------------------------------|
| `cmd/goframe/`              | CLI entry point (`main.go`).                     |
| `pkg/`                      | Stable public API surface (`app`, `router`, `db`, `model`, `auth`, `mail`, `observe`, `validate`, `signals`, `admin`, …). |
| `internal/cli/`             | CLI command implementations and tests.           |
| `internal/`                 | Private implementation details, never imported by users. |
| `contracts/`                | Stable contract baselines + freeze tests.        |
| `examples/`                 | Reference applications. Today only `mvc_api` is tracked (Phase 4 Slice 1, landed 2026-05-24). Additional reference apps and `plugins/*` are deferred to v0.9.X per ADR-010 Phase 4 / docs-sync. |
| `docs/`                     | Developer-facing documentation, ADRs, governance, guides. |
| `docs/adrs/`                | Architecture Decision Records.                   |
| `docs/governance/`          | SLOs, CI matrix, release checklist, deprecation policy. |
| `docs/migration_assistants/`| Tooling for major-version migrations.            |
| `scripts/ci/`               | Compatibility harness and contract-freeze scripts. |
| `scripts/release/`          | Release rehearsal and dependency-impact scripts. |
| `.claude/`                  | Claude Code configuration: subagents, commands, session state. |

### Non-negotiable principles (`SPEC.md` §2)

1. stdlib-first runtime (`net/http`, `database/sql`, `log/slog`, `context`).
2. Explicit configuration & lifecycle — no hidden globals.
3. Compatibility by contract on stable surfaces.
4. Security-by-default for production-sensitive features.
5. SQL-first operations and deterministic CLI behaviour.

Treat any deviation as architecturally significant — escalate via
`architect-reviewer` and an ADR.

---

## 2. Session Start Protocol

Run this **first**, before answering the user’s message, every time you open
this repo. The slash command `/resume` automates it.

1. **Read session state** in this order. Files may be missing on a fresh
   clone — that is fine.
   - `.claude/state/HANDOFF.md` — the previous session’s closing notes (last
     thing the previous Claude wrote before stopping).
   - `.claude/state/CURRENT_ITERATION.md` — the active iteration goal,
     scope, acceptance criteria, and known blockers.
   - `docs/iterations/` (if present) — chronological log of completed
     iterations; the most recent file is the freshest reference point.
2. **Inspect the working tree** with bash:
   - `git status --short` — uncommitted changes to be aware of.
   - `git log --oneline -n 10` — recent commits.
   - `git branch --show-current` — current branch (release branches such as
     `codex/v0.6.0-roadmap` carry extra constraints).
3. **Reconcile** the user’s message with the state files:
   - If the message extends or modifies the active iteration, proceed.
   - If it starts a new iteration, ask whether to **archive** the current
     `CURRENT_ITERATION.md` into `docs/iterations/` and replace it.
   - If the message conflicts silently with the handoff (e.g. user says
     "continue" but the handoff is empty), surface the gap and ask before
     guessing.
4. **Surface a one-paragraph briefing** to the user before doing work:
   *what was open, what I am about to do next, what is blocked.* Do this even
   if the user did not explicitly ask — it gives them a chance to redirect.

The `session-curator` subagent (`.claude/agents/session-curator.md`) is the
canonical owner of this protocol; delegate to it when the briefing requires
careful synthesis.

---

## 3. Working Conventions

### Go style

- Target `go 1.26+` (matches `go.mod`'s `go 1.26.3`). Lean on the stdlib;
  new third-party deps require an ADR (`docs/adrs/`) and a
  `dependency-impact` review.
- Prefer table-driven tests, `testify` only where it improves clarity.
- Public symbols in `pkg/*` must be documented (godoc) — they are part of
  the stable contract.
- Wrap errors with `%w`. Use `log/slog` with `context`-bound attributes for
  observability. Never swallow `error`.
- Concurrency: every goroutine must have a clear shutdown path. Respect
  `context.Context` deadlines.

### Configuration

- Config keys live in `goframe.yaml`. New keys must be registered in
  `docs/reference/CONFIG_KEY_REGISTRY.md` and validated by
  `pkg/app/config.go`.

### CLI

- New CLI surface: register in `internal/cli/`, document in
  `docs/reference/CLI_CONTRACT_MATRIX.md`, add an entry to
  `docs/reference/CLI_BEST_PRACTICES.md`.
- Removals or renames of stable commands trigger
  `contracts/cli_json_freeze_test.go` — coordinate via
  `migration-assistant` and the deprecation policy in
  `docs/governance/DEPRECATION_TEMPLATE.md`.

### Examples

- The tracked examples under `examples/*` (today: `mvc_api` only) are
  first-class consumers of the framework. If you change a public API,
  update the relevant example in the **same** PR. The
  `examples-maintainer` subagent enforces this. Additional reference apps
  and `plugins/*` return in v0.9.X per ADR-010 Phase 4.

### Tests

- Default fast lane: `go test ./...`.
- Compatibility fixtures: `bash scripts/ci/run_compatibility_harness.sh
  --enforce-threshold`.
- Contract freeze: `bash scripts/ci/check_contract_freeze.sh`.
- DB matrix lanes (`postgresql`, `mysql` required; `mssql`, `oracle`
  exploratory) are documented in `docs/governance/CI_MATRIX.md`.

### Documentation

- User-facing changes update `CHANGELOG.md` (under `Unreleased`).
- Architectural decisions land as ADRs in `docs/adrs/`.
- Guides under `docs/guides/` mirror runtime behaviour 1:1 — outdated
  guides are bugs.

---

## 4. Iteration Loop

Each iteration is a small, reviewable slice. Run this loop after every
meaningful change. The `/iterate` slash command wraps it.

```
              ┌────────────────────────────────────────────────────┐
              │              IMPLEMENT (you, the agent)            │
              └────────────────────────────────────────────────────┘
                                       │
                                       ▼
   1. architect-reviewer  →  is the change consistent with SPEC + ADRs?
   2. code-reviewer       →  Go quality, error handling, concurrency,
                             race / N+1 / nil-deref risks
   3. security-auditor    →  authn/authz, input validation, secrets,
                             SQL/template injection, CSRF/CORS
   4. contract-guardian   →  did we mutate a stable API/CLI/config key?
                             if yes, freeze tests + deprecation path
   5. test-runner         →  go test ./… (+ targeted -run filters,
                             race when relevant); compatibility harness
                             on contract-touching changes
   6. examples-maintainer →  reflect public-API changes in examples/*
   7. doc-updater         →  internal docs (guides, references, godoc,
                             README); sync with shipped behaviour
   8. website-curator     →  public Docusaurus site (website/docs/*); keep
                             it a faithful reflection; run the drift guard
   9. changelog-writer    →  CHANGELOG.md under Unreleased; semver bump
                             hint
  10. governance-checker  →  COMPATIBILITY_SLO, CI_MATRIX, RELEASE_CHECKLIST
                             cross-checks (light-touch unless releasing)
                                       │
                                       ▼
              ┌────────────────────────────────────────────────────┐
              │  Update CURRENT_ITERATION.md + propose commit msg  │
              └────────────────────────────────────────────────────┘
```

Rules of thumb:

- **Always** run steps 1–2 and 5.
- Skip 3 only for pure docs/tests changes; otherwise run it.
- Skip 4 only when you have not touched files under `pkg/`,
  `internal/cli/`, `contracts/`, or `goframe.yaml` schema.
- Steps 6–9 are mandatory whenever public behaviour changes (step 8,
  `website-curator`, fires whenever a reader-visible surface changes —
  public API, CLI, config keys, defaults, headline features).
- Step 10 is light during normal iterations and full-strength during
  release prep (`/release-prep`).

When a subagent flags a blocker, **stop the loop** and surface it to the
user before continuing.

---

## 5. Session End Protocol

Before the user closes the session — or before a long pause — run this. The
`/handoff` slash command wraps it.

1. Make sure `git status` is in a state you can describe in one paragraph.
2. Update `.claude/state/CURRENT_ITERATION.md`:
   - what is **done**,
   - what is **in progress**,
   - what is **blocked** and why.
3. Overwrite `.claude/state/HANDOFF.md` with a short, machine-friendly
   note: branch, last commit, next concrete step, files of interest, any
   command the next session should run first.
4. If the iteration is **complete**, archive a copy of
   `CURRENT_ITERATION.md` into `docs/iterations/YYYY-MM-DD-<slug>.md` and
   start a new empty `CURRENT_ITERATION.md`.
5. Suggest a commit message and (optionally) a CHANGELOG line.

The `session-curator` subagent owns the formatting of these files.

---

## 6. Subagent Index

All subagents live in `.claude/agents/` and follow a uniform contract: they
return findings as a short, prioritized report and never silently mutate
files outside their stated scope. Invoke them via the Task tool.

| Subagent                                | One-liner                                                                 |
|-----------------------------------------|---------------------------------------------------------------------------|
| `session-curator`                       | Owns `.claude/state/`, runs Session Start/End protocols.                  |
| `architect-reviewer`                    | Checks SPEC + ADR consistency, layering, extension points.                |
| `code-reviewer`                         | Go-idiomatic review: errors, concurrency, allocations, edge cases.        |
| `security-auditor`                      | AuthN/Z, injection, secrets, transport, CSRF/CORS, secure defaults.       |
| `contract-guardian`                     | Stable API/CLI/config surfaces; freeze tests; deprecation discipline.     |
| `test-runner`                           | Runs the right test lane and surfaces actionable failures.                |
| `examples-maintainer`                   | Keeps `examples/*` aligned with public API changes.                       |
| `doc-updater`                           | Syncs internal guides/refs/godoc/README with shipped behaviour.           |
| `website-curator`                       | Owns the public Docusaurus site (`website/docs/*`); faithfulness + drift guard. |
| `docs-content-verifier`                 | Body-content scanner for any doc page: validates Go symbols in code blocks vs the contract baseline, YAML keys vs `CONFIG_KEY_REGISTRY.md`, and Go-version pins vs `go.mod`. Invoked by `doc-updater` and `website-curator` before publishing. |
| `changelog-writer`                      | Curates `CHANGELOG.md` and proposes semver impact.                        |
| `dependency-impact`                     | Scopes the blast radius of dependency adds/upgrades.                      |
| `migration-assistant`                   | Plans deprecation + migration steps for breaking changes.                 |
| `performance-bench`                     | Benchmarks hot paths and tracks regressions.                              |
| `governance-checker`                    | Cross-checks SLOs, CI matrix, release checklist before release.           |

Slash commands in `.claude/commands/` orchestrate these:

| Command           | What it does                                                             |
|-------------------|--------------------------------------------------------------------------|
| `/resume`         | Run the Session Start Protocol and brief the user.                       |
| `/iterate`        | Run the full iteration loop on the current change set.                   |
| `/review`         | Read-only review pass (architect + code + security).                     |
| `/sync-docs`      | Run `doc-updater`, `website-curator` and `examples-maintainer` only.     |
| `/release-prep`   | Heavy-weight pre-release governance and contract validation.             |
| `/handoff`        | Run the Session End Protocol and persist next-session state.             |

---

## 7. Hard Constraints

- **Never delete or rename** symbols in `pkg/*`, stable CLI commands, or
  registered config keys without a deprecation entry and a migration
  assistant. The `contract-guardian` subagent enforces this.
- **Never mock the database** in tests that exercise migration logic — use
  the SQLite/Postgres/MySQL lanes per `docs/governance/CI_MATRIX.md`.
- **Never check in** generated artefacts under
  `examples/*/frontend/node_modules` or build outputs.
- **Never edit** `contracts/baseline/*.txt` to make a freeze test pass —
  that hides regressions. Update behaviour or open a deliberate contract
  change ADR.
- **Never publish a doc page** (whether under `docs/*` or
  `website/docs/*`) without verifying every Go symbol, every YAML/TOML
  config key, and every Go-version claim in the body against the source
  of truth (`contracts/baseline/api_exported_symbols.txt`,
  `docs/reference/CONFIG_KEY_REGISTRY.md`, `go.mod`). See §9 for the
  exact procedure. The `docs-content-verifier` subagent enforces this.
- **Never invoke a generic subagent** (`Explore`, `general-purpose`,
  `Plan`) for a task that has a specialized subagent in
  `.claude/agents/`. The specialized agent owns the task's output
  contract and follow-up obligations; the generic one does not. See §10.

---

## 8. Memory Notes (for Claude Code)

- Treat this file as your highest-priority instruction set within this
  repo. Project-specific preferences override the global system prompt
  except for safety policies.
- When unsure between two paths, ask the user — Nucleus has many
  contracts and silent guesses cost a lot to undo.
- Translate relative dates to absolute when logging anything in
  `docs/iterations/` or `.claude/state/*`.

---

## 9. Anti-falsehood discipline for documentation

> Added 2026-05-24 after an audit found three P0 falsehoods that had been
> live on the public website for weeks (wrong Go version, a non-existent
> function name in a code example, an inexistent YAML key in another
> example). The drift guard caught zero of them because all three were
> in body content, not in frontmatter. This section codifies the
> response so the class of bug cannot recur.

Every doc change — internal `docs/*` or public `website/docs/*` — must
pass three verifications before publishing:

1. **Go symbols cited in code blocks must exist in the freeze baseline.**
   Every reference in a fenced code block (\`\`\`go) of the form
   `<pkgname>.<Symbol>` is verified against
   `contracts/baseline/api_exported_symbols.txt`. Aliases and helpers
   from inside the same example file are OK; cross-package references
   are not. A `func`, `type`, `method`, `field`, `const`, or `var` entry
   in the baseline counts as a match.
2. **YAML/TOML keys in code blocks must exist in the config registry.**
   Every top-level or nested key shown in a fenced `yaml`/`toml` block
   is verified against `docs/reference/CONFIG_KEY_REGISTRY.md`. Legacy
   deprecated keys are valid matches but must carry a
   `# deprecated, use ...` comment when shown.
3. **Go version mentions must match `go.mod`.** Any string of the form
   `Go 1.XX`, `**1.XX+**`, or `go 1.XX` in prose, headings or HTML must
   match the `go` directive in `go.mod` (or accept the floor declared
   by a `toolchain go1.XX.Y` directive when present).

These checks are enforced by the `docs-content-verifier` subagent. The
`doc-updater` and `website-curator` subagents MUST hand off to it
before declaring their own `UPDATED` verdict. The body-content extension
of `scripts/website/check-coverage.sh` (planned follow-up) will make
this enforceable in CI; until then, it is a manual discipline at the
subagent layer.

When a verification fails:

- A symbol/key drift means **the docs lie** — fix the doc unless the
  underlying symbol/key was renamed; in that case, route the rename via
  `contract-guardian` + `migration-assistant` and update both source
  and docs in the same PR.
- A Go-version drift means **either the doc lies or `go.mod` does** —
  the answer is `go.mod`. Fix the doc.

## 10. Subagent dispatch policy (specialized wins)

> Added 2026-05-24 after a sub-task delegated to generic agents
> (`Explore`, `general-purpose`) missed a finding that the specialized
> `website-curator` would have surfaced via its mandated `npm run build`
> and body-content discipline.

For any task that has an owner in `.claude/agents/`, you MUST use that
agent. The generic platform agents (`Explore`, `general-purpose`,
`Plan`, `claude-code-guide`) are valid only when **no** specialized
agent claims the scope.

Decision table:

| Task | First-choice subagent | Fallback |
|---|---|---|
| Anything touching `website/docs/**` | `website-curator` | none — never edit without it |
| Anything touching `docs/guides/*`, `docs/reference/*`, godoc, `README.md`, `docs/QUICKSTART.md` | `doc-updater` | none |
| Body-content fact-check (Go symbols, YAML keys, Go version) of any page | `docs-content-verifier` | none |
| Anything touching `examples/*` | `examples-maintainer` (today: `mvc_api` is in scope; full scope returns with the remaining reference apps in v0.9.X) | none |
| `.claude/state/*` or `docs/iterations/*` | `session-curator` | none |
| Anything touching `pkg/*` exported symbols, stable CLI, config keys, `contracts/` | `contract-guardian` (plus the rest of the iteration loop) | none |
| SPEC.md / ADR architectural review | `architect-reviewer` | none |
| Go code review | `code-reviewer` | none |
| Auth / authz / secrets / CSRF / CORS / injection | `security-auditor` | none |
| Test execution | `test-runner` | none |
| `CHANGELOG.md` curation | `changelog-writer` | none |
| Dependency add/upgrade impact | `dependency-impact` | none |
| Breaking-change deprecation planning | `migration-assistant` | none |
| Hot-path benchmarks | `performance-bench` | none |
| SLO / CI matrix / release checklist cross-check | `governance-checker` | none |
| Read-only repository exploration (find a file, grep for a symbol) | none specialized | `Explore` |
| Open-ended multi-step research with no clear owner | none specialized | `general-purpose` |
| Implementation strategy planning before writing code | none specialized | `Plan` |

If the platform does not expose a specialized agent via
`subagent_type` (this is the case in Cowork mode, which only exposes
the generic types), the correct response is to **read the full prompt
of the specialized agent from `.claude/agents/<name>.md` and pass it
verbatim** to a generic agent, instructing that agent to adopt the
specialized role and honour the specialized output contract. Then
relay the result. This is a workaround for the platform limitation, not
a license to skip the discipline.

End of operating manual.
