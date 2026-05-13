# Iteration — ADR-004 Integration Sprint

> Archived: 2026-05-13
> Branch: main
> PRs: #51 (Casbin default-deny, done in prior session), #52 (drop built-in SendGrid), #53 (JWT wiring + JWKS auto-mount), #54 (circuit-breaker autowrap mail + storage)
> Status: FUNCTIONALLY COMPLETE — all three wiring items shipped; one follow-up (E2E cross-integration test) carried forward.

---

## Goal

Close the "primitive exists but framework does not use it" critique raised by
the owner on 2026-05-13: `pkg/auth.NewJWTManager` was never called from
`pkg/app`; the Casbin enforcer was not mounted without a policy file; and
`pkg/circuit` was imported only from Markdown. This sprint wired all three
primitives into the default `App.New` path and corrected the CHANGELOG entries
for #40, #41, and #46.

---

## Scope

### In

- **#51** — `pkg/authz` Casbin enforcer wired into `App.New` with default-deny
  and a bootstrap allow-list (`/healthz`, `/metrics`, `/.well-known/jwks.json`,
  `/admin/login`, `/login`). `app.WithOpenAuthz()` opt-out. ADR-004 cut.
  CHANGELOG `Changed` entry noting the breaking change for apps without a
  policy file.

- **#52** — Built-in SendGrid provider removed from `pkg/mail`. External-only
  via plugins per DEP-2026-002 / MA-2026-002. SendGrid no longer needs
  circuit-breaker wrapping; SMTP is the only in-tree provider.

- **#53** — `App.New` builds `App.JWT` from `Config.Auth.JWTKeys[]` (`hs256`
  via `secret_env`, `rs256` via `pem_path` / `pem_env`). Fallback to legacy
  `jwt_secret` when slice is empty. `/.well-known/jwks.json` auto-mounts when
  at least one asymmetric key is configured. Empty-HMAC footgun closed
  (`App.JWT == nil` + WARN when both sources are unset). PEM loader accepts
  PKCS#1 and PKCS#8; rejects trailing PEM content. CONFIG_KEY_REGISTRY,
  AUTH_GUIDE, website auth.md, API_CONTRACT_INVENTORY, and DEVELOPER_MANUAL
  all aligned.

- **#54** — `App.New` wraps `mail.Sender.Send` and remote `storage.Store` ops
  (Put / Get / Delete / Exists / List / Copy / SignedURL) with
  `pkg/circuit.Breaker` by default. Config knobs:
  `mail_circuit_breaker.*` and `storage.circuit_breaker.*`, all `transitional`,
  defaults `Enabled: true, FailureThreshold: 5, Cooldown: 30s,
  HalfOpenMaxConcurrent: 1`. `noop` mail driver and `local` storage provider
  never wrapped. `mail.HealthChecker.Healthy` bypasses the breaker so `/healthz`
  stays meaningful while `Send` is short-circuited. `storage.ErrNotFound` is
  not counted as a failure. `storage.PublicURL` is pass-through. Previously
  missing `pkg/storage` row added to API_CONTRACT_INVENTORY (stability:
  `stable`).

### Out

- Standalone E2E test that exercises Casbin + JWT + circuit breaker
  simultaneously via a single `App.New`. Carried forward as a follow-up.
- ES256 / ECDSA support and cloud secret-manager integration.
- Schema-drift detection for MSSQL / Oracle AutoMigrate scaffolds.
- CSV migration helper for Casbin 4-column rows (deferred from #41).
- Test rename `TestSQLMatrix_Exploratory*` → `*_Live*` (deferred from #38).
- `pkg/admin/ui` post-Phase 7 deprecation.

---

## Acceptance criteria

- [x] `App.New` builds and exposes `*auth.JWTManager` from `Config.Auth.JWTKeys[]`
      or falls back to legacy `jwt_secret`. `App.JWT == nil` + WARN when both unset.
- [x] `/.well-known/jwks.json` auto-mounts when at least one asymmetric key is configured.
- [x] Casbin enforcer wired into the default router path with bootstrap allow-list; `app.WithOpenAuthz()` opt-out available; ADR-004 documents the decision.
- [x] `pkg/mail.Sender.Send` and remote `pkg/storage.Store` ops wrapped with `pkg/circuit.Breaker` by default; `noop` / `local` are never wrapped.
- [x] `mail.HealthChecker.Healthy` bypasses the circuit breaker.
- [x] `storage.ErrNotFound` is not counted as a circuit-breaker failure.
- [x] New config keys registered in CONFIG_KEY_REGISTRY; API_CONTRACT_INVENTORY updated; CHANGELOG Unreleased reflects all changes.
- [ ] Standalone E2E test exercising all three (Casbin + JWT + circuit breaker) via a single `App.New` invocation. NOT YET WRITTEN — carried forward.

---

## Files of interest

- `pkg/app/app.go` — primary wire-up site for JWT, Casbin, and circuit-breaker autowrap.
- `pkg/app/config.go` — `Auth.JWTKeys[]`, `Mail.CircuitBreaker`, `Storage.CircuitBreaker` schemas.
- `pkg/auth/jwt.go` — PEM loader (PKCS#1 + PKCS#8), `NewJWTManager`.
- `pkg/authz/middleware.go` — default-deny Casbin mount.
- `pkg/mail/smtp.go` — SMTP provider; SendGrid removed.
- `pkg/storage/s3.go`, `gcs.go`, `azure.go` — circuit-breaker wrap sites.
- `pkg/circuit/` — standalone breaker primitive.
- `docs/adrs/ADR-004.md` — Casbin default-deny architecture decision.
- `docs/guides/AUTH_GUIDE.md` — JWT wiring documentation.
- `docs/guides/STORAGE_GUIDE.md` — circuit-breaker knobs and behavior.
- `docs/reference/API_CONTRACT_INVENTORY.md` — added `pkg/storage` row.
- `docs/reference/CONFIG_KEY_REGISTRY.md` — new circuit-breaker config keys.
- `CHANGELOG.md` — Unreleased entries for all four PRs.

---

## Notes / decisions log

- 2026-05-13 — Owner critique that triggered the sprint: "pkg/auth.NewJWTManager
  no se invoca desde pkg/app, el middleware de Casbin no se monta sin archivo de
  política, pkg/circuit se importa solo desde markdown. CHANGELOG #40/#41/#46
  está vendiendo features que el framework no consume."
- 2026-05-13 — SendGrid removed (#52) as a scope correction per DEP-2026-002 /
  MA-2026-002. External providers via plugins; no in-tree circuit-breaker wrapping
  needed for SendGrid.
- 2026-05-13 — Code-reviewer caught two blockers in #54: `rc == nil` sentinel in
  Get and missing CONFIG_KEY_REGISTRY entries. Both fixed before merge.
- 2026-05-13 — doc-updater noted that `pkg/storage` was previously absent from
  `contracts/baseline/api_exported_symbols.txt` despite being documented as
  `stable`. Added to API_CONTRACT_INVENTORY; contract-guardian follow-up should
  add it to the freeze baseline.

---

## Follow-ups carried into future iterations

1. **E2E cross-integration test** — single `App.New` with Casbin + JWT +
   circuit breaker all active; exercises a failing dependency (SMTP / S3 down)
   to confirm `circuit.ErrOpen` surfaces and `/healthz` still returns 200.
2. **Contract baseline update for `pkg/storage`** — add to
   `contracts/baseline/api_exported_symbols.txt`.
3. **Cosmetic doc pass** — bare code fences in `STORAGE_GUIDE.md` and
   `website/docs/features/storage-and-tasks.md` need `go`/`yaml`/`bash` tags.
4. **Standalone MAIL_GUIDE.md** — mail docs currently split across DEVELOPER_MANUAL
   and website; parity with STORAGE_GUIDE.md.
5. **Post-sprint readiness audit** — last audit was pre-sprint (2026-05-13);
   re-run now that all three integration items have landed.
6. **Tagging decision** — does the integration sprint warrant v0.6.x patch or
   v0.7.0 minor? Owner to decide.
7. **Track D drills + deprecation/MA seeding + Phase 4 modularization** — per
   post-rename roadmap memory note.
8. **Schema-drift detection + MSSQL/Oracle AutoMigrate scaffolds** — P1 from
   earlier conversations, not yet started.
9. **ES256 / ECDSA + cloud secret-manager integration** — originally P0,
   deprioritized in favor of this sprint.
