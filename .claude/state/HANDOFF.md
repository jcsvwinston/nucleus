# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.
>
> Previous handoff (2026-06-20) tracked orbit Slice 2 as "not yet started".
> This handoff reflects the full admin→orbit extraction iteration, which
> COMPLETED in this session (2026-06-21). Both state files rebuilt from
> the authoritative session facts provided by the maintainer.

ITERATION:    Admin→orbit extraction (ADR-019) — COMPLETE end-to-end.
              nucleus `pkg/admin` is gone; orbit embeds the SPA and is
              production-verified. Three follow-up chips are open (not
              blocking).
BRANCH:       nucleus main (clean — only untracked
              docs/audits/2026-06-14-exhaustive-audit.md, intentionally
              never committed).
LAST COMMIT:  nucleus  8714882  feat(nucleus): remove in-core admin, clean
                                break (ADR-019 Slice 2.4, PR #155)
              orbit    f68c64f  chore(deps): bump nucleus pin to
                                v0.9.1-0.20260621031917-8714882cc7f9
STATUS:       Iteration COMPLETE. All acceptance criteria met.
              Fleetdesk finding #9 closed. ADR-019 Accepted.
              Three background follow-up tasks are open (advisory / not
              blocking main or orbit builds):
                task_b8cbc177 — update Docusaurus site for admin→orbit break
                                (advisory CI check "Website Docs Drift"
                                currently failing on main; non-blocking)
                task_2e0651af — move orphaned nucleus CLI commands
                                (createuser/changepassword) + admin/agent|proto|server
                                modules into orbit
                task_6822ff25 — grow orbit.Config to expose cluster/live/trace/
                                auth-DB options (parity gap vs old in-core admin)
NEXT STEP:    Pick up task_b8cbc177 (Docusaurus update) — it is the most
              visible open item and clears the failing advisory CI check on main.
              Entry point: website/docs/ tree; invoke website-curator subagent.
BLOCKERS:     none (advisory CI check is non-blocking)
FILES OF INTEREST:
              website/docs/           (Docusaurus source; task_b8cbc177 target)
              docs/adrs/ADR-019.md    (status: Accepted, as of PR #155)
              pkg/admin/              (REMOVED in PR #155 — do not expect it)
              contracts/baseline/api_exported_symbols.txt (rebaselined in PR #155)
              docs/governance/DEPRECATION_TEMPLATE.md
              docs/migrations/DEP-MA-2026-004.md (admin_rbac_policy_file rename)
              ~/GolandProjects/orbit  (orbit repo; HEAD f68c64f)
              ~/GolandProjects/orbit/go.mod
                (nucleus pin v0.9.1-0.20260621031917-8714882cc7f9)
              docs/iterations/2026-06-21-admin-orbit-extraction-clean-break.md
NOTES:        nucleus main is PR-only (enforce_admins=true, required check
              "CI Required Gate" strict=true). Every nucleus change: branch →
              push → gh pr create → wait CI green → gh pr merge --squash
              --delete-branch. Direct git push origin main is REJECTED.
              govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              Config key `admin_rbac_policy_file` renamed → `rbac_policy_file`;
              deprecated alias + startup WARN live; migration doc: DEP/MA-2026-004.
              v0.9.0 is the latest published tag (2026-06-09). nucleus main is
              ahead; fleetdesk consumer pins pseudoversion
              v0.9.1-0.20260621031917-8714882cc7f9.
              orbit is a PRIVATE repo (github.com/jcsvwinston/orbit); local clone
              at ~/GolandProjects/orbit.

Updated: 2026-06-21
