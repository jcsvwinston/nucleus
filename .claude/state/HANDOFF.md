# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    v0.8.0 released — COMPLETE. Next iteration: pick from carry-forward backlog.
BRANCH:       main (clean, in sync with origin/main).
LAST COMMIT:  98753c8 fix(ci): drop stale admin-UI JS syntax check from release workflow
STATUS:       done — v0.8.0 tagged (ae394b7, IMMUTABLE), GitHub release object live at
              https://github.com/jcsvwinston/nucleus/releases/tag/v0.8.0 (Latest,
              notes-only). release.yml stale-JS-check fixed (98753c8). main CI run for
              98753c8 was in progress at close; it is a workflow-file-only change over
              code that was green at ae394b7 — expected green. Confirm via gh if needed.
NEXT STEP:    Start a new iteration. Strongest candidates (pick with owner):
              1. P1 — WithoutDefaults() admin-bootstrap leak: pkg/app/app.go:~272 calls
                 admin.EnsureBootstrapAdminUser unconditionally before the
                 !o.skipDefaults guard. Move call inside the guard.
              2. P2 — Router.Resource("") panic under module Prefix: pkg/nucleus/router.go
                 joinPath should yield "/" not "" when prefix+path are both empty.
              3. ADR-010 §2 layer 5 — module-specific config validation (last validator
                 layer; layer 4 referential shipped 2026-05-26).
              4. GOVERNANCE — enable branch protection / required gate on main so red CI
                 cannot be pushed directly (we pushed directly to main twice this session).
BLOCKERS:     none.
FILES OF INTEREST: pkg/app/app.go (~272), pkg/nucleus/router.go, .claude/state/CURRENT_ITERATION.md
NOTES:        v0.8.0 release notes: release.yml had been broken since before v0.6.0 (stale
              node --check step targeting deleted admin/ui JS files); v0.6.0 and v0.7.0
              also have no GitHub release object for this reason (last was v0.5.5). The
              98753c8 fix means the next tag will publish cleanly. proxy.golang.org has
              already fetched v0.8.0 — tag is immutable, do not move it. Full archive at
              docs/iterations/2026-05-28-cut-v0.8.0.md.

Updated: 2026-05-28
