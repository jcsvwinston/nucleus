# Migration Assistant: mail driver ignored by a WithoutDefaults() application → `WithMail()` or no driver

- ID: `MA-2026-015`
- Pairs with: `docs/deprecations/DEP-2026-015-ignored-mail-driver-without-defaults.md`
- Severity: `low` before v2.0.0 (an ERROR log line; the application behaves
  as before); `high` at v2.0.0 for applications that still carry the
  combination — they stop starting until it is resolved.
- Status: `current`

---

## Scope

Applications built `WithoutDefaults()` — the api starter's shape, or
`app.New(cfg, app.WithoutDefaults())` — whose configuration writes
`mail_driver` with a value other than `noop` (in a configuration file, or a
non-empty `NUCLEUS_MAIL_DRIVER`) and that do not pass `WithMail()`. Their
driver has never been built; from v2.0.0 the combination refuses to start
(DEP-2026-015).

Out of scope: applications built with the defaults, applications built
`WithoutDefaults()` that write no `mail_driver` or write `noop`, and
applications that already pass `WithMail()`.

## Detection

**Logs — one ERROR line at boot (this release onward):**

```
level=ERROR msg="mail_driver IGNORED: the configuration declares a mail driver and this application is built WithoutDefaults() without WithMail(), so no mail sender is built" … deprecation="DEP-2026-015: from v2.0.0 this configuration refuses to start"
```

**Source — a composition root that opts out of the defaults and not back
into mail:**

```bash
# From the consumer repo root; a hit with no WithMail in the same file is the case.
grep -n "WithoutDefaults()" main.go cmd/*/main.go 2>/dev/null
grep -n "WithMail()" main.go cmd/*/main.go 2>/dev/null
grep -n "mail_driver" nucleus.yml 2>/dev/null
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `WithoutDefaults()` + a declared `mail_driver`, no `WithMail()` | `WithoutDefaults().WithMail()` — the sender is built | manual (one call) |
| the same, where the process must not send mail | the driver removed from that process's configuration | manual |

Chosen by intent:

```go
// The application should send through the driver its configuration selects:
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithoutDefaults().
    WithMail(). // builds the declared sender; noop while none is declared
    Start()

// Without the builder:
a, err := app.New(cfg, app.WithoutDefaults(), app.WithMail())
```

```yaml
# The application should NOT send mail (for example a worker sharing a
# configuration file with the full application): remove the driver from the
# configuration this process reads.
mail_driver: smtp            # ← delete
smtp_host: smtp.example.com  # ← delete
smtp_port: 587               # ← delete
```

With `WithMail()` the boot builds the sender as the default path does, so a
configuration the mail subsystem refuses — `mail_driver: log` outside
`env: development`, `smtp` without `smtp_host` — now fails the boot with
its message until it is corrected.

## Rollback

- Before v2.0.0: removing `WithMail()` returns the application to the
  ignored driver plus the log line.
- After v2.0.0: there is no ignored state to return to; the choice is
  `WithMail()` or no driver.

## Validation

After the rewrite, boot the application and confirm:

1. no `mail_driver IGNORED` line in the boot log;
2. with `WithMail()`, a non-nil `Runtime.Mailer()` (`mail.Discards` false
   for any driver but `noop`).
