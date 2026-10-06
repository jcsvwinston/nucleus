# Deprecation Notice: the profiler served unguarded by an application built WithoutDefaults() → refused at v2.0.0 unless an explicit opt-in guards it

- ID: `DEP-2026-018`
- Status: `active`
- Announced in: `Unreleased` (NU-124, 2026-10-06; the release that carries this change)
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

An application built `WithoutDefaults()` builds no enforcer. With the
profiler on it served those dumps to anyone who reached the port, unless the
application's own middleware refused them, and the boot log carried a WARN
telling the operator to "keep it behind a policy" that such an application
cannot have.

The application keeps serving the profiler, as before, and now says so in
one ERROR line at boot instead of the WARN.

What changes and when:

- **This release:** the combination is reported, not refused. The boot log
  carries, once:

  ```
  level=ERROR msg="profiler UNGUARDED: profiling_enabled mounts /debug/pprof and this application is built WithoutDefaults(), which builds no RBAC enforcer, so no policy can say who may read it — heap and goroutine dumps answer anyone who reaches the port, unless the application's own middleware refuses them" prefix=/debug/pprof env=development fix="set profiling_enabled: false, and serve net/http/pprof from a listener only operators reach when you need a profile — or build the application without WithoutDefaults() and grant /debug/pprof/* to an on-call role in the policy, never to anonymous" deprecation="DEP-2026-018: from v2.0.0 this configuration refuses to start unless an explicit opt-in guards the profiler"
  ```

  Under `env: production` the message names the stake outright —
  `profiler UNGUARDED in production: … anyone who reaches the port can
  download heap dumps of this production process, with the live memory they
  carry (session tokens, credentials, request payloads) …`.

  `nucleus doctor --check security` reports the same combination when it
  finds the composition root beside the configuration: a **high-risk
  setting** (error) in production, a setting to review (warning) elsewhere.
  Without a root to read — a deployed image carries the binary, not
  `main.go` — it says the profiler is guarded only on the default stack,
  instead of implying it is.

- **v2.0.0:** an application built `WithoutDefaults()` with
  `profiling_enabled: true` refuses to start, with the same message as the
  error, unless an explicit opt-in guards the profiler (see Notes).

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()` whose
  configuration sets `profiling_enabled: true` (or a `Config` built in Go
  with `ProfilingEnabled` set: the key defaults to false and nothing but the
  configuration writes it, so the value is the declaration).
- Not affected: applications built with the defaults — the profiler there
  sits behind default-deny, and the boot WARN stays — and applications built
  `WithoutDefaults()` that leave the profiler off.
- `WithOpenAuthz()` is not part of this notice. It is an explicit opt-out of
  enforcement on the default stack, made in code and documented as unsafe
  outside development, and the profiler page already says the two switches
  have to be considered together.
- No Go API is removed or changes shape, and nothing is added.

## Migration Path

- Replacement, by intent:
  - The application does not need the profiler in this deployment: set
    `profiling_enabled: false` (the default).
  - The application needs a profile now and then: serve `net/http/pprof`
    yourself from a listener only operators reach — `127.0.0.1:6060`, or a
    port the network does not route — rather than on the application's
    public port.
  - The application should authorize its routes against a policy anyway:
    build it with the defaults — remove `WithoutDefaults()` — and grant
    `/debug/pprof/*` to an on-call role, never to `anonymous`.
- Behavior differences: on the default stack every route, not only the
  profiler, answers only to the callers a policy grants.
- Required app changes: one key, or a few lines that start a private
  listener, or one builder call removed and a policy reviewed.

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
  stack, where the profiler answers an anonymous request 403. `internal/cli`
  pins the doctor report in each state.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, with an ERROR line in place of a WARN);
  after it, `profiling_enabled: false` restores a booting application.

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

The explicit opt-in is not part of this change. Its shape — an option that
takes the guard (a middleware, or a predicate on the request) and mounts the
profiler behind it, the sibling of `WithRateLimit()` — is a decision of its
own, together with the `WithAuthz()` that DEP-2026-017 leaves open: a
core-only application that opts back into default-deny would put the
profiler behind it as the default stack does. If either lands before
v2.0.0, this notice names it as the first replacement.

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
