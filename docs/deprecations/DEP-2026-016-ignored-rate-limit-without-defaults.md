# Deprecation Notice: a rate limit an application built WithoutDefaults() does not mount → refused at v2.0.0

- ID: `DEP-2026-016`
- Status: `active`
- Announced in: `Unreleased` (NU-122, 2026-10-06; the release that carries this change)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is
  a **behaviour change of a configuration combination**, not a symbol
  removal: no `// Deprecated:` marker carries it.
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

The rate limiter is one of the default subsystems: the default stack mounts
it after the bearer decode and the API-key read, so it keys a request by its
user and tenant (NU-4). An application built `WithoutDefaults()` mounts
none. Its configuration could still set one — `rate_limit_requests: 100`,
with `rate_limit_window`, `rate_limit_burst`, `rate_limit_by_route`,
`rate_limit_by_role`, or a `NUCLEUS_RATE_LIMIT_REQUESTS` variable — and the
keys were ignored without a word: with `rate_limit_requests: 1` the
application answered 200, 200, 200, and nothing at boot said the limit was
not there. Worse, `nucleus doctor --check security` and the
`deploy.rate_limit` component of `nucleus health --deploy` judged the
configuration alone, so they reported a limit the running application does
not enforce: false assurance on a security control.

This release adds `WithRateLimit()` (`app.WithRateLimit`,
`nucleus.WithRateLimit`, `AppBuilder.WithRateLimit`), which mounts the
limiter the configuration declares on such an application, at the point of
the chain the default stack mounts it, and nothing while
`rate_limit_requests` is 0. The api starter and `nucleus serve
--without-defaults` carry it. An application that sets a limit and does not
pass the option keeps starting with the limit unenforced, as before, and
now says so in one ERROR line at boot.

What changes and when:

- **This release:** the ignored limit is reported, not refused. The boot
  log carries, once:

  ```
  level=ERROR msg="rate_limit_requests IGNORED: the configuration sets a rate limit and this application is built WithoutDefaults() without WithRateLimit(), so no limiter is mounted and no request is refused" requests=100 window=1m0s fix="add WithRateLimit() beside WithoutDefaults() — …" deprecation="DEP-2026-016: from v2.0.0 this configuration refuses to start"
  ```

  `nucleus doctor --check security` reports the same combination as a
  warning, and `deploy.rate_limit` as a warning, when they find the
  composition root beside the configuration; when they cannot find it,
  they say the limit is enforced only on the default stack or with
  `WithRateLimit()` instead of implying it is.

- **v2.0.0:** the same combination refuses to start, with the same message
  as the error.

`rate_limit_requests: 0`, written or not, is not reported: it asks for no
limit. The other `rate_limit_*` keys without `rate_limit_requests` are not
reported either: on any application they select nothing without it.

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()` whose
  configuration sets `rate_limit_requests` above 0 and that does not pass
  `WithRateLimit()`.
- Not affected: applications built with the defaults (the limiter is one of
  them), applications built `WithoutDefaults()` that set no limit, and
  applications that pass `WithRateLimit()`.
- No Go API is removed or changes shape. The additions are
  `app.WithRateLimit`, `nucleus.WithRateLimit` and
  `AppBuilder.WithRateLimit`. Unlike `StorageDeclared` and `MailDeclared`,
  no `Config` field records the declaration: `rate_limit_requests`
  defaults to 0 and nothing but the configuration writes it (`nucleustest`
  does not, as it does the mail driver), so the value is the declaration,
  and a `Config` built in Go that sets it is reported too.

## Migration Path

- Replacement: either mount the limiter the configuration declares, or
  stop declaring it.
  - `nucleus.New().FromConfigFile("nucleus.yml").WithoutDefaults().WithRateLimit()…`
    (or `app.New(cfg, app.WithoutDefaults(), app.WithRateLimit())`);
  - or set `rate_limit_requests` to 0 (or remove it, and
    `NUCLEUS_RATE_LIMIT_REQUESTS`) in that application's configuration.
- Behavior differences: with `WithRateLimit()` the limiter answers 429 past
  the budget. On an application built `WithoutDefaults()` no middleware
  decodes the bearer ahead of it, so it keys a request by its API key's
  owner (`WithAPIKeys`) and tenant, and by its client IP otherwise — a
  bearer-authenticated request is keyed by its address, and
  `rate_limit_by_role` sees every caller as `anonymous`. The default stack
  keys by user.
- Required app changes: one builder call, or one key.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-016-ignored-rate-limit-to-withratelimit.md`
- Detection rule: the boot log line `rate_limit_requests IGNORED` carrying
  `deprecation="DEP-2026-016…"`, or the `nucleus doctor --check security`
  warning naming this notice.
- Suggested rewrite: manual — add `.WithRateLimit()` after
  `.WithoutDefaults()` in the composition root, or set the key to 0.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that the combination
  still starts, refuses nothing (200, 200, 200 with
  `rate_limit_requests: 1`) and logs exactly one ERROR line naming
  `WithRateLimit()` and this notice; that with the option, and on the
  default stack, the second request answers 429 and nothing is said; and
  that nothing is said or mounted without a limit. `pkg/nucleus` pins the
  same through the builder; `internal/cli` pins the doctor and
  `health --deploy` reports, with and without a composition root, and that
  the api starter calls `WithRateLimit()`. The additions are listed in
  `contracts/baseline/api_exported_symbols.txt` (additions only).
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, plus the log line); after it, adding
  `WithRateLimit()` or setting the key to 0 restores a booting application.

## Timeline

- Announcement date: `2026-10-06`
- Review checkpoint: the set that certifies the release carrying this notice.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
