# Deprecation Notice: authorization configuration and module policy rows an application built WithoutDefaults() never enforces → refused at v2.0.0

- ID: `DEP-2026-017`
- Status: `active`
- Announced in: `Unreleased` (NU-123, 2026-10-06; extended to module policy
  rows by NU-126 the same day, before any release carried it — the release
  that carries this change)
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

The rows mounted modules declare in `Module.Policies` ask for it too
(NU-126). They are loaded into the default stack's enforcer; here there is
none, and they were discarded without a word — deny rows included. A module
that lets anonymous callers read and keeps writes for a role, which is what
`nucleus generate module` writes, answered every write to anyone.

The application keeps starting with all of it ignored, as before, and now
says so at boot: one ERROR line for the keys, and one for the module rows.
There is no option that builds the enforcer on such an application, as
`WithStorage()`, `WithMail()` and `WithRateLimit()` build theirs:
authorization in Nucleus is default-deny over every route (ADR-004), and
that is the default stack.

What changes and when:

- **This release:** the ignored keys are reported, not refused. The boot
  log carries, once:

  ```
  level=ERROR msg="authz configuration IGNORED: the configuration asks for authorization and this application is built WithoutDefaults(), which builds no RBAC enforcer, so none of it is enforced" keys="rbac_policy_file" rbac_policy_file=rbac_policy.csv fix="build the application without WithoutDefaults() …" deprecation="DEP-2026-017: from v2.0.0 this configuration refuses to start"
  ```

  `nucleus doctor --check rbac` reports the same combination as a warning
  when it finds the composition root beside the configuration, instead of
  "RBAC policy file found".

  Module rows that would refuse something are reported once as well,
  after the modules mount:

  ```
  level=ERROR msg="module policies DISCARDED: mounted modules declare policy rows and this application is built WithoutDefaults(), which builds no RBAC enforcer, so none of them is enforced — the routes they keep from anonymous callers answer anyone, and their deny rows refuse no one" modules="notes (3 rows, 0 deny)" rows=3 deny_rows=0 unguarded="POST /notes, PUT /notes/{id}, DELETE /notes/{id}" fix="build the application without WithoutDefaults() so the default-deny enforcer loads the rows (ADR-004) — or, for a module of your own, remove its rows and refuse the callers in its own middleware or handlers" deprecation="DEP-2026-017: from v2.0.0 this configuration refuses to start"
  ```

  `unguarded` lists the routes the modules registered that their rows do
  not open to an anonymous caller — refused on the default stack, answered
  here. A module is reported when it has such a route or declares a deny
  row. A module whose rows grant the anonymous subject every action on
  every route it serves — the accounts module (`accounts.FromRuntime()`) —
  loses nothing where nothing is enforced, and is not reported. The doctor
  cannot report module rows: they are declared in Go, in the module's
  package, not in the composition root it reads; its `rbac` check says on
  such an application that they are discarded.

- **v2.0.0:** the same combinations refuse to start, with the same messages
  as the errors.

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()` whose
  configuration sets `rbac_policy_file`, or sets `metrics_public: false`
  while the metrics path is served (the Prometheus exporter linked and
  `metrics_path` not empty).
- The same application mounting a module whose `Policies` declare a deny
  row, or leave a route the module serves closed to the anonymous subject
  (an action not granted, or a grant only to a named role or user).
- Not affected: applications built with the defaults, and applications
  built `WithoutDefaults()` that set neither key — or set
  `metrics_public: false` with no metrics endpoint served, which gates
  nothing that exists — and mount no module whose rows refuse anything.
  `WithOpenAuthz()` does not change the report on the keys: it opts out of
  enforcement, and the keys still ask for it. The module rows are a
  different case there: `WithOpenAuthz()` builds the enforcer and loads
  them, and mounts no middleware to consult it, which is the opt-out it
  names.
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
  - A module of the application's own declares rows: remove them and refuse
    the callers in the module's own middleware or handlers. A module the
    application does not own — its rows cannot be removed from the
    composition root — leaves the default stack as the only path today.
- Behavior differences: on the default stack every route answers only to
  the callers a policy grants; a route nobody granted answers 403.
- Required app changes: one builder call removed and a policy reviewed, or
  two keys deleted.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-017-ignored-authz-config-to-default-stack.md`
- Detection rule: the boot log lines `authz configuration IGNORED` and
  `module policies DISCARDED` carrying `deprecation="DEP-2026-017…"`, or the
  `nucleus doctor --check rbac` warning naming this notice.
- Suggested rewrite: manual — the choice between the two paths above is the
  application's.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that an application
  built `WithoutDefaults()` with `rbac_policy_file` still starts, builds no
  authorizer, answers an unpoliced route, and logs exactly one ERROR line
  naming the key and this notice; that nothing is said without the keys,
  for `metrics_public: false` with no metrics endpoint, or on the default
  stack; and which keys are reported in which states. `pkg/nucleus` pins
  that a module whose rows keep writes from anonymous callers still has its
  writes answered on a `WithoutDefaults()` application, with exactly one
  ERROR line naming the module, its deny rows and the routes; that the same
  module is enforced and unreported on the default stack; which modules and
  routes are reported for which rows; and `pkg/accounts` that the account
  flows in the api starter's shape are not reported. `internal/cli` pins
  the doctor report.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, plus the log lines); after it, removing
  the keys, and the module rows or the module, restores a booting
  application.

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
The module rows make that decision more pressing: without it, a core-only
application that mounts a third-party module whose rows refuse something
has no way to keep booting at v2.0.0 but the default stack.

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
