# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.
>
> NOTE: the previous HANDOFF.md (dated 2026-06-19, finding-#35 close) was
> stale — it predated the entire orbit foundation epic (PRs #148–#152 +
> orbit repo scaffold). Both this file and CURRENT_ITERATION.md were
> rebuilt from reality on 2026-06-20.

ITERATION:    Orbit extraction (ADR-019) — Slice 1 + nucleus prereqs COMPLETE;
              Slice 2 in progress (orbit-side, not yet started).
              nucleus HEAD daa6706. orbit HEAD 9cce16f.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's
              call, not this session's work).
              orbit main @ 9cce16f (PRIVATE repo, local only).
LAST COMMIT:  nucleus  daa6706  feat(nucleus): Router.Mount(pattern,
                                http.Handler) — mount a sub-handler subtree
                                (ADR-019 orbit Slice 2) (#152)
              orbit    9cce16f  scaffold: orbit.Module(Config{Prefix}) +
                                health probe; builds+tests green
STATUS:       All nucleus-side prerequisites for orbit done (Slices 1a/1b/1c
              + Router.Mount). orbit repo exists, builds, health probe green.
              Slice 2.2 (move pkg/admin into orbit) not yet started.
              Findings ledger: 22 FIXED / 12 OPEN (finding #9 closes at
              Slice 2.3 when SPA is embedded).
NEXT STEP:    In orbit repo (~/GolandProjects/orbit):
              1. Re-pin nucleus from 481db78 to daa6706 in go.mod
                 (gains Router.Mount).
              2. Begin Slice 2.2: copy pkg/admin (+ ui/) from nucleus into
                 orbit; adapt construction to Runtime accessors
                 (rt.Models/Databases/Session().ActiveSessions/Authorizer/
                 Observability/Storage + app.RequestScopeFromContext);
                 mount via r.Mount(prefix, panel.Handler()); seed bootstrap
                 allow-list (ADR-004/#19 pattern).
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/orbit (orbit repo; HEAD 9cce16f)
              ~/GolandProjects/orbit/go.mod (nucleus pin; bump 481db78→daa6706)
              pkg/nucleus/runtime.go (Runtime.Models/Databases/Observability)
              pkg/nucleus/eventbus.go (EventBus/SQLEvent/HTTPEvent)
              pkg/auth/session.go (SessionInfo/ActiveSessions/sentinels)
              pkg/router/router.go (Router.Mount — PR #152 daa6706)
              pkg/app/scope.go (RequestScope/RequestScopeFromContext)
              pkg/admin/ (move source for Slice 2.2; remove in Slice 2.4)
              docs/adrs/ADR-019.md (Proposed; flips Accepted at Slice 2.4)
              .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — see NOTES)
              docs/iterations/2026-06-20-orbit-extraction-foundation.md
NOTES:        nucleus main is PR-only (enforce_admins=true, required check
              "CI Required Gate" strict=true). Direct git push origin main is
              REJECTED. Every nucleus change: branch → push → gh pr create →
              wait CI green → gh pr merge --squash --delete-branch.
              govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              TODO: unpin when x/tools publishes a fix for the TypeParam panic.
              orbit pin history: scaffold pins nucleus 481db78 (Slice 1c).
              Must bump to daa6706 (Router.Mount) before any Slice 2 work.
              v0.9.0 is the latest published tag (2026-06-09). nucleus is
              ahead on main; fleetdesk consumer pins a pseudoversion.
              fleetdesk last pin: v0.9.1-0.20260619132308-32a01a002e72
              (32a01a0; finding #35 close). Not touched in this session.

Updated: 2026-06-20
