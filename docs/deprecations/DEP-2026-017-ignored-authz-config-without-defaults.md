# Deprecation Notice: authorization configuration an application built WithoutDefaults() never enforces → refused at v2.0.0

- ID: `DEP-2026-017`
- Status: `active`
- Announced in: `Unreleased` (NU-123, 2026-10-06; the release that carries this change)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is
  a **behaviour change of a configuration combination**, not a symbol
  removal: no `// Deprecated:` marker carries it.
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

An application built `WithoutDefaults()` builds no RBAC enforcer and mounts
no default-deny middleware: that is documented ("no authz"), and the api
starter says so when it is generated. Two keys of its configuration ask for
that enforcer all the same, and were ignored without a word:

- `rbac_policy_file`: the policy file is never read, and no route is
  authorized by it. A reviewer who reads the configuration sees routes that
  are protected.
- `metrics_public: false`: on the default stack it takes the metrics path
  off the anonymous allow-list, so only the callers a policy grants can read
  it. Here, with the Prometheus exporter linked, the endpoint answers
  anyone.

The application keeps starting with both ignored, as before, and now says
so in one ERROR line at boot. There is no option that builds the enforcer
on such an application, as `WithStorage()`, `WithMail()` and
`WithRateLimit()` build theirs: authorization in Nucleus is default-deny
over every route (ADR-004), and that is the default stack.

What changes and when:

- **This release:** the ignored keys are reported, not refused. The boot
  log carries, once:

  ```
  level=ERROR msg="authz configuration IGNORED: the configuration asks for authorization and this application is built WithoutDefaults(), which builds no RBAC enforcer, so none of it is enforced" keys="rbac_policy_file" rbac_policy_file=rbac_policy.csv fix="build the application without WithoutDefaults() …" deprecation="DEP-2026-017: from v2.0.0 this configuration refuses to start"
  ```

  `nucleus doctor --check rbac` reports the same combination as a warning
  when it finds the composition root beside the configuration, instead of
  "RBAC policy file found".

- **v2.0.0:** the same combination refuses to start, with the same message
  as the error.

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()` whose
  configuration sets `rbac_policy_file`, or sets `metrics_public: false`
  while the metrics path is served (the Prometheus exporter linked and
  `metrics_path` not empty).
- Not affected: applications built with the defaults, and applications
  built `WithoutDefaults()` that set neither key — or set
  `metrics_public: false` with no metrics endpoint served, which gates
  nothing that exists. `WithOpenAuthz()` does not change the report: it
  opts out of enforcement, and the keys still ask for it.
- No Go API is removed or changes shape, and nothing is added. No `Config`
  field records the declaration: `rbac_policy_file` defaults to empty and
  `metrics_public` to true in both loaders, and nothing but the
  configuration writes either, so the values are the declarations. A
  `Config` built in Go that serves metrics and leaves `MetricsPublic` false
  asks for what the default stack would give it — a gated endpoint — and is
  reported too.

## Migration Path

- Replacement, by intent:
  - The application should authorize its routes: build it with the
    defaults — remove `WithoutDefaults()` — so the default-deny enforcer
    loads the policy file. The default stack also builds mail (the `noop`
    driver unless one is declared), the local storage default and the rate
    limiter.
  - The application authorizes in its handlers, or not at all: remove
    `rbac_policy_file` and `metrics_public: false`, and keep the metrics
    path private at the network layer.
- Behavior differences: on the default stack every route answers only to
  the callers a policy grants; a route nobody granted answers 403.
- Required app changes: one builder call removed and a policy reviewed, or
  two keys deleted.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-017-ignored-authz-config-to-default-stack.md`
- Detection rule: the boot log line `authz configuration IGNORED` carrying
  `deprecation="DEP-2026-017…"`, or the `nucleus doctor --check rbac`
  warning naming this notice.
- Suggested rewrite: manual — the choice between the two paths above is the
  application's.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that an application
  built `WithoutDefaults()` with `rbac_policy_file` still starts, builds no
  authorizer, answers an unpoliced route, and logs exactly one ERROR line
  naming the key and this notice; that nothing is said without the keys,
  for `metrics_public: false` with no metrics endpoint, or on the default
  stack; and which keys are reported in which states. `internal/cli` pins
  the doctor report.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, plus the log line); after it, removing
  the keys restores a booting application.

## Timeline

- Announcement date: `2026-10-06`
- Review checkpoint: the set that certifies the release carrying this notice.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

An option that builds the enforcer on a `WithoutDefaults()` application
(a `WithAuthz()`, the sibling of `WithStorage()`) is not part of this
change: whether a core-only application can opt back into default-deny, and
whether it then also decodes the bearer globally, is a decision of its own.
If it lands before v2.0.0, this notice names it as the first replacement.

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
