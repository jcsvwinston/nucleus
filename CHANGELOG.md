# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
while in pre-1.0 mode (`v0.x.y`).

## [Unreleased]

### Security

- **Bumped three dependencies to clear govulncheck-flagged CVEs (the CI smoke gate).** `golang.org/x/net` v0.54→v0.55 (GO-2026-5026), `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` v1.35→v1.43 (GO-2026-4985 — also realigns the exporter with the rest of the otel tree, already at v1.43.0), and `github.com/go-jose/go-jose/v4` v4.1.3→v4.1.4 (GO-2026-4945, indirect). `govulncheck ./...` now reports zero called vulnerabilities. All are patch/minor bumps within their existing major versions; no public API change, no new dependency, firewall + freeze contracts unaffected. (The nested `admin/{agent,proto,server}` modules are not covered by the root `govulncheck ./...` lane — a separate security-hygiene follow-up.)

- **Session cookie is now `Secure` by default. BREAKING (operational): plain-HTTP deployments must opt out.** `session_cookie_secure` flipped its default from `false` to `true` (`app.DefaultConfig()` / `app.LoadConfig` with no override), so the framework's session cookie now refuses to ride over plain HTTP — closing the Phase 2b security-auditor MED-1 finding and matching the CSRF cookie posture ([ADR-008](docs/adrs/ADR-008-csrf-followups.md), `Secure: !InsecureCookie`). Deployments that terminate TLS upstream and speak HTTP to the app, or that run over `http://` in local development, must now set `session_cookie_secure: false` explicitly or sessions will not persist. A config-level `null` reverts to the new secure default, so it cannot silently re-open the gap. Secure-by-default per SPEC §2.4.
- **CSRF middleware now emits structured observability and ships secure-by-default cookies.** `pkg/router` CSRF middleware now plumbs an optional `*slog.Logger` (defaulting to `slog.Default()`; `router.DefaultStack` plumbs the router's logger) and emits a `WARN` line whenever the XSRF-TOKEN cookie encryption fails server-side, and a `DEBUG` line whenever an incoming `X-XSRF-TOKEN` header fails to decrypt (public-endpoint noise — opt-in at production log levels). The cookie `Secure` flag default flipped from `false` to `true`: the `_csrf` and `XSRF-TOKEN` cookies issued by the zero-value `CSRFOptions{}` (the path `router.WithCSRF` takes) now refuse to ride over plain HTTP. See [ADR-008](docs/adrs/ADR-008-csrf-followups.md).
- **Structured-logger secret redaction, on by default.** `observe.NewLogger` previously installed a `slog.Handler` with no `ReplaceAttr` — any code that logged a secret-bearing attribute (`authorization`, `password`, `token`, a session `cookie`, …) emitted it verbatim. `NewLogger` now redacts: the value of any attribute whose key is in a curated, case-insensitive denylist (`observe.DefaultRedactedKeys()` — `authorization`, `cookie`, `set-cookie`, `password`, `secret`, `token`, `api_key`, `access_token`, `private_key`, …) is replaced with `observe.RedactionPlaceholder` (`[REDACTED]`). The key and log-line shape are unchanged. Pure standard library (`slog.HandlerOptions.ReplaceAttr`); no new dependency. See [ADR-007](docs/adrs/ADR-007-slog-secret-redaction.md).
- **Canonical redaction set now covers the framework's compound secret config keys.** `observe.DefaultRedactedKeys()` gained `jwt_secret`, `admin_bootstrap_password`, `admin_cluster_token`, `session_redis_url`, `admin_cluster_redis_url`, `secret_access_key` (S3), and `account_key` (Azure) — keys whose leaf names the prior short atomic entries (`secret`, `password`, `token`) did not match, so they previously logged in cleartext. They now redact in both structured logs **and** `nucleus config print --effective` (one canonical surface). See [ADR-007](docs/adrs/ADR-007-slog-secret-redaction.md) / [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 3a.
- **Canonical redaction set now covers S3 access-key IDs.** `observe.DefaultRedactedKeys()` gained `access_key_id` and `aws_access_key_id` — the public half of an AWS/S3 credential pair. The prior denylist already covered `secret_access_key` (the secret half, landed in Phase 3a); the access-key ID was omitted and previously appeared in cleartext in both structured logs and `/_/config` JSON output. Both keys now redact via the one canonical surface. Deployments that intentionally logged an attribute named `access_key_id` for non-secret purposes will see `[REDACTED]` there; rename the attribute or use `observe.NewLoggerWithRedaction` with the key excluded to opt out. See [ADR-007](docs/adrs/ADR-007-slog-secret-redaction.md) / [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 3b.
- **Config referential validation rejects inconsistent combinations at load (ADR-010 §2 layer 4).** `pkg/nucleus` now fails fast with the new `ErrInvalidConfigReference` sentinel when: `session_cookie_samesite=none` is set together with `session_cookie_secure=false` (browsers silently drop a non-Secure `SameSite=None` cookie, breaking sessions); `mail_driver=smtp` is set without `smtp_host` or with `smtp_port<=0`; or a module's `Requires()` names a database alias not present in `databases` — fulfilling the long-documented ADR-010 §6 boot guarantee (`module "<name>" requires database "<alias>" which is not configured`, previously never enforced). The cross-field checks run at both `FromConfigFile` load and `nucleus.Run`; the module check runs at `Run` (modules are mounted on the builder, not in the config file). BREAKING (validation): a config that previously booted into a broken state now fails loud at startup — correct the config. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) §2/§6.
- **CSRF token comparison is now constant-time.** `pkg/router` CSRF middleware compared the submitted token against the expected token with `!=`, a byte-by-byte comparison that short-circuits on the first mismatch and leaks — through response latency — how many leading bytes an attacker guessed correctly. It now uses `crypto/subtle.ConstantTimeCompare`. See [ADR-006](docs/adrs/ADR-006-csrf-hardening.md).
- **CSRF `EncryptionKey` is no longer derived from the cookie name.** `CSRFOptions.defaults()` previously filled an empty `EncryptionKey` with `sha256(CookieName)` — a globally-predictable AES key, since the cookie name is public and defaults to a constant. Any deployment that enabled `EnableXSRFCookie` without an explicit key had a forgeable `XSRF-TOKEN` cookie. The weak-key derivation is removed; the key is now mandatory and validated (see the `Changed` note below). `encryptToken` / `decryptToken` no longer slice the key with `key[:32]` (which panicked at request time on a short key and silently truncated a long one) — they pass the key to `aes.NewCipher`, which validates the length. A latent bug where a too-short ciphertext decrypted to `""` with a nil error is also fixed. See [ADR-006](docs/adrs/ADR-006-csrf-hardening.md).

### Fixed

- **AutoMigrate no longer generates un-indexable string columns on MySQL and MS SQL Server.** A Go `string` field used as a PRIMARY KEY or in an index mapped to `TEXT` (MySQL) / `NVARCHAR(MAX)` (MSSQL), which both engines reject at migration time — MySQL `Error 1170: BLOB/TEXT column used in key specification without a key length`, MSSQL `Column … is of a type that is invalid for use as a key column in an index`. Key-bound string columns now render as a bounded `VARCHAR(255)` / `NVARCHAR(255)` (within both engines' index-key byte limits); non-key string columns are unchanged (`TEXT` / `NVARCHAR(MAX)`). PostgreSQL and SQLite were already correct (`TEXT` is indexable) and are unchanged. This unblocks the live MySQL (required) and MSSQL (exploratory) AutoMigrate CI lanes.
- **Admin bootstrap now works on MS SQL Server and Oracle.** `App.New` failed at startup for any deployment whose admin-auth database was MSSQL or Oracle: `admin.EnsureBootstrapAdminUser` emitted a single hardcoded `CREATE TABLE IF NOT EXISTS … INTEGER NOT NULL DEFAULT 0` for the admin users table, which MSSQL rejected (`Incorrect syntax near 'nucleus_admin_users'`) and Oracle rejected (`ORA-03076`, `NOT NULL DEFAULT` ordering). The CREATE statement is now dialect-aware (`IF OBJECT_ID(...) IS NULL` + `NVARCHAR`/`BIT` for MSSQL; a PL/SQL block swallowing ORA-00955 + `VARCHAR2`/`NUMBER(1) DEFAULT 0 NOT NULL` for Oracle; the portable `IF NOT EXISTS` form unchanged for SQLite/PostgreSQL/MySQL). `BootstrapAdminConfig` gains a `System` field (`App.New` passes the admin-auth DB's `System()`); an empty `System` preserves the prior portable behaviour. The previously-disabled exploratory test `TestSQLMatrix_AutoMigrate_Exploratory` is now wired into the **MSSQL** CI exploratory lane (fully green). The Oracle lane remains deferred pending a separate model-scaffold identifier-casing fix (see the next entry + the NOTE in `.github/workflows/ci.yml`).
- **Oracle `App.AutoMigrate` no longer fails with ORA-06550.** Fixing the admin bootstrap above let the live Oracle exploratory lane reach the `AutoMigrate` step, which exposed a second latent bug: `pkg/model.BuildOracleMigrationScaffold` emitted each PL/SQL block with a trailing `/` line. `/` is a SQL\*Plus / SQLcl script terminator — it is not valid PL/SQL, and the Go driver (go-ora) raises `ORA-06550` on it when `App.AutoMigrate` (`sqlDB.Exec`) or the file-based `Migrator` (`tx.Exec`) sends the scaffold straight to the driver (never through SQL\*Plus). The `/` terminator is removed; the scaffold now mirrors the no-`/` PL/SQL blocks `pkg/db` already uses for the migrations / checksums tables, which the driver accepts. With this fix Oracle `AutoMigrate` executes without error. (Two known follow-ups, both tracked in `CURRENT_ITERATION.md`: (1) `BuildOracleMigrationScaffold` quotes identifiers, creating case-sensitive lowercase tables that diverge from the unquoted-uppercase convention used by the rest of the framework's Oracle path and expected by `USER_TAB_COLUMNS` introspection — this is why the Oracle `TestSQLMatrix_AutoMigrate_Exploratory` lane stays deferred; (2) Oracle scaffolds for models with secondary indexes emit multiple PL/SQL blocks, which the single-`Exec` AutoMigrate path cannot run as one batch.)
- **Oracle `AutoMigrate`d tables are now visible to the rest of the framework (identifier casing).** Resolves follow-up (1) above. `pkg/model.BuildOracleMigrationScaffold` wrapped every identifier in double quotes (`CREATE TABLE "users"`), creating case-sensitive **lower-case** tables — but the CRUD runtime layer (`pkg/model/crud.go`) emits **bare** identifiers (Oracle folds to UPPER), the migrations/checksums bootstrap creates unquoted tables, and `USER_TAB_COLUMNS` introspection matches via `UPPER(...)`. A scaffolded table was therefore invisible to CRUD, introspection, and schema-drift. The scaffold now emits **unquoted** identifiers (Oracle folds to upper case), making the whole Oracle path consistent; the Oracle `TestSQLMatrix_AutoMigrate_Exploratory` CI lane is re-enabled. No public Go API change. The framework's Oracle identifier strategy is pinned in [ADR-011](docs/adrs/ADR-011-oracle-identifier-casing.md). (Known limitation, pre-existing and tracked: unquoted identifiers break on Oracle reserved words — e.g. a column named `comment`/`number` — which already affected the bare-identifier CRUD layer; selective quoting is a separate follow-up.)
- **Oracle `AutoMigrate` works for models with secondary indexes (multi-block execution).** Resolves follow-up (2) above. An Oracle scaffold for a model with a secondary index emits several `BEGIN…END;` PL/SQL blocks (CREATE TABLE, then one CREATE INDEX per index), but go-ora executes only one block per `Exec`, so `App.AutoMigrate` (and the file `Migrator`) failed on the second block. The scaffold now separates blocks with a `/` on its own line (the idiomatic Oracle/SQL\*Plus block terminator), and a new `db.ExecScript(execer, system, script)` splits Oracle scripts on those `/` lines and Execs each block in order — **stripping the `/` first**, so go-ora never sees it (the `ORA-06550` concern from the previous entry still holds; this refines, not reverts, that fix). Both `App.AutoMigrate` and the file `Migrator` (apply + rollback) route through `ExecScript`; non-Oracle dialects pass straight through to a single `Exec`, unchanged. The live AutoMigrate fixture gained a secondary index so the Oracle CI lane exercises the path. (Note: the pure-Go SQLite/`modernc` driver has the same one-statement-per-Exec limitation for `;`-separated scripts — a general non-Oracle splitter is a possible future extension, out of scope here.)
- **MySQL and SQLite `App.AutoMigrate` work for models with secondary indexes (multi-statement execution).** Resolves the SQLite/MySQL limitation flagged in the previous entry. After the AutoMigrate fixture gained a secondary index, the live **MySQL** CI lane failed: `db.ExecScript` sent the whole multi-statement scaffold (`CREATE TABLE …; CREATE INDEX …`) in a single `Exec` for every non-Oracle dialect, but `go-sql-driver/mysql` (without `multiStatements=true`) and the pure-Go `modernc` SQLite driver execute exactly one statement per `Exec` and reject a batch — MySQL failed the `CREATE INDEX` with `Error 1064 (42000)`. `ExecScript` now splits `mysql` and `sqlite` scripts into their individual `;`-terminated statements and Execs each in order; the splitter is quote- and comment-aware, so a `;` inside a single-quoted string (with `''`/backslash escapes), a `"`/`` ` ``-quoted identifier, or a `--` / `/* */` comment is not treated as a statement boundary. `postgresql` and `mssql` keep the single-`Exec` passthrough (their drivers accept multi-statement batches in one round trip) and `oracle` keeps its `/`-delimited block splitting — all unchanged. No public Go API change (`db.ExecScript`'s signature is unchanged); both `App.AutoMigrate` and the file `Migrator` (apply + rollback) route through it.

### Added

- **`nucleus.OpenAPISpec` type and `AppBuilder.WithOpenAPI` — fluent OpenAPI document endpoint (ADR-010 Phase 4, Slice 2).** `pkg/nucleus` gains a new exported type `OpenAPISpec` with two fields (`Pattern string`, `Provider openapi.DocumentProvider`), an `App.OpenAPI *OpenAPISpec` field on the direct-struct surface, and an `AppBuilder.WithOpenAPI(pattern string, provider openapi.DocumentProvider)` builder method. When either surface is used, `nucleus.Run` calls `app.App.MountOpenAPI` to register a JSON OpenAPI document endpoint at the declared path — no manual route wiring required. Purely additive contract: four new exported names (`OpenAPISpec`, `OpenAPISpec.Pattern`, `OpenAPISpec.Provider`, `AppBuilder.WithOpenAPI`), `App.OpenAPI` field; no removed or renamed symbol. Backward compatible — call sites that do not declare an OpenAPI spec are unaffected. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 4, Slice 2.
- **`nucleus.Runtime` — managed-resource handle for module lifecycle hooks (ADR-010 Phase 4, Gap 1).** A new interface type in `pkg/nucleus` that modules receive in `OnStart` and `OnShutdown`. Exposes three methods: `DB() *sql.DB` (the framework-managed connection pool bound to the module's `DefaultDB` alias — no need for the module to open its own connection), `AutoMigrate(models ...any) error` (delegates to the app's migration layer), and `Logger() *slog.Logger` (the app-level structured logger). Additive — no existing call site changes unless the module also adopts the new hook signature (see `### Changed` below). Contract baseline net delta: `type:Runtime` + 3 `iface-method` entries; no removed symbol. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 4, Gap 1.
- **First reference application reintroduced: `examples/mvc_api` (ADR-010 Phase 4, Slice 1).** A minimal MVC + REST API (one `notes` resource) demonstrating the fluent surface — `nucleus.New().FromConfigFile().WithoutDefaults().Mount(...).Start()`, `Module[C]` lifecycle, `Router.Resource` + the REST sub-interfaces, and `Context`. It lives in the root module so it compiles against local `pkg/` and is build/test-checked by CI. Schema is managed by explicit SQL migrations via `nucleus migrate up` (the fluent path does not auto-migrate). The `examples/*` tree was purged in Phase 1; this is the first app reintroduced and the foundation the website's include-from-source pattern will import in a later slice. Two framework gaps it surfaced are documented in its README as tracked follow-ups: modules cannot yet reach the managed `*sql.DB` (the example opens its own connection in `OnStart` pending a `nucleus.Runtime` handle), and `Run` calls `Routes` before `OnStart` (handled with a lazy DB accessor).
- **Config field-semantic validation (ADR-010 §2 layer 3).** `pkg/nucleus` now rejects configuration values that bind cleanly but are semantically invalid — out of range, not a recognised enum member, or a negative duration — with a new `ErrInvalidConfigValue` sentinel naming the offending key, its value, and the accepted set/range. Runs in both `AppBuilder.FromConfigFile` (fail-fast at load) and the package-level `Run` (direct-struct surface). Validated: enums `session_store` ∈ {memory,sql,redis}, `log_level` ∈ {debug,info,warn,warning,error}, `log_format` ∈ {json,text}, `session_cookie_samesite` ∈ {strict,lax,none} (empty = default); ranges `port`/`smtp_port` ∈ [0,65535] (0 = OS-assigned) and non-negative `rate_limit_requests`/`rate_limit_burst`; non-negative server/session/JWT/rate-limit durations. **Behaviour note:** values that were previously accepted and then silently defaulted or rejected late (e.g. an unknown `session_store`, a typo'd `session_cookie_samesite` that quietly fell back to `lax`) now fail early at config load. `mail_driver`/`storage.provider` (plugin-extensible / validated downstream), `env` (freeform), and `multitenant.resolver` (auto-normalised) are intentionally not covered; layer 3 is a `pkg/nucleus` guarantee and is not added to the lower-level `pkg/app.New`.
- **`pkg/db.ExecScript(execer, system, script string) error`** — executes a migration script, splitting it into individually-executable units per SQL dialect. Oracle scripts are split on `/`-terminator lines (stripped before Exec, since go-ora rejects a bare `/` and runs one PL/SQL block per Exec); all other dialects pass through to a single `Exec`. Used by `App.AutoMigrate` and the file `Migrator`.

- **`ConfigSource.Line int` — 1-based source-line provenance for YAML file keys (ADR-010 Phase 3.1).** `pkg/nucleus.ConfigSource` gains an additive `Line int` field (`json:"line,omitempty"`). For keys loaded from YAML/YML files, `Line` reports the 1-based line number where the key was defined, walking the `go.yaml.in/yaml/v3` `yaml.Node` AST. TOML positions are available only via go-toml's explicitly-unstable API and are therefore out of scope; JSON has no standard line API; both format kinds report `Line == 0`. The CLI renders the long form `[yaml:path:line]` when a line is present and `[kind:path]` otherwise. The `"default"`, `"env"`, and `"runtime"` source kinds always carry `Line == 0`. Known limitation: keys produced by `_append`/`_remove` suffix operators and keys reached through YAML anchors or merge keys carry no line. The `go.yaml.in/yaml/v3` module is promoted from indirect to direct dependency (it was already in the module graph via koanf; the promotion is confined to unexported helpers). Contract baseline rebaselined additively: `ConfigSource.Line` is a new exported field (+1); no removed or renamed symbol — backward compatible. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 3.1.
- **`GET /_/config` — auth-gated runtime effective-config endpoint (ADR-010 Phase 3b).** `pkg/nucleus.Run` now mounts a `GET /_/config` handler that returns the application's effective merged configuration as JSON, with secrets redacted via the canonical `observe.DefaultRedactedKeys()` (extended in this phase — see `### Security` above). The endpoint is the runtime counterpart to `nucleus config print --effective` (Phase 3a): both views are produced by the same `LoadEffective` call and share one redaction surface. The endpoint is mounted **only when the admin subsystem is active** — a `WithoutDefaults()` application does not expose it (404). Access is gated by admin-session authentication (`admin.NewDatabaseAdminAuth`): an anonymous request receives 403; a valid admin session receives 200 with `Cache-Control: no-store`. The app-wide Casbin default-deny (ADR-004) is satisfied via a bootstrap-subject allow policy added by the nucleus layer for the `/_/config` path — no change to the stable `pkg/authz` package. Applications constructed via the direct-struct `nucleus.Run(App{})` path (no config files) receive a `"runtime"`-kind snapshot flattened from the live `core.Config`. No new exported `pkg/*` symbol is introduced; the Phase 3a `LoadEffective` / `EffectiveConfig` API is reused unchanged. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 3b.
- **`nucleus config print --effective` + `pkg/nucleus.LoadEffective` — effective-config inspection (ADR-010 Phase 3a).** `LoadEffective(paths []string, extraKeys ...string) (EffectiveConfig, error)` merges the configured files exactly as `FromConfigFile` does and returns every effective key with its value and per-key source — `ConfigSource{Kind, Path}` where `Kind` is `default` or `yaml`/`toml`/`json` and `Path` is the file. Sensitive values are redacted via the canonical `observe.DefaultRedactedKeys()` plus a parent-aware rule for `databases.<alias>.url`/`.dsn` (no second redaction list). The new `config print --effective` CLI command renders `key = value [kind:path]`; `--config` is repeatable (merged left to right) and `--json` emits the structured `EffectiveConfig`. New stable `pkg/nucleus` API: `LoadEffective`, `ConfigSource`, `EffectiveValue`, `EffectiveConfig`. Provenance is source-kind+path only — env/flag-layer attribution and `file:line` numbers are deferred to Phase 3.1, and the auth-gated `/_/config` runtime endpoint to Phase 3b. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) §5.
- **`pkg/db.NewModuleMigrator(db, path, moduleName, logger)` — module-scoped migration namespacing (ADR-010 Phase 2d).** Creates a `*Migrator` that records applied-migration and checksum rows under a `<moduleName>/<file-id>` storage key in `nucleus_schema_migrations` and `nucleus_schema_migration_checksums`. Closes the cross-module filename-collision class: two modules that both ship `001_init.up.sql` and share a database alias no longer fail the second `Up()` with a PRIMARY KEY collision. The legacy unscoped `NewMigrator` constructor is unchanged and continues to write raw file IDs — host applications that pre-date the module pattern keep their existing migration history with zero churn. Operators migrating an existing unscoped Migrator to a module-scoped one need a one-time manual `UPDATE nucleus_schema_migrations SET id = 'modname/' || id WHERE …` (and the same `UPDATE` on `nucleus_schema_migration_checksums`); the framework intentionally does not auto-promote existing rows. `Migrator.Drift` is now ownership-aware: an unscoped Migrator ignores foreign-module rows (those with a `/` in the storage ID); a module-scoped Migrator only reports drift for its own rows. `Migrator.Status` and `Migrator.Drift` continue to return human-readable file IDs (no namespace prefix) — operators see filenames, not storage keys. `NewModuleMigrator` panics at construction time on an empty name or a name containing `/` or NUL, since constructor-time misuse is a programming error. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) §16.
- **`pkg/nucleus.AppBuilder.WithUnknownFields(mode string)` unknown-fields mode selector and `NUCLEUS_ENV=production` production strict-override (ADR-010 Phase 2c).** Two modes are accepted via the new `UnknownFieldsStrict` / `UnknownFieldsWarn` exported constants: strict (the default) keeps the Phase 2a behaviour — unknown configuration keys reject the load with `ErrUnknownConfigKeys`; warn emits a `WARN`-level slog event listing the offending keys and proceeds with the load (the unknowns are stripped so they cannot leak into the merged config). Activating warn mode also emits a startup `WARN` so misconfigured deployments surface in operational telemetry before reaching production. The `NUCLEUS_ENV=production` environment variable (case-insensitive, whitespace-trimmed; constant `EnvProduction`) is the operator escape hatch: when set, the loader forces the mode back to strict regardless of code-level configuration and emits a `WARN` recording the override. Misuse of the mode value records the new `ErrInvalidUnknownFieldsMode` deferred sentinel; calling `WithUnknownFields(...)` AFTER `FromConfigFile` records a misorder error analogous to `WithConfigStrict`. New exported names: `AppBuilder.WithUnknownFields`, `UnknownFieldsStrict`, `UnknownFieldsWarn`, `EnvProduction`, `ErrInvalidUnknownFieldsMode`. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) §15.
- **`pkg/nucleus` multi-file `FromConfigFile` with TOML/JSON parsers + merge engine (ADR-010 Phase 2b).** `AppBuilder.FromConfigFile(path1, path2, ...)` now loads, validates, and merges any number of configuration files in left-to-right precedence (`struct defaults < file[0] < file[1] < … < file[N-1]`). Each file is size-capped at `MaxConfigFileBytes` (1 MiB) before parsing — uniform DoS guard across all three formats. Three parsers are now wired: `.yaml`/`.yml`, `.toml` (via `koanf/parsers/toml/v2`), and `.json`. The Phase 2a `ErrUnsupportedConfigFormat` path for TOML/JSON inputs is removed; those extensions now parse successfully.
- **ADR-010 §3 merge semantics on `FromConfigFile`.** Scalars replace; maps deep-merge; lists replace by default. Two suffix operators provide additive / subtractive list semantics that survive every parser the loader supports: `<key>_append` appends listed entries to the existing collection (e.g. `log_redact_extra_keys_append: [foo]`); `<key>_remove` removes listed entries (idempotent — removing a missing element is a no-op). Operator keys are stripped before the strict-schema check so they do not trip `ErrUnknownConfigKeys`.
- **`null` reverts to default; non-nullable security keys reject `null`.** A `null` value in any merged file resets the key to its `app.DefaultConfig()` value (rather than to Go's zero value, which would silently degrade security booleans). The non-nullable security set (`cors.origins`, `auth.providers`, `authz.policy_path`, `session.secret`, `jwt_secret`) rejects `null` outright with the new `ErrSecurityKeyNotNullable` sentinel — ADR-010 §14. Of the named keys, `jwt_secret` is enforced today; the remaining four are guarded as forward-compat placeholders for when those `app.Config` fields land.
- **`AppBuilder.WithConfigStrict(strict bool)` builder method.** Toggles the merge-engine's mixed-format guard for subsequent `FromConfigFile` calls. With strict mode off (the default), a file list mixing two or more of YAML / TOML / JSON emits a `WARN`-level slog event and proceeds. With strict mode on, the load returns the new `ErrMixedConfigFormats` sentinel. Must be called BEFORE `FromConfigFile` to affect the same load.
- **Phase 2a wildcard-matcher fix (regression closed).** `keyMatchesAny` previously recognised only the literal `*` as a wildcard segment, while `app.ContractConfigKeyPatterns()` returns patterns with `<alias>` / `<site>` / `<tenant>` placeholders. Any production config that set `databases.<some>.url` would have failed strict-schema validation with "unknown configuration key" — a Phase 2a bug discovered during Phase 2b planning. The matcher now recognises both forms; the `[]` slice suffix on the last segment of patterns like `log_redact_extra_keys[]` is also stripped during pattern compilation since koanf flattens slice values under the bare key.
- **New exported sentinels in `pkg/nucleus`:** `ErrSecurityKeyNotNullable` (null on a non-nullable security key), `ErrMixedConfigFormats` (`WithConfigStrict(true)` rejection). New builder method: `AppBuilder.WithConfigStrict(bool)`. Freeze baseline net delta: +3 entries. New deps: `github.com/knadh/koanf/parsers/toml/v2`, `github.com/knadh/koanf/parsers/json`, `github.com/knadh/koanf/providers/confmap`. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) §3 + §14.
- **`pkg/app.NormalizeRuntimeConfig` exported (ADR-010 Phase 2b).** The previously-internal `normalizeRuntimeConfig` (database-alias canonicalisation, multi-site/multi-tenant resolver normalisation, admin defaulting) is now exported as `app.NormalizeRuntimeConfig(cfg *Config)`. The multi-file loader in `pkg/nucleus.FromConfigFile` calls it so its returned `*Config` is indistinguishable from the env-var path produced by `app.LoadConfig`. Callers that construct `*app.Config` directly — plugin authors, test helpers — can now call the same normalisation without replicating internal logic. Safe to call with `nil` (no-op). Backward compatible — old call sites continue to work.
- **`observe.NewLoggerWithRedaction` + `RedactionConfig`** — additive constructor for explicit control over the new structured-logger secret redaction: `ExtraKeys` (extend the denylist), `Placeholder` (override `[REDACTED]`), `Disabled` (opt out — code-level only, no config switch). `observe.DefaultRedactedKeys()` exposes the built-in denylist for auditing; `observe.RedactionPlaceholder` is the default masked value. The `log_redact_extra_keys` config key (lifecycle `transitional`) threads `ExtraKeys` through `App.New`. See [ADR-007](docs/adrs/ADR-007-slog-secret-redaction.md).
- **`router.NewCSRFMiddleware`** — an additive, error-returning CSRF middleware constructor: `func(CSRFOptions) (func(http.Handler) http.Handler, error)`. Returns `router.ErrCSRFEncryptionKey` on a misconfiguration instead of panicking. `CSRFMiddleware` keeps its signature and becomes the `regexp.MustCompile`-style wrapper that panics on the same error. See [ADR-006](docs/adrs/ADR-006-csrf-hardening.md).
- **`CSRFOptions.Logger *slog.Logger`** — optional structured logger for CSRF encrypt/decrypt observability. Defaults to `slog.Default()`; `router.DefaultStack` plumbs the router's logger automatically so apps built through `router.WithCSRF` inherit redaction, attributes, and sink from the rest of the app. See [ADR-008](docs/adrs/ADR-008-csrf-followups.md).
- **`db.Migrator.SchemaDrift(ctx, models...)`** — schema-level drift detection complementing the file-level `Migrator.Drift()`. Introspects the live database for all five supported engines via `pragma_table_info` (SQLite), `information_schema.columns` (PostgreSQL, MySQL, MSSQL with `SCHEMA_NAME()` filtering and `@p1` placeholders), and `USER_TAB_COLUMNS` (Oracle with `:1` placeholders, UPPER-case identifier fallback for hand-rolled DDL, and `NULLABLE = 'Y'/'N'` polarity). Four drift kinds reported: `schema_missing_table`, `schema_missing_column`, `schema_extra_column`, `schema_column_nullability`. `db.ErrSchemaDriftUnsupported` now fires only for engines outside the supported set. Closes audit `2026-05-14-post-sprint-readiness` §3 row 9 / §7 task 8. MSSQL/Oracle introspection landed via the ADR-009 addendum dated 2026-05-15.
- **Live-DB SchemaDrift tests** — `pkg/db/schema_drift_live_test.go::TestSQLMatrix_SchemaDrift` (Postgres + MySQL, `NUCLEUS_SQL_MATRIX_URL`) and `TestSQLMatrix_SchemaDrift_Exploratory` (MSSQL + Oracle, `NUCLEUS_SQL_EXPLORATORY_URL`) provision a fixture table and exercise all four drift kinds against each live container as subtests. CI workflow `.github/workflows/ci.yml` updated so the matrix lanes pick them up.
- **Live-DB integration tests for `app.AutoMigrate`** — `pkg/app/automigrate_live_test.go::TestSQLMatrix_AutoMigrate` (Postgres + MySQL, `NUCLEUS_SQL_MATRIX_URL`) and `TestSQLMatrix_AutoMigrate_Exploratory` (MSSQL + Oracle, `NUCLEUS_SQL_EXPLORATORY_URL`) call `App.AutoMigrate` against the live container, then introspect `information_schema` to verify the table and column NOT NULL / nullable polarity match what `model.ExtractMeta` declared. The 2026-05-15 SchemaDrift iteration fixed the CI workflow `-run` regex so the **required-lane** `TestSQLMatrix_AutoMigrate` (PG/MySQL) is now actually exercised — it had been compiling but not executing. The exploratory counterpart `TestSQLMatrix_AutoMigrate_Exploratory` (MSSQL/Oracle) is now wired into the exploratory lanes as well, following the `pkg/admin` bootstrap users-table dialect-aware DDL fix (see `### Fixed` above) that closes the `Incorrect syntax near 'nucleus_admin_users'` (MSSQL) / `ORA-03076` (Oracle) errors the test surfaced. Closes part of the gap flagged by audit `2026-05-14-post-sprint-readiness` §5 risk 4 / §7 task 7.
- **ES256 JWT signing (P-256).** `pkg/auth` gains an `ES256` `SigningAlgorithm` and a `SigningKey.ECDSAPrivate` field. `App.New` builds, signs, validates, and publishes ES256 keys end to end; `JWKSHandler` emits `kty: EC` / `crv: P-256` JWKs with RFC 7518 §6.2 fixed-length coordinates. `pkg/app/jwt_setup.go` loads ES256 keys from SEC1 or PKCS#8 PEM via `pem_path` / `pem_env`. Only the P-256 curve is accepted — a P-384/P-521 key with `algorithm: ES256` fails fast at `App.New`. Pure standard library (`crypto/ecdsa`, `crypto/elliptic`); no new dependency. See [ADR-005](docs/adrs/ADR-005-es256-and-aws-secrets-manager.md).
- **AWS Secrets Manager resolver for JWT key material.** New package `pkg/auth/secrets` with a `Resolver` interface, an `EnvResolver` (zero-dependency, resolves `env:NAME` and bare names), and an `AWSSecretsManagerResolver`. `JWTKeySpec.secret_env` and `pem_env` are now resolver references: a bare name or `env:NAME` reads the process environment (unchanged behaviour); an `aws-sm:<secret-id>` reference reads AWS Secrets Manager, with an optional `#<json-key>` fragment to extract one field of a JSON-object secret. The AWS SDK client is constructed lazily — only when a `jwt_keys[]` entry actually uses the `aws-sm:` scheme — so deployments that do not use AWS Secrets Manager never trigger AWS credential resolution. No AWS SDK type appears in any stable `pkg/*` signature (dependency firewall enforced). See [ADR-005](docs/adrs/ADR-005-es256-and-aws-secrets-manager.md).

### Changed

- **Behaviour change (`pkg/nucleus.Run`): mounted module `Models()` are now registered with the application model registry before `OnStart` (ADR-010 Phase 4, Slice 2).** Previously `Module[C].Models` was captured by the framework but never consumed at startup — `Run` populated the slice internally but did not pass it to `app.App`'s model registry, so generic CRUD/AutoMigrate metadata was absent and the admin panel (when mounted) had no per-model entries to display. `Run` now calls the registry for each mounted module's declared models before invoking module `OnStart`, in declaration order. Effect: a `Module[C]` with `Models: []any{T{}}` automatically populates AutoMigrate metadata and — when the admin subsystem is active — the admin panel, with per-model display driven by the model's `admin:` struct tags. No API surface change; no call site requires updating. Backward compatible — modules that declare no `Models` are unaffected. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 4, Slice 2.
- **BREAKING (`pkg/nucleus` module hook signature — ADR-010 Phase 4, Gap 1/Gap 2): `ModuleSpec.OnStart`/`OnShutdown` and the `Module[C]` func fields now receive `nucleus.Runtime` instead of `*nucleus.App`.** Signatures change from `func(ctx context.Context, a *nucleus.App, cfg C) error` to `func(ctx context.Context, rt nucleus.Runtime, cfg C) error`. Module authors replace `a.Config.Databases[...]` pool-opening code with `rt.DB()`. The sole internal consumer (`examples/mvc_api`) is updated in the same change. Pre-`v1.0` clean break per ADR-006/ADR-008 precedent — no DEP/MA artefact. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 4, Gap 1.
- **BREAKING (`pkg/nucleus.Run` lifecycle ordering — ADR-010 Phase 4, Gap 2): `OnStart` is now invoked BEFORE `Routes` (was: Routes registered first).** A module initialises its resources (database handles, caches, background workers) in `OnStart`; its `Routes` closure then captures those resources directly with no lazy accessor needed. Any module that relied on `Routes` running before `OnStart` — e.g. one that registered a handler referencing an uninitialised field — must move that initialisation into `OnStart`. Pre-`v1.0` clean break; only `examples/mvc_api` consumed this ordering and is updated in the same change. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 4, Gap 2.
- **Behaviour change (`pkg/nucleus`, `FromConfigFile`→`Run` path): `NUCLEUS_`-prefixed environment variables now override config-file values (ADR-010 Phase 3.1).** Prior to this change the fluent builder path (`FromConfigFile` + `Run`) ignored the environment entirely — `app.LoadConfig` applied env overrides for the direct-config path, but the file-based fluent path did not. The `loadMerged` step now applies a koanf `env.Provider` with the `NUCLEUS_` prefix and `__`→`.` transform (identical to `app.LoadConfig`) AFTER the file loop, honouring the documented ADR-010 §4 precedence `defaults < files < env`. Operators who previously set `NUCLEUS_*` variables expecting them to take effect via the fluent path will see them honoured for the first time. Unknown `NUCLEUS_`-prefixed variables (not in the schema) are silently ignored — env is an ambient namespace, unlike config files where unknown keys are strict errors. **Operator notice — boot error on empty security-key env var:** setting a non-nullable security key (e.g. `NUCLEUS_JWT_SECRET=`) to an empty string is now a boot error, mirroring the file-layer `null` guard (`ErrSecurityKeyNotNullable`) — an empty env value cannot silently disable signing. Env-sourced keys are attributed as `ConfigSource{Kind:"env", Path:"NUCLEUS_VAR_NAME"}` in `nucleus config print --effective` and `GET /_/config`, rendering as `[env:NUCLEUS_VAR_NAME]`. Backward compatible for deployments that did not set `NUCLEUS_*` env vars; no API surface changed. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 3.1.
- **BREAKING (`pkg/nucleus` rewrite — ADR-010 Phase 1 Foundation): the legacy fluent chain is replaced wholesale.** The pre-Phase-1 surface (`nucleus.New().Port().Host().SQLite().Postgres().MySQL().WithAdmin().SPA().Templates().Static().Cors().Provide().Model().AutoMigrate().Run()`, the `Resource(path, controller)` shape requiring a five-method controller, the `RouterGroup` struct, the legacy `Load(path)` that panicked on error) is removed entirely. The new surface — canonical `nucleus.App{}` struct embedding `app.Config`, generic `nucleus.Module[C any]` with `Build() ModuleSpec`, `nucleus.Router` interface with three coexisting registration styles (flat, REST `Resource(path, controller, nucleus.Methods(...))` with explicit verb registration, and nested `Group(prefix, func(g Router))`), three coexisting entry surfaces (fluent builder, direct struct, bootstrap pattern) producing equal `App{}` values per `pkg/nucleus/equivalence_test.go` — lands in this PR. `FromConfigFile` is shape-only in Phase 1 and returns `ErrConfigLoaderNotImplemented` at `Build`/`Start`/`Serve` time; the five-layer validator and merge engine arrive in Phase 2. Pre-`v1.0` clean break per the ADR-006 / ADR-008 precedent — no DEP/MA artefacts, no WARN-wrapped legacy methods. See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md).
- **BREAKING (`pkg/nucleus.FromConfigFile` is now operational — ADR-010 Phase 2a): the Phase 1 stub is replaced by a real single-file YAML loader.** `AppBuilder.FromConfigFile(path)` now loads the named file via koanf, applies struct defaults from `app.DefaultConfig()`, and returns a populated `nucleus.App` when called through `Build`/`Start`/`Serve`. Three validation guards land alongside: a **1 MiB per-file size cap** (`MaxConfigFileBytes`) enforced before parsing — eliminates anchor-expansion / deep-nesting DoS classes against `gopkg.in/yaml.v3`; **strict-unknown-fields schema validation** against `app.ContractConfigKeyPatterns()` — unknown keys surface as `ErrUnknownConfigKeys` with did-you-mean hints for likely typos (Levenshtein distance ≤3 on the final segment); and **extension-based parser inference** — `.yaml`/`.yml` work today, `.toml`/`.json` produce a targeted `ErrUnsupportedConfigFormat` referencing Phase 2b. Multi-file `FromConfigFile(a.yaml, b.yaml)` fails fast with a Phase 2b reference until the merge engine lands. `ErrConfigLoaderNotImplemented` is removed (clean break — pre-`v1.0` Phase-1 stub now retired). Three new exported sentinels (`ErrConfigFileTooLarge`, `ErrUnsupportedConfigFormat`, `ErrUnknownConfigKeys`) and one constant (`MaxConfigFileBytes`) join the `pkg/nucleus` baseline. New dep: `github.com/knadh/koanf/providers/rawbytes` (zero-go, sibling of the YAML provider already in tree). See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) §2.
- **BREAKING (`examples/*` removed): every example application is removed; new reference applications will land in v0.9.X.** Owner decision dated 2026-05-16, recorded in ADR-010: the original Phase 1 plan rewrote the two `examples/ecommerce_dashboard/backend/*` consumers in the same PR. Instead, the entire `examples/*` tree (`admin-quickstart`, `balancer`, `ecommerce_dashboard`, `fleetmanager`, `ministore`, `mvc_api`, `plugins`) was removed, alongside the runnable lab scripts (`scripts/cluster-{start,stop}.sh`, `scripts/dev/run_admin_cluster_lab.{sh,ps1}`) and the example-dependent docs (`docs/ADMIN_CLUSTER_LAB.md`, `docs/reference/PLUGIN_EXAMPLES.md`). The compatibility harness loses its three fixture profiles (`minimal-api`, `admin-heavy`, `plugin-heavy`) for this window and runs a `core-build` placeholder; the fixture profiles return with the new reference applications in v0.9.X (ADR-010 Phase 4). The `Dockerfile` now builds and ships the `nucleus` CLI rather than the previous `examples/mvc_api` server. Migration is empty for external users (there were none); operators downstream that previously consumed the example-server Docker image should pin to a pre-2026-05-16 tag until v0.9.X.
- **Behaviour change (`pkg/observe`, stable surface): `NewLogger` redacts secret-keyed attributes by default.** A deployment that intentionally logged a field under a denylisted key (e.g. an opaque non-secret named `token`) now sees `[REDACTED]` there. This is the intended security default per [ADR-007](docs/adrs/ADR-007-slog-secret-redaction.md); the escape hatch is `observe.NewLoggerWithRedaction` with the key omitted, a renamed attribute, or `RedactionConfig.Disabled`. No `DEP-` entry (no symbol removed or renamed).
- **BREAKING (CSRF XSRF-cookie config): `EncryptionKey` is mandatory and must be exactly 32 bytes when `EnableXSRFCookie` is `true`.** `pkg/router` is a `stable` surface; this is a deliberate behaviour change per [ADR-006](docs/adrs/ADR-006-csrf-hardening.md). An application that called `CSRFMiddleware` with `EnableXSRFCookie: true` and no (or a non-32-byte) `EncryptionKey` previously started successfully with a weak/truncated key; it now **panics at startup** (or, via `NewCSRFMiddleware`, returns `router.ErrCSRFEncryptionKey`). Migration: set `EncryptionKey` to exactly 32 bytes, sourced from the environment or a secret manager — see `docs/guides/CSRF_GUIDE.md`. Deployments with `EnableXSRFCookie: false` (the default) are unaffected: `EncryptionKey` stays optional and unvalidated for them.
- **BREAKING (CSRF config field type): `CSRFOptions.EncryptionKey` is now `[]byte` (was `string`).** Raw AES-256 key material is bytes, not a string; the type now matches the rest of the framework's key-material conventions (`crypto/aes`, `pkg/auth`). Migration is mechanical: wrap the env-var read in `[]byte(...)` at the construction site (`EncryptionKey: []byte(os.Getenv("CSRF_ENCRYPTION_KEY"))`). Pre-`v1.0` SemVer permits the change; recorded under [ADR-008](docs/adrs/ADR-008-csrf-followups.md).
- **BREAKING (CSRF cookie polarity flip): `CSRFOptions.Secure bool` replaced by `CSRFOptions.InsecureCookie bool`.** The cookie `Secure` flag default flipped from `false` to `true` (security-by-default per SPEC.md §2 principle 4). The zero-value `CSRFOptions{}` literal — the path `router.WithCSRF` takes — now issues `_csrf` and `XSRF-TOKEN` cookies with `Secure: true`. Migration: code that previously wrote `Secure: true` removes the field (it is now the default); code that intentionally ran with `Secure: false` (local-dev plain HTTP) sets `InsecureCookie: true` instead. Recorded under [ADR-008](docs/adrs/ADR-008-csrf-followups.md).
- **Changed (`nucleus new` scaffolder — ADR-010 Phase 4): both `api` and `mvc` templates now generate a minimal skeleton on the fluent `pkg/nucleus` surface, with no baked-in demo feature code.** The generated project contains: a composition-root `main.go` (at repo root; `go run .`) whose only content is `nucleus.New().FromConfigFile("nucleus.yml").[WithoutDefaults().]Start()` with no modules mounted; `nucleus.yml` pre-populated with sensible defaults; `.gitignore`; a `README.md` pointing to the docs and to `examples/mvc_api` as the working reference application; and an empty `migrations/` directory. The `mvc` template additionally generates a minimal `rbac_policy.csv` (wired via `admin_rbac_policy_file`) that grants anonymous access only to the built-in `/healthz` endpoint — the rest of the app is default-deny Casbin. The `api` template uses `WithoutDefaults()` and ships no RBAC file (fully open). No `internal/<resource>` feature modules, no seeds, no workers, and no demo routes are generated; the `examples/mvc_api` reference application serves that role. The `nucleus new` command name, flags, and positional arguments are unchanged — no CLI contract change. Backward compatible for existing projects (only newly scaffolded projects are affected). See [ADR-010](docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md) Phase 4.

### Dependencies

- **`github.com/aws/aws-sdk-go-v2/config` and `.../service/secretsmanager`** added (direct). First cloud-vendor SDK in the tree, gated entirely to `pkg/auth/secrets` and linked into the credential path only when an operator references the `aws-sm:` scheme. Added under [ADR-005](docs/adrs/ADR-005-es256-and-aws-secrets-manager.md) with a `dependency-impact` review.

## [0.7.0] - 2026-05-14

### Compatibility statement

`v0.7.0` contains two pre-`v1.0` breaking changes, each with a documented
migration path and an opt-out:

- Built-in `sendgrid` mail provider removed — see
  [DEP-2026-002](docs/deprecations/DEP-2026-002-builtin-sendgrid-provider.md)
  / [MA-2026-002](docs/migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md).
- Casbin default-deny enforcer mounted by `App.New` — see
  [ADR-004](docs/adrs/ADR-004-casbin-default-deny-mount.md); opt out with
  `app.WithOpenAuthz()`. The related policy-CSV format change is covered by
  [DEP-2026-003](docs/deprecations/DEP-2026-003-casbin-policy-csv-3col-to-4col.md)
  / [MA-2026-003](docs/migration_assistants/MA-2026-003-casbin-policy-csv-3col-to-4col.md).

Both are permitted under the pre-`v1.0` exception rule in
`docs/governance/DEPRECATION_TEMPLATE.md` (removals are exception-only and
must ship migration notes). The MSSQL/Oracle post-sprint stability drill on
`main` returned 10/10 + 10/10 (100%/100%, READY). Critical dependency
changes since `v0.6.0` (`go-mssqldb` v1.8.2→v1.10.0, `otlptracehttp`
v1.35.0→v1.43.0) were reviewed and accepted — see the Dependencies section
below.

### Added

- **AutoMigrate scaffolds for MSSQL and Oracle.** `pkg/model.BuildMSSQLMigrationScaffold` and `pkg/model.BuildOracleMigrationScaffold` extend the dialect-aware scaffolder to the enterprise engines that were previously rejected by `App.AutoMigrate` with `db.ErrAutoMigrate`. MSSQL output uses bracket-quoted identifiers (`[name]`), `BIGINT IDENTITY(1,1) PRIMARY KEY` for auto-increment, and `IF OBJECT_ID(..., 'U') IS NULL` + `sys.indexes` lookups for idempotency. Oracle output uses double-quoted identifiers, `NUMBER GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY`, and PL/SQL blocks that swallow `ORA-00955` (table/index already exists), `ORA-01418` (index does not exist), and `ORA-00942` (table does not exist), the same idempotency pattern used by the migrations-table bootstrap. Pipes through `pkg/app.buildAutoMigrateScaffold` so `app.New(cfg).AutoMigrate(...)` now works for all five supported engines (SQLite, PostgreSQL, MySQL, MSSQL, Oracle). Unknown engines still return `db.ErrAutoMigrate`. Scaffolds are tested with the same string-matching pattern as the Postgres/MySQL scaffolds; live-DB integration tests against MSSQL/Oracle containers remain a follow-up tracked in the post-sprint readiness audit.
- **Checksum-based migration drift detection.** `Migrator.Drift()` (`pkg/db/migrate.go`) now reports a second drift kind, `checksum_mismatch`, when a `.up.sql` file has been edited in place after the migration was applied. Computed as SHA-256 hex of the file content at apply time and recorded in a new sibling table `nucleus_schema_migration_checksums` (dialect-aware DDL parallels `nucleus_schema_migrations`). Stored in the same transaction as the migration application so the checksum and the applied-marker can never disagree. Pre-checksum migrations (rows that exist in `nucleus_schema_migrations` but not in `nucleus_schema_migration_checksums`) are not falsely reported as drift. New `DriftEntry` fields `ExpectedChecksum` and `ActualChecksum` are populated only for `checksum_mismatch` entries. Closes the file-level-only limitation flagged by the 2026-05-13 audit; schema-introspection drift (`information_schema` vs migrations) remains a separate follow-up.
- **Casbin policy CSV migration helper** — `authz.MigrateCSVPolicyFile(path, defaultEffect string) (CSVMigrationReport, error)` (`pkg/authz/migrate.go`) rewrites legacy 3-argument policy rows (`p, sub, obj, act`) into the 4-argument deny-override form (`p, sub, obj, act, allow|deny`). Idempotent, preserves comments / blank lines / grouping rows, atomic write. Closes the "CSV migration helper for 4-column rows" follow-up carried over from PR #41 and paired with [DEP-2026-003](docs/deprecations/DEP-2026-003-casbin-policy-csv-3col-to-4col.md) / [MA-2026-003](docs/migration_assistants/MA-2026-003-casbin-policy-csv-3col-to-4col.md). The `pkg/authz/enforcer.go` godoc has carried a forward reference to this helper since PR #41; the helper now exists.
- **Circuit-breaker autowrap for mail and storage** — `App.New` now wraps
  `mail.Sender.Send` and remote `storage.Store` operations (Put / Get /
  Delete / Exists / List / Copy / SignedURL) with `pkg/circuit.Breaker`
  by default. Closes the "primitive exists ≠ product uses it" gap for
  [#46](https://github.com/jcsvwinston/nucleus/issues/46). New config
  keys: `mail_circuit_breaker.{enabled,failure_threshold,cooldown,
  half_open_max_concurrent}` and `storage.circuit_breaker.{enabled,
  failure_threshold,cooldown,half_open_max_concurrent}`. Defaults are
  enabled, threshold 5, cooldown 30s, half-open budget 1. The `noop`
  mail driver and the `local` storage provider are never wrapped.
  `mail.HealthChecker` (the SMTP HELO probe used by `/healthz`)
  bypasses the breaker so a recovering dependency is observable while
  Send is short-circuited. `storage.ErrNotFound` is not counted as a
  breaker failure (a missing object is a normal outcome). See updated
  `docs/reference/CONFIG_KEY_REGISTRY.md` and `docs/guides/`.
- **Public documentation site** — bootstrapped a Docusaurus 3 (TypeScript)
  site under `website/`, deployed to GitHub Pages at
  <https://jcsvwinston.github.io/nucleus/>. The site adopts the Nucleus
  identity ahead of the code-level rename tracked in
  [`ADR-003`](docs/adrs/ADR-003-project-identity-nucleus.md):
  - `website/docusaurus.config.ts` configured with
    `url=https://jcsvwinston.github.io`, `baseUrl=/nucleus/`,
    `organizationName=jcsvwinston`, `projectName=nucleus`.
  - Landing page (hero, feature grid, code showcase, subsystem grid,
    final CTA) plus a structured docs tree: Introduction, Getting
    started, Concepts (Application, Configuration, Routing, Models &
    DB), Features (Admin, Auth, Observability, Storage & Tasks),
    Architecture (Principles, Compatibility), CLI overview.
  - Custom palette + typography (Inter / JetBrains Mono); custom logo.
  - `.github/workflows/docs.yml` — build-only on PRs, build + deploy to
    GitHub Pages on push to `main` via `actions/deploy-pages@v4`,
    path-scoped to `website/**`. Non-blocking to the framework `CI
    Required Gate`.
  - The authoritative docs tree under `docs/` is unchanged; content
    will be promoted into the site incrementally.
  - Note: requires `Settings → Pages → Source: GitHub Actions` to be
    enabled in the repository (one-time owner action).
- **Track B: Compatibility Harness** — Complete implementation of cross-version validation:
  - Fixture applications: `examples/mvc_api` (minimal API, admin-heavy), `examples/plugins` (plugin-heavy)
  - CI harness: `scripts/ci/run_compatibility_harness.sh` with profile-based testing
  - Golden tests: `contracts/freeze_test.go` enforces no removals from CLI, config, and API baselines
  - Compatibility report: `scripts/release/generate_compatibility_report.sh` generates release artifacts
- **Track C: Critical Dependency Firewall** — Complete implementation of dependency isolation:
  - Adapter boundaries: All critical dependencies wrapped behind framework interfaces
  - Type leak prevention: `contracts/firewall_test.go` with automated AST-based detection
  - Dependency impact report: `scripts/release/generate_dependency_impact_report.sh` with critical dependency tracking
  - Swap drills: SQL driver swap validated (SQLite ↔ PostgreSQL ↔ MySQL)
- **Track D: Enterprise Data Coverage** — Critical command coverage for MSSQL/Oracle:
  - migrate (up, down, status) - Added to exploratory tests
  - fixtures (loaddata, dumpdata) - Added to exploratory tests
  - inspectdb - Already tested in exploratory tests
  - sessions/cache (clearsessions) - Added to exploratory tests
  - Stability drill script operational: `scripts/ci/run_exploratory_stability.sh`
  - Stability report created: `docs/reports/mssql_oracle_stability_report.md`
  - Next step: Execute stability drills to validate promotion thresholds (MSSQL >= 80%, Oracle >= 80%)

- **Standalone scaffold** — `goframe new` now generates a self-contained project:
  - `go.mod` includes `require github.com/jcsvwinston/nucleus <version>`
  - release builds embed the exact version tag via goreleaser ldflags
  - dev builds use `latest` so `go mod tidy` resolves the newest published tag
  - projects compile without a `replace` directive or local GoFrame source
- **Build-tagged enterprise SQL drivers** — MSSQL and Oracle drivers are now opt-in:
  - `pkg/db/driver_mssql.go` (`//go:build mssql`) — register with `-tags mssql`
  - `pkg/db/driver_oracle.go` (`//go:build oracle`) — register with `-tags oracle`
  - SQLite, PostgreSQL, and MySQL remain included by default
- **Composable `app.New()` with Extension pattern** — modular initialization:
  - `Extension` interface in `pkg/app/extensions.go` (Name/Attach/Shutdown lifecycle)
  - `app.New(cfg, ...Option)` now accepts `WithExtensions()` and `WithoutDefaults()`
  - Default subsystems (admin, storage, mail, authz) extracted to `attachDefaultSubsystems()`
  - `app.New(cfg)` without options remains fully backward compatible
- **`--template api` scaffold tier** — lightweight core-only projects:
  - `goframe new myapp --template api` generates a minimal API using `app.WithoutDefaults()`
  - No admin panel, storage, mail, or authz subsystems initialized
  - Ideal for microservices and lightweight REST APIs
- **Unified storage layer** (`pkg/storage`) — provider-agnostic file storage with durable interface:
  - S3-compatible driver (AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces)
  - GCS native driver (Google Cloud Storage)
  - Azure Blob native driver
  - Local filesystem driver (development only)
  - `CredentialSource` with 4 injection methods: `value`, `env_var`, `file`, `secret_manager` (via `env:` prefix)
  - Tenant-aware key prefixing via `TenantStore` wrapper
  - Public path mapping with CDN support (`PublicMapper`)
  - Signed URLs for time-limited private object access
  - TTL-based cleanup of temporary objects (`_tmp/` prefix)
- **Tenant-aware admin CRUD** — automatic tenant filtering and tenant ID injection:
  - Models declare tenant field via `db:"tenant"` tag or `tenant_id` column convention
  - Admin middleware extracts tenant from request scope and applies filter
  - Tenant selector in admin header for multi-tenant deployments
- **RBAC in admin panel** via Casbin enforcer:
  - Policy management API (add/remove policies, assign/remove roles)
  - Permission checking with superuser bypass
  - Configurable via `admin_rbac_policy_file`
- **Audit logging** for all admin CRUD operations:
  - Bounded in-memory store (default 10,000 entries)
  - Filtering by user, model, action with pagination
  - Audit log viewer in admin UI
- **Data Studio import/export** (P3):
  - Export: CSV, JSON, SQL dump with tenant filtering
  - Import: CSV/JSON upload → validation → execute with conflict resolution (skip/update/error)
  - Fixtures: Django-compatible `dumpdata`/`loaddata` format
  - Toolbar buttons: Export selected | Export all | Import | Recent exports dropdown
- **Multi-node safe**: all file operations use shared S3 storage — zero node affinity
- **Admin UI enhancements**:
  - Health check dashboard (DB/Redis connectivity with latency)
  - Migration management UI (status + apply)
  - Deployment detection (standalone/Docker/K8s, cluster topology)
  - Cache management (Redis stats + flush)
  - File storage browser
  - i18n support (EN/ES) with locale selector
  - Export history dropdown with download links
- **Model tenant field detection**: `TenantFieldName()` on `ModelMeta` with `db:"tenant"` tag parsing
- **Admin storage integration**: `PanelConfig.Store` for export/import operations via shared storage
- **CLI ↔ doc parity guard** (`contracts/cli_doc_parity_test.go`): asserts every `nucleus <token>` reference in `website/docs/cli/overview.md` resolves to a primary command in `internal/cli/root.go` or to a Django-style alias. Closes the regression path for fabricated commands (audit `docs/audits/2026-05-12-enterprise-readiness.md`, discrepancies D1 + D2). Exposes `cli.ContractAliasCommandNames()` to mirror the existing `ContractPrimaryCommandNames()` accessor.

### Fixed

- `website/docs/cli/overview.md` no longer documents fabricated commands `nucleus i18n extract|compile`, `nucleus contenttype list`, or the `nucleus fixtures dumpdata|loaddata` namespace — replaced with the real `nucleus makemessages` / `nucleus compilemessages` / `nucleus remove_stale_contenttypes` / `nucleus dumpdata` / `nucleus loaddata` and `nucleus findstatic`. Audit `docs/audits/2026-05-12-enterprise-readiness.md` discrepancies D1, D2.
- `README.md` lifecycle-command count corrected from `34` to `37` (matches the registered `commandSpec` entries in `internal/cli/root.go`).
- **Rate-limit per-tenant** (`pkg/router/ratelimit.go`): `rateLimitKeyFromRequest` now prefixes the bucket key with `tenant:<id>|` when a tenant is resolved into the request scope, so two requests sharing a `user_id` but distinct `tenant_id`s no longer share a bucket. Plumbing crosses the `pkg/app` → `pkg/router` boundary via a new `observe.CtxWithTenantID` / `observe.TenantIDFromCtx` pair (the request-scope middleware in `pkg/app/requestscope.go` now mirrors the resolved tenant into `pkg/observe`, the same channel `UserIDFromCtx` already uses). `observe.WithContext` enriches loggers with a `tenant_id` field when present. Closes audit discrepancy D5; the README promise of "rate-limit per-tenant" is now load-bearing.
- **Core `/healthz` handler** (`pkg/app/healthz.go`): `App.New` now registers `GET /healthz` by default. The handler probes every entry in `a.DBs` via `db.DB.Health` (per-DB timeout 2s) and returns `200` with `{"status":"healthy",...}` when all probes pass, or `503` with `{"status":"unhealthy",...}` when any fails. Suitable for Kubernetes liveness/readiness probes — works under `app.WithoutDefaults()` too. Redis / mail / storage probes are tracked as follow-ups; `website/docs/features/observability.md` is now in sync with the implemented scope. Closes audit discrepancy D3; the README + observability doc promise of `/healthz` is now load-bearing.
- **Endpoints ↔ doc parity guard** (`contracts/endpoints_doc_parity_test.go`): mounts a minimal in-memory app via `app.New(cfg, app.WithoutDefaults())`, then verifies every endpoint documented in `website/docs/features/observability.md` and `website/docs/getting-started/quickstart.md` responds with the expected status. Currently covers `/healthz`; future entries append in lockstep with docs + impl.
- **`pkg/health` package** — new internal abstraction for dependency probes used by `/healthz`. Exposes a `Prober` interface, a `Run(ctx, probes, timeout)` concurrent aggregator, and three concrete constructors: `NewDBProbe`, `NewRedisProbe`, `NewStorageProbe`. Keeps `github.com/redis/go-redis/v9` wrapped — `pkg/app` no longer imports the redis client directly (firewall-friendly). `pkg/app/healthz.go` now derives probes from current `App` state on every request: one `db:<alias>` per entry in `a.DBs`, plus `redis` if `Config.RedisURL` is set, plus `storage` if a `Store` is attached. Per-probe budget remains 2 s; probes run concurrently so total wall time is bounded by the slowest probe. `website/docs/features/observability.md` documents the registration rules and the underlying calls.
- **Circuit-breaker primitive** (`pkg/circuit`) — new standalone package exposing `Config`, `New`, `(*Breaker).Do(ctx, fn)`, `(*Breaker).State()`, the `State` enum (`StateClosed` / `StateOpen` / `StateHalfOpen`), and `ErrOpen`. Standard three-state state machine with configurable failure threshold, cooldown, and half-open probe budget. Race-tested under concurrent probe contention. Intentionally minimal — no event bus, no metrics, no per-call timeout; compose those with `pkg/observe` and the `/metrics` MeterProvider. Use it to wrap calls to mail / object storage / plugin bridges / third-party APIs so a single dependency outage cannot cascade. Documented in `website/docs/features/observability.md`.
- **Multi-driver `AutoMigrate`** (`pkg/model`, `pkg/app/app.go`) — `App.AutoMigrate` now dispatches by `db.DB.System()` and supports SQLite, PostgreSQL, and MySQL. New scaffold builders: `model.BuildPostgresMigrationScaffold` (BIGSERIAL PK, BYTEA / TIMESTAMPTZ types, double-quoted identifiers, `DROP TABLE … CASCADE` on rollback) and `model.BuildMySQLMigrationScaffold` (BIGINT AUTO_INCREMENT PK, LONGBLOB / DATETIME(6) / TINYINT(1) types, backtick-quoted identifiers, MySQL-syntax `DROP INDEX … ON …`). MSSQL and Oracle still return `db.ErrAutoMigrate` — explicit SQL migrations + `nucleus migrate` is the path for those engines, consistent with ADR-001. New exported `(d *DB) System()` accessor; `quickstart.md` admonition updated to reflect the SQLite + Postgres + MySQL coverage and the dev-mode caveats.
- **Migration drift detection** (`pkg/db/migrate.go`, `internal/cli/migrate.go`) — new `Migrator.Drift() ([]DriftEntry, error)` method detects file-level drift: rows in `nucleus_schema_migrations` whose corresponding `.up.sql` file is absent from the migrations directory (typical cause: an operator deleted a migration after applying it). Exposed in the CLI as `nucleus migrate drift`; the command prints a tab-separated row per drifted ID and **exits non-zero** when any drift is reported so CI gates can detect it programmatically. Schema-level drift (actual `information_schema.columns` shape vs migration intent) is a separate, per-dialect check tracked as a follow-up. `website/docs/cli/overview.md` lists the new subcommand.
- **`/metrics` Prometheus endpoint** (`pkg/observe/otel.go`, `pkg/app/app.go`) — `TelemetryConfig` gains a `PrometheusEnabled` flag. When set, `SetupOpenTelemetry` attaches a Prometheus reader to the OTel MeterProvider (alongside the existing OTLP reader, when configured) and returns an additional `http.Handler` value. `App.New` wires the handler at the path configured by `Config.MetricsPath` (default `/metrics`). OTLP push and Prometheus pull coexist on the same MeterProvider — instrumentation code is unchanged. `application/openmetrics-text` content type, registry-scoped, deny-list-friendly. Closes the long-standing "no Prometheus exposition path" gap documented in `observability.md`; that doc is now updated and the endpoints-parity guard in `contracts/` covers `/metrics` end-to-end against a minimal in-memory app.
- **Mail probe in `/healthz`** (`pkg/health/mail.go`, `pkg/mail`) — new optional `mail.HealthChecker` interface (`Healthy(ctx) error`); SMTP implements it natively (TCP dial + HELO + QUIT, no auth, no message sent). `pkg/health.SupportsMailProbe` + `NewMailProbe` register a `mail` row in the `/healthz` response when (and only when) the configured `Sender` opts in. `noop`, `sendgrid` and external plugin senders intentionally do not implement `HealthChecker` today — their `/healthz` rows simply do not appear. Documented registration semantics in `observability.md`.
- **Casbin deny-override** (`pkg/authz/enforcer.go`) — default RBAC model now stamps an `eft` column on every policy and uses the deny-override effect formula `some(where (p.eft == allow)) && !some(where (p.eft == deny))`. Default-deny semantics are preserved (no matching policy → deny). New public method `Enforcer.Deny(sub, obj, act)` lets operators block a specific user even when a broader role's allow rule would otherwise grant access. `AddPolicy` auto-stamps `allow` so callers do not change shape; `RemovePolicy` lifts both allow and deny variants matching the tuple. CSV policy files now require a 4th column (`allow` or `deny`); programmatic callers are unchanged. **Wired into the default `App.New` path per [ADR-004](docs/adrs/ADR-004-casbin-default-deny-mount.md):** the enforcer is constructed unconditionally, a bootstrap allow-list is seeded for framework-owned routes (`/healthz`, `/metrics`, `/login`, `/.well-known/jwks.json`, `/static/*`, configured `admin_prefix`), and the default-deny middleware is mounted on the router. `app.WithOpenAuthz()` is the code-level opt-out. Documented in `website/docs/features/auth.md` and `docs/guides/AUTH_GUIDE.md`.
- **JWT key rotation + JWKS endpoint** (`pkg/auth/jwt.go`) — `JWTManager` extends from single-secret HS256 to a multi-key keyset that supports rotation without downtime, plus `RS256` for asymmetric signing. New exported surface: `SigningAlgorithm` (HS256, RS256), `SigningKey`, `NewJWTManagerFromKeys`, `RotateKey`, `RemoveKey`, `CurrentKID`, `JWKSHandler`, `JWKS`, plus the wire types `JWKSet` / `JWK`. Tokens issued in multi-key mode carry a `kid` header; `Validate` looks the key up by kid and rejects unknown ones. The legacy `NewJWTManager(secret, expiry, issuer)` path is unchanged and still produces kid-less tokens that validate against the single secret. `JWKSHandler` serves the RFC 7517 JSON Web Key Set at any path the user mounts it on (typically `/.well-known/jwks.json`); HMAC keys are intentionally excluded so the public endpoint cannot leak shared secrets. `website/docs/features/auth.md` documents the single-secret and rotation modes, the operator rotation flow (`RotateKey` → grace window → `RemoveKey`) and the JWKS shape with a worked example. Closes the highest-leverage P0 item from the post-iteration backlog.
- `website/docs/getting-started/quickstart.md` now carries an explicit `:::warning` admonition that `.AutoMigrate()` is SQLite-only — citing the `AutoMigrate intentionally unsupported` comment in `pkg/db/migrate.go` and the matching `ErrAutoMigrate` fallback in `pkg/app/app.go`. Points users at `nucleus migrate` as the multi-driver path. Closes audit discrepancy D8.

### Docs

- Seeded `docs/deprecations/` and `docs/migration_assistants/` with their first concrete entries: `DEP-2026-001-legacy-plugin-prefixes.md` (retroactive record of the `goframe-plugin-*` / `goframe-mail-*` removal shipped in `v0.6.0`) and its paired `MA-2026-001-legacy-plugin-prefix-to-nucleus-plugin.md`. Exercises the formats defined in `docs/governance/DEPRECATION_TEMPLATE.md` and `docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md` against a real surface.
- `docs/deprecations/DEP-2026-002-builtin-sendgrid-provider.md` + `docs/migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md` — paired notice for the SendGrid removal documented under `Removed` below.

### Removed

- **BREAKING: built-in `sendgrid` mail provider.** `pkg/mail/sendgrid.go`, the `init()` registration, `mail.Config.SendGridAPIKey` + `SendGridEndpoint` fields, `app.Config.SendGridAPIKey` + `SendGridEndpoint` fields (with their `sendgrid_api_key` / `sendgrid_endpoint` `koanf` keys), `admin.PanelConfig.SendGridEndpoint`, and the per-vendor case in `pkg/admin/runtime_email.go` all removed. The framework now ships only protocol-universal senders (`noop`, `smtp`); vendor-specific providers (SendGrid, Mailgun, AWS SES, Postmark, Resend, …) install as external `nucleus-plugin-<provider>` binaries on `PATH` and are discovered via the existing `pkg/mail/external.go` path. Reference skeleton at `examples/plugins/mail/`. Migration: see [MA-2026-002](docs/migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md) — drop the four field/key occurrences from source and config, install `nucleus-plugin-sendgrid` on `PATH`, move the API key from tracked YAML to the plugin's documented env-var path. Contract baseline `contracts/baseline/config_key_patterns.txt` rebaselined with the two SendGrid keys dropped.

### Changed

- **BREAKING (default behaviour of `App.New`):** per [ADR-004](docs/adrs/ADR-004-casbin-default-deny-mount.md), the Casbin enforcer + default-deny middleware are now mounted on the router by default. Existing applications that called `app.New(cfg)` without `admin_rbac_policy_file` set will return `403 Forbidden` on every business endpoint after upgrading. Two escape hatches: load policies via `admin_rbac_policy_file` (production path), or pass `app.WithOpenAuthz()` to skip the mount entirely (development path; emits a `WARN` and surfaces in PR review). Framework-owned routes (`/healthz`, `/metrics`, `/login`, `/.well-known/jwks.json`, `/static/*`, the configured `admin_prefix`) are pre-allowed via `BootstrapAllowList()` and continue to respond. Existing tests in `pkg/app` updated to pass `WithOpenAuthz()` where the test subject is unrelated to authz; `examples/mvc_api` and the scaffold template in `internal/cli/new.go` seed an explicit anonymous allow for their public API surface as the prescribed pattern for production apps that want unauthenticated routes.
- Documentation reorganized with new `STORAGE_GUIDE.md`, updated `INDEX.md`, `ADMIN_PANEL.md`, and `ENTERPRISE_LONG_TERM_ROADMAP.md`
- Removed outdated historical reports (`ROADMAP_SUPERAR_DJANGO.md`, `GAP_IMPLEMENTATION_STRATEGY.md`, and 5 stale report snapshots)
- `SPEC.md` updated with storage layer, admin import/export, and RBAC documentation
- Admin panel now requires storage configuration for export/import functionality

### Security

- Credentials never stored as plain text in config — resolved at startup via `CredentialSource`
- All exported files stored with `Private` visibility, accessed via time-limited SignedURLs
- Import validation prevents injection of read-only/excluded fields

- Unified request context helpers in `pkg/router`:
  - `ContextHandler` adapter for one-entrypoint handler style
  - optional dependency injection via `router.WithSession(...)` and `router.WithTemplates(...)`
- REST resource route helper in `pkg/router`:
  - `Router.Resource("/users", router.ResourceHandlers{...})` for conventional CRUD route registration
  - automatic mapping for list/create/retrieve/update/delete endpoints
- `pkg/plugins` inventory and capability probe package to discover:
  - built-in mail providers as `mail.send` capability providers
  - generic external plugins (`goframe-plugin-<provider>`)
  - legacy external mail plugins (`goframe-mail-<driver>`)
- New plugin diagnostics command group:
  - `goframe plugin list`
  - `goframe plugin doctor`
  - `goframe plugin test --provider <p> --capability <c>`
- Typed Plugin SDK v1 envelope and baseline capability schemas in `pkg/plugins`:
  - request/response envelopes (`version: v1`)
  - capability payload/output structs for `mail.send`, `queue.publish`, and `webhook.deliver`
  - external plugin executor with exit-code/retriable mapping
- Official Plugin SDK v1 example providers:
  - `examples/plugins/mail` (`goframe-plugin-examplemail`, `mail.send`)
  - `examples/plugins/queue` (`goframe-plugin-examplequeue`, `queue.publish`)
  - usage guide in `docs/PLUGIN_EXAMPLES.md`
- Mail runtime bridge now supports capability plugins:
  - preferred external provider binary `goframe-plugin-<driver>` when `mail.send` is advertised
  - legacy fallback `goframe-mail-<driver>`
- Plugin runtime tests now cover success, provider error mapping, and timeout behavior for external execution.
- Session runtime now supports first-class backend selection via config:
  - `session_store: memory|sql|redis`
  - SQL-backed store with automatic session table bootstrap (`session_table`, default `goframe_sessions`)
  - Redis-backed store (`session_redis_url` or `redis_url` fallback)
  - configurable session cookie settings (`session_cookie_*`) and idle timeout
- Session runtime metadata middleware now records serving-node identity in session state:
  - first/last seen timestamps
  - runtime pod, host, and instance identifiers for shared-session environments
- Admin session observability endpoint and UI:
  - `GET /admin/api/sessions`
  - `/admin` sessions dashboard with active-session table, pod/host attribution, and telemetry windows (real-time, last hour, today)
- Admin live runtime inspector foundation:
  - `GET /admin/api/live/snapshot` for in-memory request/session runtime snapshots
  - `GET /admin/api/live/ws` for non-blocking WebSocket event stream
  - bounded request ring buffer + in-memory session tracker + non-blocking subscriber drop policy
  - new `/admin#/live` view wired to snapshot + live stream
  - live SQL sniffer from framework CRUD operations (`operation`, `query`, redacted `args`, `duration_ms`, `trace_id`) emitted to snapshot and WebSocket stream
- Admin system pulse snapshot foundation:
  - `GET /admin/api/system/snapshot` for Go runtime + DB pool + startup environment telemetry
  - startup environment viewer with mandatory masking for `KEY|SECRET|PASSWORD|TOKEN`
  - new `/admin#/system` view for goroutine states, memory/GC metrics, and DB pool stats
  - integrated worker/job pool monitor via Asynq runtime inspector (queues, servers, active workers)
  - feature flags runtime control endpoints:
    - `GET /admin/api/system/flags`
    - `POST /admin/api/system/flags`
    - `PUT /admin/api/system/flags/{name}`
    - `DELETE /admin/api/system/flags/{name}`
  - queue runtime operation endpoint with safety guardrails:
    - `POST /admin/api/system/jobs/queues/{name}/actions/{action}` where action is `pause|unpause|retry`
    - explicit acknowledgment payload required; production additionally requires `force=true`
- Advanced in-process rate limiting dimensions:
  - `rate_limit_burst` for controlled token-bucket burst capacity
  - `rate_limit_by_route` for route-scoped budgets
  - `rate_limit_by_role` for role-scoped budgets (JWT claims)
- Added negative-test coverage for security defaults and edge cases:
  - CSRF token mismatch rejection
  - CORS origin allow/deny behavior
  - session config fallback/invalid-store handling
- SQL matrix integration tests for required DB profiles (`PostgreSQL`/`MySQL`):
  - `pkg/db` runtime connect + ping smoke (`GOFRAME_SQL_MATRIX_URL`)
  - `internal/cli` critical command smoke for migrate/health/fixtures/shell (`GOFRAME_SQL_MATRIX_URL`)
- SQL matrix compatibility tests for exploratory DB profiles (`MS SQL Server`/`Oracle`):
  - explicit unsupported-scheme behavior coverage in `pkg/db`
  - exploratory URL smoke (`GOFRAME_SQL_EXPLORATORY_URL`)
- CI SQL matrix profile reference with local reproduction commands in `docs/CI_MATRIX.md`.
- Compatibility fixture harness script for release gating:
  - `scripts/ci/run_compatibility_harness.sh`
  - fixture profiles: `minimal-api`, `admin-heavy`, `plugin-heavy`
  - markdown report output with threshold enforcement
- New MVC/API fixture smoke test:
  - `TestExampleMVCAPI_Minimal_Smoke` in `examples/mvc_api`
- Expanded exploratory SQL matrix CLI integration coverage for `MSSQL`/`Oracle`:
  - `createcachetable` idempotency validation
  - `sqlflush` and `flush --dry-run` output validation
  - `sqlsequencereset` output validation across exploratory engines
- `sqlsequencereset` for Oracle now emits concrete reset SQL for common sequence naming strategies:
  - `<table>_SEQ`
  - `<table>_ID_SEQ`
  - next sequence value derived from `MAX(id)+1` when `id` column exists
- Automated release report generators:
  - `scripts/release/generate_compatibility_report.sh`
  - `scripts/release/generate_dependency_impact_report.sh`
- Contract-governance documentation set:
  - `docs/API_CONTRACT_INVENTORY.md`
  - `docs/CLI_CONTRACT_MATRIX.md`
  - `docs/CONFIG_KEY_REGISTRY.md`
- Request-scope routing foundation for MultiSite/MultiTenant in `pkg/app`:
  - host/site/tenant resolution middleware
  - `RequestScope` context helpers
  - `App.Database(alias)` and `App.DatabaseForRequest(r)` for DB alias routing
- CLI output contract foundation:
  - global output flags: `--output plain|pretty|json`, `--color auto|always|never`, `--symbols|--no-symbols`, `--json` shorthand
  - pretty/status rendering support for `health`, `routes`, `mailproviders`, and `plugin` command family
  - tests for global output mode/color behavior
- Security-by-default tenant isolation guardrails:
  - startup validation rejects tenant configurations that resolve multiple tenants to one DB alias
  - tenant routing rejects shared site DB alias usage when multitenancy is enabled
- Deprecation and migration-assistant governance docs:
  - `docs/DEPRECATION_TEMPLATE.md`
  - `docs/MIGRATION_ASSISTANT_CONVENTIONS.md`
  - reusable templates:
    - `docs/templates/deprecation_notice.md`
    - `docs/templates/migration_assistant.md`
- DB observability metrics in `pkg/db`:
  - query total/error counters and query duration histogram
  - pool utilization/wait metrics (`open`, `idle`, `in_use`, `wait_count`, `wait_duration_ms`)
- Job observability and tracing in `pkg/tasks`:
  - enqueue and processing lifecycle metrics (`started`, `succeeded`, `retried`, `failed`, duration)
  - producer/consumer spans for enqueue and worker processing
  - request-context correlation helpers via `Manager.EnqueueJSONCtx(...)`
- Observability dashboard and alert recommendations in `docs/OBSERVABILITY_BASELINE.md`.

### Changed

- `goframe generate` now follows the same canonical scaffold layout as `new`/`startapp`:
  - models under `internal/models`
  - controller scaffolds and tests under `internal/controllers`
- `goframe new` scaffold now writes `go.mod` with `go 1.25` to align with framework minimum support.
- `goframe sendtestemail` and deploy health messaging now reference generic plugin naming (`goframe-plugin-<driver>`) with legacy fallback details.
- Documentation consolidated with a canonical docs entrypoint (`docs/INDEX.md`), active-vs-historical separation, and refreshed cross-links.
- Fixed stale local absolute link in `docs/DETAILED_TUTORIAL.md` to a portable relative reference.
- Standardized documentation headers across `docs/` with consistent `Reference date` and `Status` metadata.
- Normalized documentation wording to avoid ambiguous temporal phrasing and align plugin-runtime terminology.
- README and plugin/mail docs updated with capability-based plugin command references.
- `docs/V0.6.0_ROADMAP.md` checklist updated for completed Plugin SDK baseline items.
- `app.New` now wires session middleware by default and exposes `App.Session`.
- `goframe check --deploy` now validates session/cookie production posture (store mode, redis/sql requirements, secure cookie and SameSite combinations).
- Documentation updated with cluster-safe session guidance (`sql`/`redis` for multi-replica environments).
- Roadmap updated with:
  - completed admin session observability item for `v0.6.0`
  - MongoDB adapter exploration listed as non-priority post-`v0.6.0` backlog
  - MS SQL Server and Oracle explicitly tracked as exploratory CI lanes with promotion criteria to first-class support
- Router middleware now supports token-bucket rate limiting with optional route and role dimensions while preserving previous config compatibility.
- CLI test suite now verifies production guardrails in non-interactive runs across destructive commands:
  - `flush`
  - `loaddata --truncate`
  - `migrate down`
  - `migrate steps -N`
  - `migrate refresh`
- CI now includes dedicated SQL matrix jobs:
  - required lanes: PostgreSQL + MySQL
  - exploratory non-blocking lanes: MS SQL Server + Oracle compatibility smoke
- CI now emits a stable required check context `CI Required Gate` that aggregates required lanes for branch protection.
- Added branch-protection automation script `scripts/ci/configure_branch_protection.sh` and merge-policy guidance in `docs/CI_MATRIX.md`.
- HTTP telemetry middleware now stores `trace_id` in `observe` context for downstream correlation.
- GitHub workflows now use current action majors (`checkout/setup-go/setup-node` and GoReleaser action), with Node 24 in CI/release/rehearsal jobs.
- CI now includes a required `compatibility-harness` job and folds it into `CI Required Gate`.
- Rehearsal and release workflows now publish compatibility/dependency report artifacts.
- `scripts/release/rehearse_rc.sh` now generates release-gate reports into `dist/reports/`.
- Compatibility report generation now validates contract-governance document/template presence as a release gate check.
- Database configuration contract is now alias-only:
  - removed legacy keys `database_engine`, `database_url`, `database_max_open`, `database_max_idle`, `database_max_lifetime`
  - canonical runtime keys are `database_default` + `databases.<alias>.*`
- CLI/runtime DB wiring now resolves from the primary alias (`database_default`) rather than legacy single-URL keys.
- `pkg/model` metadata contract now supports:
  - explicit FK declarations (`fk` / `fk:<model|table.column|key=value,...>`)
  - simple and composite index declarations (`index`, `index:<name>`, `unique`, `unique:<name>`)
  - validation for multiple PK declarations, malformed FK specs, and mixed unique/non-unique index groups.
- New metadata-driven SQLite migration scaffold generator in `pkg/model`:
  - deterministic FK constraint names (`fk_<table>_<column>__<ref_table>_<ref_column>`)
  - index creation from model metadata and reverse index drops in `down` scaffolds
  - wired into `goframe generate resource` and `goframe startapp` migration generation.
- `goframe inspectdb` now enriches generated tags with schema metadata:
  - PK emitted as `pk`
  - FK emitted as `fk:<table>.<column>`
  - index metadata emitted as `index`/`unique` (single-column) or named variants for composites.
- New stable-contract freeze guardrails:
  - baseline files under `contracts/baseline/` for CLI primary command names, CLI JSON status envelope/data keys for automation-critical commands, config key patterns, and exported symbols from stable API packages
  - automated no-removal checks in `contracts/freeze_test.go`
  - CI/release integration via `scripts/ci/check_contract_freeze.sh` and required `contract-freeze` job.
- Admin API hardening:
  - action-level authorization checks now cover CSV export and session inventory endpoints
  - bulk delete responses now report per-id failure details (`requested`, `deleted`, `failed`, `errors[]`)
  - list endpoint now validates pagination/search/filter inputs explicitly (`page`, `page_size`, `search`, and filter fields/values)
- Critical maintenance CLI commands now honor a homogeneous output contract across global modes:
  - `createuser`, `changepassword`, `createcachetable`, `clearsessions`, `remove_stale_contenttypes`
  - default `plain` remains backward-compatible in message wording
  - `pretty` uses status-tag rendering and `json` emits structured command status payloads for automation
- `SPEC.md` is now synchronized with current architecture and dependency reality:
  - SQL-first runtime over `database/sql`
  - alias-only DB config contract and multisite/multitenant guardrails
  - current dependency set without stale Chi/Bun/GORM/Mongo references
- Week 6 release-readiness docs now include:
  - latest compatibility harness snapshot (`docs/reports/compatibility_harness_latest.md`)
  - release-readiness execution snapshot (`docs/reports/release_readiness_2026-04-07.md`)
  - explicit critical-dependency review note (`docs/reports/dependency_critical_review_2026-04-07.md`)

### Dependencies

- **`github.com/microsoft/go-mssqldb` v1.8.2 → v1.10.0** (direct, critical
  set). Reviewed and accepted: additive-only changes (new connection-string
  parameters, nullable civil types), `govulncheck`-clean, no removed public
  symbols. Used only as a blank `database/sql` driver import behind the
  `mssql` build tag (`pkg/db/driver_mssql.go`) — no third-party type reaches
  a stable `pkg/*` signature. The 2026-05-14 MSSQL stability drill ran 10/10
  with this version already in the tree.
- **`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`
  v1.35.0 → v1.43.0** (direct, critical set). Reviewed and accepted: eight
  minor releases, no breaking changes, no CVEs. Encapsulated entirely within
  `pkg/observe`; the firewall test confirms no leaked types. Callers gain
  W3C Trace Context Level 2 random-trace-ID flag propagation and the
  `WithHTTPClient` option.

## [0.6.0] - 2026-05-09

### Changed

- Renamed: GoFrame → Nucleus. New module path: `github.com/jcsvwinston/nucleus`. New CLI binary: `nucleus`. New canonical config filename: `nucleus.yml` (extension changed from `.yaml`). New public package entry: `pkg/nucleus` (renamed from `pkg/fluent`), `nucleus.New()`. See ADR-003 for rationale.

### Removed

- Legacy plugin discovery prefix `goframe-plugin-*` and legacy mail bridge `goframe-mail-*`. Plugins must use `nucleus-plugin-<provider>`.
- Removed `examples/showcase_demo` (depended on the external Quark module).
- Removed empty `examples/admin_generator`.
- Removed orphan `docs/quark/`.
- Untracked `coverage.out` (now ignored by `.gitignore`).

### Fixed

- README example now imports a real package (`pkg/nucleus`); previously it referenced a non-existent `pkg/goframe`.
- Aligned Go version requirement statements (minimum 1.25; CI continues to test against 1.26.3 as the latest).

### Docs

- Extracted ADR-001 (stdlib-First) and ADR-002 (Django-Inspired CLI) to standalone files under `docs/adrs/`.
- Added ADR-003 (Project Identity — Nucleus).
- Documented Outbox `KafkaBridge`/`WebhookBridge` as preview / not-for-production in SPEC.

## [0.5.5] - 2026-04-05

### Added

- `goframe shell` now supports `--sandbox` mode to allow only read-only SQL statements (`SELECT`/`EXPLAIN`/`SHOW`/`DESCRIBE`).
- Django-style CLI aliases:
  - `runserver` -> `serve`
  - `startproject` -> `new`
  - `makemigrations` -> `migrate create <name>`
  - `showmigrations` -> `migrate status`
  - `createsuperuser` -> `createuser`
  - `dbshell` -> `shell`
  - `check` -> `health`
- `goframe startapp` command to scaffold a new app module inside an existing project.
- `goframe test` command to run `go test` with framework-friendly flags and `--dry-run`.
- New SQL parity commands inspired by Django:
  - `goframe sqlmigrate` (print SQL for specific migration files)
  - `goframe sqlflush` (print generated flush SQL)
  - `goframe sqlsequencereset` (print sequence reset SQL)
  - `goframe flush` (execute flush SQL with production guardrails)
- Fixture parity commands inspired by Django:
  - `goframe dumpdata` (export table data as JSON fixtures)
  - `goframe loaddata` (import JSON fixtures, optional `--truncate` with guardrails)
- `goframe inspectdb` command to introspect SQL schema and generate Go model structs.
- `goframe diffsettings` command to compare effective configuration against framework defaults.
- `goframe health --deploy` / `goframe check --deploy` to run deploy hardening checks.
- `goframe changepassword` command to rotate admin-user passwords (Django-style parity for auth contrib).
- `goframe testserver` command to run fixture-loading (`loaddata`) followed by server startup, with `--dry-run` support.
- `goframe createcachetable` command to provision database-backed cache table schema.
- `goframe clearsessions` command to purge expired sessions (or all sessions via `--all`) from SQL-backed session tables.
- `goframe makemessages` command to extract translatable strings into locale `.po` catalogs.
- `goframe compilemessages` command to compile locale `.po` catalogs into JSON bundles.
- `goframe collectstatic` command to collect static assets into `static_root`, with `--dry-run` and `--clear`.
- `goframe findstatic` command to resolve static assets across discovered source directories, including glob queries.
- `goframe remove_stale_contenttypes` command to purge orphan content-type rows based on current SQL tables, with `--dry-run` and production guardrails.
- `goframe ogrinspect` command to inspect geospatial SQL tables (`geometry`/`geography`) and generate Go model structs.
- `goframe mailproviders` command to list registered mail drivers and external `goframe-mail-<driver>` plugins discovered on `PATH`.
- `goframe optimizemigration` command to normalize and deduplicate SQL statements in migration files.
- `goframe squashmigrations` command to squash a migration range into one `.up.sql`/`.down.sql` pair, with optional source archiving.
- `goframe sendtestemail` command now validates and sends through configurable `mail_driver` (`smtp`, `sendgrid`, or external plugin `goframe-mail-<driver>`), with `--dry-run` mode.
- New `pkg/mail` provider architecture with:
  - provider registry via `mail.RegisterProvider(...)` for in-process extensions
  - built-in drivers `noop`, `smtp`, and `sendgrid`
  - external plugin bridge via executables named `goframe-mail-<driver>` on `PATH`
- `pkg/tasks` baseline with Asynq support for background jobs (enqueue + worker runtime).
- OpenTelemetry bootstrap (`pkg/observe/otel.go`) with OTLP traces/metrics initialization and graceful shutdown wiring from `app.New`.
- HTTP telemetry middleware with spans and request metrics in `pkg/router`.
- Configurable rate limiting middleware (fixed-window) based on user-id (when available) or client IP.
- `goframe new` scaffold now generates `cmd/worker/main.go` and `internal/tasks/article_events.go`, plus Redis/OTel/rate-limit config keys in `goframe.yaml`.
- Enterprise roadmap and alignment status document (`docs/ENTERPRISE_ROADMAP.md`).
- CLI parity matrix document against Django 6.0 (`docs/CLI_DJANGO_PARITY.md`).

### Changed

- `goframe check --deploy` now includes mail readiness checks (`deploy.mail_*`) based on `mail_driver` and provider-required settings.
- `goframe sendtestemail` now accepts `--driver` to override `mail_driver` for one-off provider checks.
- CLI tests now cover `shell --sandbox` for both allowed (`SELECT`) and blocked write statements.
- JWT middleware now enriches request context with `observe` user-id for cross-cutting middleware (logging/rate-limit correlation).
- README, project layout, and developer manual updated to include worker/background jobs, OTel, and rate-limiting usage.
- Documentation filenames standardized to English (`docs/DEVELOPER_MANUAL.md`, `docs/DETAILED_TUTORIAL.md`) and references updated.
- README/manual/CLI best practices updated with Django-style aliases and parity references.
- CLI parity matrix updated to mark `startapp` and `test` alignment progress.
- CLI parity matrix updated to mark SQL parity command alignment progress.
- CLI parity matrix updated to mark fixture command alignment progress.
- CLI parity matrix updated to mark `inspectdb` alignment progress.
- CLI parity matrix updated to mark `diffsettings` and deploy check alignment progress.
- CLI parity matrix updated to mark `changepassword` and `testserver` alignment progress.
- CLI parity matrix updated to mark `createcachetable` and `clearsessions` alignment progress.
- CLI parity matrix updated to mark `makemessages` and `compilemessages` alignment progress.
- CLI parity matrix updated to mark `optimizemigration` and `squashmigrations` alignment progress.
- CLI parity matrix updated to mark `sendtestemail` alignment progress.

## [0.5.4] - 2026-04-01

### Fixed

- `goframe new` now supports `--template mvc`, aligned with the expected scaffolding workflow.
- `goframe new` now returns a clear error when an unsupported template is requested.
- CLI tests now cover supported and unsupported `--template` values.

### Changed

- README and developer manual examples now include `--template mvc` in `goframe new`.
- Root `.gitignore` now ignores `dist/` release rehearsal artifacts.

## [0.5.3] - 2026-03-31

### Fixed

- Public module path alignment for external consumers:
  - `go.mod` now declares `github.com/jcsvwinston/nucleus`
  - all internal imports updated to the public module path
  - GoReleaser ldflags updated to inject version with the new module path
- CLI scaffold/runtime references updated to the public module path so generated apps can resolve dependencies from `@latest`.

### Changed

- Developer docs and examples aligned with the new public module import path.

## [0.5.2] - 2026-03-31

### Added

- Complete end-user developer manual (`docs/DEVELOPER_MANUAL.md`):
  - installation paths
  - MVC/API/Admin workflow
  - full CLI reference
  - migration/seed operations
  - deployment and troubleshooting guidance

### Changed

- README development guides now include the complete developer manual.

## [0.5.1] - 2026-03-31

### Added

- Cross-OS release asset smoke workflow (`.github/workflows/release_asset_smoke.yml`).

### Changed

- Release asset smoke checks fixed to map tag (`vX.Y.Z`) to artifact naming (`X.Y.Z`).
- Release workflow made idempotent when assets already exist for a tag.
- CI/release/rehearsal workflows force JavaScript actions to run on Node 24.

## [0.5.0] - 2026-03-31

### Added

- Cross-OS release asset smoke workflow (`.github/workflows/release_asset_smoke.yml`).

### Changed

- Promoted `v0.5.0-rc1` to stable after successful artifact execution checks on Linux, macOS, and Windows.

## [0.5.0-rc1] - 2026-03-31

### Added

- Phase 5 release-candidate baseline:
  - CI workflow (`.github/workflows/ci.yml`)
  - tag-based release workflow (`.github/workflows/release.yml`)
  - release rehearsal workflow (`.github/workflows/rehearsal.yml`)
  - GoReleaser config for multi-platform artifacts (`.goreleaser.yaml`)
  - rehearsal script (`scripts/release/rehearse_rc.sh`)
  - versioning strategy docs (`docs/VERSIONING.md`)
  - release checklist (`docs/RELEASE_CHECKLIST.md`)
  - Go version support (minimum 1.25+)

### Changed

- Project status docs aligned with current roadmap and phase closures.
- `goframe version` now prints build-injected release versions instead of a fixed value.

## [0.4.0] - 2026-03-31

### Added

- Bun-first SQL layer and consolidated migration/seed CLI flow.
- Rich admin SPA with:
  - command palette
  - filters and sorting
  - bulk selected export
  - tabs/detail panels
  - accessibility and recoverable-error hardening
- Runnable example app (`examples/mvc_api`) combining MVC + API + Admin.
- CLI project bootstrap via `goframe new`.
- Smoke E2E test for the official example.

### Fixed

- Admin SPA serving reliability when mounted under `/admin` prefix.

---

[Unreleased]: https://github.com/jcsvwinston/nucleus/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/jcsvwinston/nucleus/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/jcsvwinston/nucleus/compare/v0.5.5...v0.6.0
[0.5.5]: https://github.com/jcsvwinston/nucleus/compare/v0.5.4...v0.5.5
[0.5.4]: https://github.com/jcsvwinston/nucleus/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/jcsvwinston/nucleus/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/jcsvwinston/nucleus/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/jcsvwinston/nucleus/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/jcsvwinston/nucleus/compare/v0.5.0-rc1...v0.5.0
[0.5.0-rc1]: https://github.com/jcsvwinston/nucleus/compare/v0.4.0...v0.5.0-rc1
[0.4.0]: https://github.com/jcsvwinston/nucleus/releases/tag/v0.4.0
