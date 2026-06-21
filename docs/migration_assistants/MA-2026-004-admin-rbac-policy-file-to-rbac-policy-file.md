# Migration Assistant: `admin_rbac_policy_file` → `rbac_policy_file`
## (and: adopting the orbit admin module)

- ID: `MA-2026-004`
- Pairs with: `docs/deprecations/DEP-2026-004-admin-rbac-policy-file-rename.md`
- Severity: `low` — mechanical one-line key rename; the deprecated alias keeps
  the old value working until removal. No runtime behaviour change.
- Status: `current`

---

## Part 1 — Rename `admin_rbac_policy_file` to `rbac_policy_file`

### Scope

Applications that set the RBAC policy-file path via the `admin_rbac_policy_file`
config key in `nucleus.yml` (or any Nucleus-loaded YAML / TOML file, or via the
`Config` struct in Go code). The key controlled which Casbin CSV file the
framework-level default-deny enforcer loads at startup (ADR-004).

Out of scope: applications that rely solely on programmatic policy construction
(`Enforcer.AddPolicy`, `Enforcer.Deny`, `Enforcer.AddRole`). Those do not read
a file and are unaffected.

### Detection

**Config file — search for the old key:**

```bash
# From the consumer repo root.
rg --type yaml 'admin_rbac_policy_file' .
rg --type toml 'admin_rbac_policy_file' .
```

**Go source — search for the struct field:**

```bash
rg 'AdminRBACPolicyFile' .
```

**Runtime signal — check application startup logs:**

```bash
# Any of these patterns in stdout/stderr on startup indicates the deprecated
# alias is still active.
grep 'admin_rbac_policy_file is deprecated' <(your-app 2>&1) || true
```

A repository is impacted if any of the above produces output.

### Rewrite Plan

| Old                               | New                            | Type          |
|-----------------------------------|--------------------------------|---------------|
| `admin_rbac_policy_file: <path>`  | `rbac_policy_file: <path>`     | YAML key rename |
| `Config.AdminRBACPolicyFile`      | `Config.RBACPolicyFile`        | Go field rename |

Both changes are purely mechanical. The value (the file path) is unchanged.

**YAML — before:**

```yaml
# nucleus.yml (pre-ADR-019 Slice 2)
admin_rbac_policy_file: rbac_policy.csv
```

**YAML — after:**

```yaml
# nucleus.yml (ADR-019 Slice 2+)
rbac_policy_file: rbac_policy.csv
```

**Go struct literal — before:**

```go
cfg := &app.Config{
    AdminRBACPolicyFile: "rbac_policy.csv",
}
```

**Go struct literal — after:**

```go
cfg := &app.Config{
    RBACPolicyFile: "rbac_policy.csv",
}
```

Automatic rewrite candidates:

```bash
# YAML files — in-place sed rename (review the diff before committing).
find . -name '*.yml' -o -name '*.yaml' | \
  xargs sed -i '' 's/admin_rbac_policy_file:/rbac_policy_file:/g'

# Go source — in-place sed rename.
find . -name '*.go' | \
  xargs sed -i '' 's/AdminRBACPolicyFile/RBACPolicyFile/g'
```

Review the diff carefully. The `sed` rewrites are conservative (exact token
match), but spot-check for any false positives in comments or string literals.

### Verification

After renaming:

```bash
# Confirm the old key is gone.
rg 'admin_rbac_policy_file' . && echo "STILL PRESENT — fix needed" || echo "OK"
rg 'AdminRBACPolicyFile'    . && echo "STILL PRESENT — fix needed" || echo "OK"

# Start the application; the deprecation WARN must not appear.
./your-app 2>&1 | grep 'admin_rbac_policy_file is deprecated' \
  && echo "WARN still firing — recheck config" || echo "OK"

# RBAC enforcement still active.
nucleus doctor --config nucleus.yml
```

### Rollback

The deprecated alias continues to work until `v0.12.0`. Reverting the YAML
rename restores the previous behavior with a startup WARN. No data is at risk.

---

## Part 2 — Adopting the orbit admin module

### Context

ADR-019 Slice 2 removes the in-core admin panel (`pkg/admin`, `app.App.Admin`,
`app.MountAdmin()`). That removal is a **hard clean break with no deprecation
cycle** (pre-v1.0 single-maintainer precedent, ADR-006/008/010). Apps that
relied on the built-in `/admin` UI must explicitly mount the `orbit` module
instead.

Orbit is a separate Go module (`github.com/jcsvwinston/orbit`) that embeds its
own SPA and mounts the admin panel in-process via the ADR-010 extension surface.
The feature set is at parity with the removed in-core panel.

### Detection

Check whether the application currently uses any of the following:

```bash
# MountAdmin or app.Admin field access in Go source.
rg 'MountAdmin\|\.Admin\b\|admin\.Panel\|admin\.NewPanel\|RegisterAdminModels' .

# Old admin_* config keys in YAML (all removed except the rbac alias above).
rg 'admin_prefix\|admin_title\|admin_auth_database\|admin_bootstrap_\|admin_cluster_\|admin_live_\|admin_trace_url_template' .
```

Any match means the application used the in-core admin and must migrate to orbit.

### Migration steps

**Step 1 — Add the orbit dependency.**

```bash
go get github.com/jcsvwinston/orbit@latest
go mod tidy
```

**Step 2 — Mount orbit in the application bootstrap.**

Remove any call to `app.MountAdmin()` and replace it with an `orbit.Module`
mount via the builder:

```go
// Before (in-core admin, removed in ADR-019 Slice 2):
a, err := app.New(cfg)
// …
a.MountAdmin()

// After (orbit module, explicit mount):
import "github.com/jcsvwinston/orbit"

a, err := app.New(cfg,
    app.WithExtensions(
        orbit.Module(orbit.Config{
            Prefix: "/admin",
            Title:  "My App Admin",
            // Auth:   orbit.NewDatabaseAuth(...),  // optional; defaults to DB-backed
        }),
    ),
)
```

If the application uses `app.WithoutDefaults()` and mounts extensions manually,
append `orbit.Module(...)` to the extensions list.

**Step 3 — Move old `admin_*` config keys into `orbit.Config`.**

The table below maps every removed `nucleus.yml` key to its `orbit.Config`
field equivalent. Set these values in Go when constructing `orbit.Config`, or
expose them via your own config struct; orbit does not read `nucleus.yml`
directly.

> **Note:** `orbit.Config` is currently flat — there are no nested sub-structs.
> Several admin options that had no analogue at orbit's initial extraction are
> listed as "not yet exposed". Reaching full config parity for the live-cluster,
> trace, and dedicated-auth-DB options is a planned orbit follow-up; operators
> relying on those features should track the orbit module's config surface.

| Removed `nucleus.yml` key         | `orbit.Config` field            | Notes |
|------------------------------------|---------------------------------|-------|
| `admin_prefix`                     | `Prefix`                        | Default `/admin` if omitted. |
| `admin_title`                      | `Title`                         | Default `"Orbit Admin"` if omitted. |
| `admin_bootstrap_username`         | `BootstrapUsername`             | Used once on first boot. |
| `admin_bootstrap_email`            | `BootstrapEmail`                | Used once on first boot. |
| `admin_bootstrap_password`         | `BootstrapPassword`             | Empty → random, printed to STDERR once. |
| `multitenant.enabled`              | `MultiTenantEnabled`            | |
| `multitenant.default`              | `MultiTenantDefault`            | |
| _(tenant ID list)_                 | `MultiTenantIDs`                | `[]string` of tenant identifiers. |
| `environment`                      | `Environment`                   | e.g. `"production"`, `"development"`. |
| _(migrations path)_                | `MigrationsPath`                | Path to orbit's own migration files. |
| _(audit log max size)_             | `AuditMaxSize`                  | Maximum audit-log size in MB. |
| `admin_auth_database`              | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_live_exclude_patterns`      | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_cluster_enabled`            | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_cluster_redis_url`          | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_cluster_channel`            | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_cluster_node_id`            | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_cluster_token`              | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |
| `admin_trace_url_template`         | _(not yet exposed in orbit.Config — tracked orbit enhancement)_ | |

Full example mapping a previous `nucleus.yml` admin block into an orbit mount:

```go
import "github.com/jcsvwinston/orbit"

orbitCfg := orbit.Config{
    Prefix:            "/admin",
    Title:             "Nucleus Admin",
    BootstrapUsername: "admin",
    BootstrapEmail:    "admin@localhost",
    BootstrapPassword: "", // random on first boot
    Environment:       "production",
    MigrationsPath:    "db/orbit/migrations",
    AuditMaxSize:      100,
    // MultiTenantEnabled, MultiTenantDefault, MultiTenantIDs: set if using
    // multi-tenant mode.
    //
    // admin_auth_database, admin_live_exclude_patterns, admin_cluster_*,
    // and admin_trace_url_template have no orbit.Config equivalent yet.
    // Track the orbit module for updates.
}

a, err := app.New(cfg,
    app.WithExtensions(orbit.Module(orbitCfg)),
)
```

**Step 4 — Remove the now-unused `admin_*` keys from `nucleus.yml`.**

All `admin_*` keys except `admin_rbac_policy_file` (handled in Part 1) are
ignored by `pkg/app` after the clean break. Remove them from config files to
avoid confusion; leaving them is harmless (unrecognised keys are silently
ignored by koanf with strict mode off) but noisy under `nucleus doctor`.

```yaml
# nucleus.yml — delete these lines (values that have an orbit.Config equivalent
# should be moved into orbit.Config in Go; the rest are no longer consumed):
# admin_prefix: /admin
# admin_title: Nucleus Admin
# admin_bootstrap_username: admin
# admin_bootstrap_email: admin@localhost
# admin_bootstrap_password: ""
# admin_auth_database: default          # no orbit.Config equivalent yet
# admin_live_exclude_patterns: ["/admin"]  # no orbit.Config equivalent yet
# admin_cluster_enabled: false          # no orbit.Config equivalent yet
# admin_cluster_redis_url: ""           # no orbit.Config equivalent yet
# admin_cluster_channel: ""            # no orbit.Config equivalent yet
# admin_cluster_node_id: ""            # no orbit.Config equivalent yet
# admin_cluster_token: ""              # no orbit.Config equivalent yet
# admin_trace_url_template: ""         # no orbit.Config equivalent yet
```

**Step 5 — Remove `RegisterAdminModels` calls.**

`app.App.RegisterAdminModels` is removed. Model registration for orbit's Data
Studio uses the standard model registry:

```go
// Before:
a.RegisterAdminModels(&User{}, &Post{})

// After: use the standard registry directly.
a.Models.Register(&User{}, model.ModelConfig{})
a.Models.Register(&Post{}, model.ModelConfig{})
// orbit discovers registered models via Runtime.Models() automatically.
```

### Verification

After adopting orbit:

```bash
# Application starts cleanly.
go build ./...
nucleus doctor --config nucleus.yml

# The admin UI is reachable at the configured prefix.
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/admin/
# Expected: 200 or 302 (redirect to login)

# No dead admin_* config warnings (nucleus doctor or startup logs).
./your-app 2>&1 | grep -E 'admin_prefix|admin_title|admin_auth_database|admin_bootstrap_|admin_live_|admin_cluster_|admin_trace_url_template' \
  && echo "Stale keys found — clean up nucleus.yml" || echo "OK"
```

### Rollback

There is no rollback path for the hard removal of the in-core admin (ADR-019
Slice 2 / ADR-006/008/010 clean-break precedent). To run the old panel, pin the
Nucleus version to the last pre-extraction release (`v0.9.x`) via `go.mod`.

### Compatibility Notes

- Orbit's behaviour is at feature parity with the removed in-core panel at the
  time of extraction. Any in-core admin feature that was already working will be
  present in orbit.
- The `nucleus_admin_users` table schema is owned by orbit after the extraction.
  Orbit runs its own migrations on first boot; no manual schema change is needed.
- The `__nucleus_admin_*` session-key namespace moves into orbit. Existing
  sessions created by the in-core panel are invalidated by the extraction; users
  must log in again after the upgrade.
- RBAC policy file: orbit reads the Casbin enforcer from the `Runtime.Authorizer()`
  accessor. The framework still loads the policy file configured by
  `rbac_policy_file` (or the deprecated `admin_rbac_policy_file` alias). No
  orbit-specific RBAC config is needed unless orbit is running behind a
  custom prefix that requires additional allow-list rows (orbit seeds its own
  bootstrap allow-list for its prefix automatically per ADR-004).
