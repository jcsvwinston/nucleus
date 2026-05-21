# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Website refresh + website-curator subagent — COMPLETE (pushed). No active iteration.
BRANCH:       main (clean, in sync with origin/main).
LAST COMMIT:  5a79095 chore(agents): add website-curator subagent + wire into loop and commands
STATUS:       Public site (website/docs/) refreshed to match shipped Nucleus behaviour; heuristic drift guard added (scripts/website/check-coverage.sh + advisory website-drift CI job); website-curator subagent created and wired into CLAUDE.md §4 loop + §6 index + iterate.md + sync-docs.md; doc-updater narrowed to internal docs; all pushed; docs.yml Pages deploy triggered by 3ca91ce.
NEXT STEP:    Owner picks the next iteration from the candidate list in CURRENT_ITERATION.md. Top picks: #1 shared pkg-enumeration helper for contract scanners; #2 pkg/observability inventory entry + firewall scan; #3 add covers:/config_keys: frontmatter manifests to the 14 website/docs/ pages. Optionally confirm the docs.yml Pages deploy for 3ca91ce went green (GitHub Actions tab).
BLOCKERS:     none.
FILES OF INTEREST:
  - .claude/agents/website-curator.md — new subagent; owns website/docs/**, manifests, drift guard, site build.
  - .claude/agents/doc-updater.md — narrowed to internal docs + godoc.
  - scripts/website/check-coverage.sh — drift guard; bash 3.2 portable; --strict mode.
  - .github/workflows/ci.yml — advisory website-drift job (not a required gate); Oracle AutoMigrate_Exploratory NOTE breadcrumb still present.
  - website/docs/** — six pages refreshed (cli/overview, concepts/configuration, getting-started/quickstart, features/auth, concepts/routing, concepts/models-and-database).
  - pkg/nucleus/nucleus.go — corrected package-level godoc (comment-only; freeze test unaffected).

NOTES:
  - The two-docs-tree rule (root docs/ = internal/contract, never published; website/docs/ = curated public Docusaurus, deploy via docs.yml on website/** push) is codified in website-curator.md, doc-updater.md, and user memory (docs_two_trees.md). No sync between the two trees.
  - The website-drift CI job is advisory (not required) — it catches regressions; omissions are the website-curator subagent's job within the loop. Promote to required once covers: manifests exist and the job has proven stable (candidate #16).
  - The permission rule allowing the agent harness to self-modify .claude/ config lives in .claude/settings.local.json (gitignored, local-only; not committed).
  - No CHANGELOG entry and no semver bump for either commit — docs + internal tooling only, no user-facing runtime change.

Updated: 2026-05-21
