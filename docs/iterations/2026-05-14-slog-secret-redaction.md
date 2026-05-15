# Iteration — Structured-Logger Secret Redaction

> Archived: 2026-05-15
> Work shipped: 2026-05-14
> Branch: main
> PR: #62 (merged as f56032e)
> ADR: ADR-007
> Status: COMPLETE — merged, all acceptance criteria met.

---

## Goal

Close the structured-logger secrets gap flagged by the 2026-05-14
post-sprint readiness audit §7 item 6 — `pkg/observe`'s `NewLogger`
built a `slog.Handler` with no `ReplaceAttr`, so any code that logged a
secret-bearing attribute (`authorization`, `password`, `token`, a
session `cookie`, …) emitted it verbatim. This was the sibling security
item to the CSRF hardening shipped earlier the same day.

---

## What shipped (PR #62)

- **Default-on redaction.** `NewLogger` installs a `ReplaceAttr` hook
  that masks the value of any attribute whose key is in a curated
  case-insensitive denylist (`observe.DefaultRedactedKeys()`). The key
  and the log-line shape are unchanged; only the value becomes
  `RedactionPlaceholder` (`[REDACTED]`).
- **Curated denylist** (`pkg/observe/redact.go`). HTTP auth / headers
  (`authorization`, `cookie`, `set-cookie`, …), generic secrets
  (`password`, `secret`, `api_key`, `client_secret`, …), tokens
  (`token`, `access_token`, `refresh_token`, `oauth_token`,
  `github_token`, `slack_token`, `csrf_token`, …), key material
  (`private_key`, `private_key_pem`, `rsa_private_key`, …), connection
  strings (`database_url`, `dsn`, `redis_url`, `smtp_pass`, …) and
  cloud credentials (`aws_secret_access_key`, `aws_session_token`).
  Substring/suffix matching was rejected in the ADR for false-positive
  risk; the curated exact-match list is predictable and operator-
  extendable.
- **Additive API.** `NewLoggerWithRedaction` (the explicit-control
  constructor), `RedactionConfig` (`Disabled`/`ExtraKeys`/`Placeholder`),
  `DefaultRedactedKeys()`, `RedactionPlaceholder` — all added to the
  freeze baseline.
- **Code-only opt-out.** There is deliberately no config key to disable
  redaction. `log_redact_extra_keys` (lifecycle `transitional`) lets the
  operator extend the denylist via `nucleus.yml`; disabling requires
  code (`NewLoggerWithRedaction` with `Disabled: true`) so the decision
  surfaces in code review. Same discipline as ADR-004 / `WithOpenAuthz`.
- **ADR-007** cut; `OBSERVABILITY_BASELINE.md`, `CONFIG_KEY_REGISTRY.md`,
  `API_CONTRACT_INVENTORY.md`, `CHANGELOG.md` all aligned.

## Acceptance criteria — all met

- [x] `slog.HandlerOptions.ReplaceAttr` redacts denylisted keys by default.
- [x] `NewLoggerWithRedaction` + `RedactionConfig` exposed (additive).
- [x] `log_redact_extra_keys` config key wired through `App.New`.
- [x] No config key disables redaction.
- [x] Contract freeze green; new symbols + config key in the baselines.
- [x] `go test ./...` green.

## Review-loop fixes folded into PR #62

architect-reviewer **PASS**, code-reviewer **NITS**, security-auditor
**PASS** (2 MED, both addressed below), contract-guardian **PASS**.

- **Security MED — bootstrap-password lockout.** The auto-generated
  admin bootstrap password was logged under the key `"password"` — now
  `[REDACTED]`, which would lock the operator out on first boot. The
  password is now written **once to stderr**, deliberately bypassing the
  logger; the structured log records only that it happened. The
  framework's single sanctioned secret-to-stderr path. (`pkg/app/app.go`)
- **Security MED — denylist expansion.** Added framework-relevant keys:
  DSN / connection strings, `smtp_pass`, `aws_secret_access_key`,
  `aws_session_token`, private-key-material names, provider tokens
  (`oauth_token`, `github_token`, `slack_token`).
- **Code-review — built-in collision guard.** `ExtraKeys` cannot silence
  slog's built-in attrs (`time` / `level` / `msg` / `source`) even if an
  operator accidentally lists one — guarded explicitly.
- **Godoc — limitations documented.** `NewLogger` states redaction is
  key-based only: a secret interpolated into the `msg` string, or
  nested in a struct logged via `slog.Any` under a benign key, is not
  redacted. Redaction is defence-in-depth, not a license.
- **API_CONTRACT_INVENTORY.md** updated with the seven new `pkg/observe`
  symbols (contract-guardian follow-up).

## Notes / decisions log

- 2026-05-14 — chose curated exact-match denylist over substring/suffix
  patterns. `*_token` would catch `access_token` (good) but also
  `page_token` / `continuation_token` (false-positive — silently hides
  pagination debug info). Exact-match list is predictable; operators
  extend it for app-specific fields.
- 2026-05-14 — config-split rule: **extend** the denylist is reachable
  via `log_redact_extra_keys`; **disable** is code-only. Architect-
  reviewer noted this is a new governance precedent worth generalising
  if it spreads to other subsystems (e.g. CSRF key-strength enforcement,
  rate-limit-bypass).
- 2026-05-14 — bootstrap-password redirected to stderr. This is the
  framework's only sanctioned secret-to-stderr path. Future secrets
  (e.g. one-time API tokens) should follow the same pattern.

## Follow-ups carried into future iterations

1. **`mergeDefaults` slice-aliasing** — `merged.LogRedactExtraKeys =
   base.LogRedactExtraKeys` shares the slice header. Nil-safe today
   (zero-value defaults), but a defensive `append(nil, …)` pass over
   every slice field in `mergeDefaults` would close the aliasing risk
   consistently. Code-review nit, not blocking. (`pkg/app/app.go`)
2. **Hot-path allocation under mixed-case attr keys.** `strings.ToLower`
   has a fast path that returns the input unchanged when it is already
   lowercase ASCII (the common case = zero allocs). Mixed-case keys like
   the literal `Authorization` still allocate 8 bytes per record. An
   allocation-free ASCII case-fold lookup would close it. Negligible in
   practice; revisit if profiling flags it.
3. **Governance note on the extend-yes / disable-no config split** —
   architect's future recommendation. If the same pattern surfaces in
   other subsystems, add a short note under `docs/governance/` so the
   precedent is generalised.
4. **`slog.Any` struct recursion** — a secret nested inside a struct
   logged via `slog.Any` under a benign key is not redacted. Documented
   as a known limitation; a deeper fix would need a custom marshalling
   hook. Out of scope for this iteration.
