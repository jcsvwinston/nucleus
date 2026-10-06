# Deprecation Notice: the profiler served unguarded by an application built WithoutDefaults() without WithAuthz() → refused at v2.0.0 unless WithAuthz() guards it

- ID: `DEP-2026-018`
- Status: `active`
- Announced in: `Unreleased` (NU-124, 2026-10-06; `WithAuthz()` named as the
  opt-in by the owner's decision of the same day, before any release carried
  it — the release that carries this change)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is
  a **behaviour change of a configuration combination**, not a symbol
  removal: no `// Deprecated:` marker carries it.
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`profiling_enabled: true` mounts `net/http/pprof` under `/debug/pprof`: the
index, `cmdline`, `profile`, `symbol`, `trace`, and the named profiles —
`heap` and `goroutine` among them. A heap dump is not a summary: it carries
the process's live memory, session tokens, credentials and request payloads
included. That is why the profiler is off by default and deliberately not in
the anonymous bootstrap allow-list: on the default stack the default-deny
enforcer answers an anonymous request `403`, and only a policy that grants
the paths opens them.

An application built `WithoutDefaults()` builds no enforcer unless it also
carries `WithAuthz()`. Without it, with the profiler on, it served those
dumps to anyone who reached the port, unless the application's own
middleware refused them, and the boot log carried a WARN telling the
operator to "keep it behind a policy" that such an application cannot have.

The application keeps serving the profiler, as before, and now says so in
one ERROR line at boot instead of the WARN. `WithAuthz()` puts the profiler
behind the default stack's default-deny gate: `403` to an anonymous
request, and the default stack's WARN instead of the ERROR.

What changes and when:

- **This release:** the combination is reported, not refused. The boot log
  carries, once:

  ```
  level=ERROR msg="profiler UNGUARDED: profiling_enabled mounts /debug/pprof and this application is built WithoutDefaults() without WithAuthz(), which builds no RBAC enforcer, so no policy can say who may read it — heap and goroutine dumps answer anyone who reaches the port, unless the application's own middleware refuses them" prefix=/debug/pprof env=development fix="add WithAuthz() beside WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithAuthz(), or app.New(cfg, app.WithoutDefaults(), app.WithAuthz()) — and grant /debug/pprof/* to an on-call role in the policy, never to anonymous; or set profiling_enabled: false, and serve net/http/pprof from a listener only operators reach when you need a profile" deprecation="DEP-2026-018: from v2.0.0 this configuration refuses to start unless WithAuthz() guards the profiler"
  ```

  Under `env: production` the message names the stake outright —
  `profiler UNGUARDED in production: … anyone who reaches the port can
  download heap dumps of this production process, with the live memory they
  carry (session tokens, credentials, request payloads) …`.

  `nucleus doctor --check security` reports the same combination when it
  finds the composition root beside the configuration and the root does not
  call `WithAuthz()`: a **high-risk setting** (error) in production, a
  setting to review (warning) elsewhere. Without a root to read — a
  deployed image carries the binary, not `main.go` — it says the profiler
  is guarded only on the default stack or with `WithAuthz()`, instead of
  implying it is.

- **v2.0.0:** an application built `WithoutDefaults()` with
  `profiling_enabled: true` refuses to start, with the same message as the
  error, unless `WithAuthz()` guards the profiler — the explicit opt-in the
  refusal accepts (see Notes).

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()`, and without
  `WithAuthz()`, whose configuration sets `profiling_enabled: true` (or a `Config` built in Go
  with `ProfilingEnabled` set: the key defaults to false and nothing but the
  configuration writes it, so the value is the declaration).
- Not affected: applications built with the defaults, and applications
  built `WithoutDefaults()` with `WithAuthz()` — the profiler there sits
  behind default-deny, and the boot WARN stays — and applications built
  `WithoutDefaults()` that leave the profiler off.
- `WithOpenAuthz()` is not part of this notice. It is an explicit opt-out of
  enforcement on the default stack, made in code and documented as unsafe
  outside development, and the profiler page already says the two switches
  have to be considered together.
- No Go API is removed or changes shape. `WithAuthz()` is added —
  `app.WithAuthz`, `nucleus.WithAuthz`, `AppBuilder.WithAuthz` — as the
  opt-in.

## Migration Path

- Replacement, by intent:
  - The application should authorize its routes against a policy, and stay
    core-only: add `WithAuthz()` beside `WithoutDefaults()`, and grant
    `/debug/pprof/*` to an on-call role, never to `anonymous`. Every other
    route then answers only to the callers a policy grants as well.
  - The application does not need the profiler in this deployment: set
    `profiling_enabled: false` (the default).
  - The application needs a profile now and then: serve `net/http/pprof`
    yourself from a listener only operators reach — `127.0.0.1:6060`, or a
    port the network does not route — rather than on the application's
    public port.
  - The application wants the rest of the default stack too: build it with
    the defaults — remove `WithoutDefaults()` — and grant `/debug/pprof/*`
    to an on-call role, never to `anonymous`.
- Behavior differences: with `WithAuthz()`, as on the default stack, every
  route, not only the profiler, answers only to the callers a policy
  grants.
- Required app changes: one builder call added and a policy reviewed, or
  one key, or a few lines that start a private listener, or one builder
  call removed and a policy reviewed.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-018-unguarded-profiler-to-private-listener.md`
- Detection rule: the boot log line `profiler UNGUARDED` carrying
  `deprecation="DEP-2026-018…"`, or the `nucleus doctor --check security`
  finding naming this notice.
- Suggested rewrite: manual — the choice between the paths above is the
  application's.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that an application
  built `WithoutDefaults()` with `profiling_enabled` still starts, still
  answers `GET /debug/pprof/` with 200, and logs exactly one ERROR line
  naming the prefix, `WithoutDefaults()` and this notice, and no WARN
  advising a policy; that in production the line names heap dumps and live
  memory; and that nothing is said with the profiler off, or on the default
  stack, where the profiler answers an anonymous request 403 — nor beside
  `WithoutDefaults()` with `WithAuthz()`, where it answers 403 too.
  `internal/cli` pins the doctor report in each state, `WithAuthz()` in the
  root included.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, with an ERROR line in place of a WARN);
  after it, `WithAuthz()` or `profiling_enabled: false` restores a booting
  application.

## Timeline

- Announcement date: `2026-10-06`
- Review checkpoint: the set that certifies the release carrying this notice.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The repository's security policy does not already demand refusing an
unauthenticated profiler at boot: `SECURITY.md`, the ADRs and the frozen
posture baseline (`contracts/baseline/security_posture.txt`) say nothing
about it, and the profiler's own documentation states the invariant —
"behind authorization when on" — as a property of the default stack, while
documenting `WithOpenAuthz()` as the case where it does not hold. Refusing
now would stop applications that boot today; QADR-0010 groups that into the
major.

The explicit opt-in is `WithAuthz()`, which landed before v2.0.0 by the
owner's decision of 2026-10-06 ("offer it as an option"): a core-only
application that opts back into default-deny puts the profiler behind it as
the default stack does. The other shape this notice considered — an option
that takes a guard of its own (a middleware, or a predicate on the request)
and mounts the profiler behind it — was not built: `WithAuthz()` is the
guard the framework already has.

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
