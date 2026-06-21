# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.
>
> This handoff reflects 2026-06-21 session close: all admin→orbit follow-ups
> completed. ADR-019 is 100% done. No active iteration.

ITERATION:    Admin→orbit extraction (ADR-019) — COMPLETE (all follow-ups done).
              nucleus is a single admin-free Go module; orbit owns the full
              admin product. No open chips remain.
BRANCH:       main
LAST COMMIT:  133359e chore(core)!: remove the admin observability subsystem — relocated to orbit (ADR-019) (#159)
STATUS:       done
NEXT STEP:    Await owner direction. Likely candidates: (a) flip orbit repo
              public so consumers can `go get github.com/jcsvwinston/orbit`
              and set up orbit CI/tests; (b) start a new nucleus framework
              iteration.
BLOCKERS:     none
FILES OF INTEREST: ~/GolandProjects/orbit (orbit repo; HEAD 59f2e59),
              docs/adrs/ADR-019.md,
              internal/cli/orbit_guard.go,
              docs/iterations/2026-06-21-admin-orbit-followups-complete.md
NOTES:        orbit is PRIVATE (github.com/jcsvwinston/orbit); must be flipped
              to public at release before consumers can go get it.
              nucleus createuser/changepassword intentionally remain in nucleus,
              guarded on orbit schema presence via internal/cli/orbit_guard.go.
              The 3 docs/audits/* files are intentionally never committed
              (docs/audits/2026-06-14-exhaustive-audit.md,
               docs/audits/2026-06-21-exhaustive-audit.md,
               docs/audits/2026-06-21-exhaustive-audit.es.md).
              govulncheck pinned @v1.3.0 — do NOT upgrade to @latest (panics
              under Go 1.26.4 generics with x/vuln v1.4.0).
              nucleus main is PR-only (enforce_admins=true, CI Required Gate
              required). Direct git push origin main is REJECTED.
              v0.9.0 is the latest published tag; bump
              defaultPinnedFrameworkVersion in internal/cli/new.go after each
              new tag.

Updated: 2026-06-21
