# `.claude/state/`

This directory is Claude Code's working memory **for the active
iteration**. It is read at the start of every session and rewritten at
the end of every session.

| File                                   | Owner             | Lifecycle                             |
|----------------------------------------|-------------------|----------------------------------------|
| `HANDOFF.md`                           | `session-curator` | Overwritten by `/handoff` each session |
| `CURRENT_ITERATION.md`                 | `session-curator` | Updated by `/iterate` and `/handoff`   |
| `templates/HANDOFF.template.md`        | humans            | Source of truth for the handoff shape  |
| `templates/CURRENT_ITERATION.template.md` | humans         | Source of truth for the iteration shape|

Conventions:

- **Absolute dates only** (e.g., `2026-05-10`). Never "today" or
  "yesterday".
- **No secrets, tokens, or full file contents** — use paths and line
  ranges.
- Other subagents **must not** edit files in this directory. Only
  `session-curator` writes here.

When an iteration's acceptance criteria are all met, `session-curator`
copies `CURRENT_ITERATION.md` to `docs/iterations/YYYY-MM-DD-<slug>.md`
and replaces it with the empty template.
