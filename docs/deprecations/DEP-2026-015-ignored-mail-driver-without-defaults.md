# Deprecation Notice: a mail driver an application built WithoutDefaults() ignores → refused at v2.0.0

- ID: `DEP-2026-015`
- Status: `active`
- Announced in: `Unreleased` (NU-114, 2026-10-06; the release that carries this change)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is
  a **behaviour change of a configuration combination**, not a symbol
  removal: no `// Deprecated:` marker carries it.
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

An application built `WithoutDefaults()` builds no mail sender. Its
configuration could still select one — `mail_driver: smtp` with the
`smtp_*` keys, a plugin driver, `mail_driver: log` in development, or a
`NUCLEUS_MAIL_DRIVER` variable — and the driver was ignored without a word:
the application booted exactly as it would with no driver at all, and the
first sign of it was a nil `Runtime.Mailer()`, or a module that checked for
one and skipped the message. It is the silence DEP-2026-013 removed for
storage (NU-99), left in place for mail (NU-114).

`WithMail()` — the option that builds the declared sender on such an
application — already exists, and `nucleus add accounts` writes it. This
release makes the other side of the combination visible: an application
that declares a mail driver and does not pass the option keeps starting
with the driver ignored, as before, and now says so in one ERROR line at
boot.

What changes and when:

- **This release:** the ignored driver is reported, not refused. The boot
  log carries, once:

  ```
  level=ERROR msg="mail_driver IGNORED: the configuration declares a mail driver and this application is built WithoutDefaults() without WithMail(), so no mail sender is built" driver=smtp fix="add WithMail() beside WithoutDefaults() — …" deprecation="DEP-2026-015: from v2.0.0 this configuration refuses to start"
  ```

- **v2.0.0:** the same combination refuses to start, with the same message
  as the error.

`mail_driver: noop`, written or not, is not reported: it asks for no
delivery, and an application without a sender delivers none. The `smtp_*`
keys alone are not reported either: without a driver they select nothing,
on any application.

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()` whose
  configuration writes `mail_driver` with a value other than `noop`
  (`Config.MailDeclared`) and that does not pass `WithMail()`.
- Not affected: applications built with the defaults (the mail sender is
  one of them), applications built `WithoutDefaults()` that write no
  `mail_driver`, and applications that pass `WithMail()`. A driver the
  configuration did not write is not reported: `nucleustest` selects the
  memory driver on every application it starts, and that is not the
  application's configuration declaring a sender.
- No Go API is removed or changes shape. The addition is
  `app.Config.MailDeclared`, set by both loaders (`app.LoadConfig` and the
  builder's `FromConfigFile`) from the file and `NUCLEUS_MAIL_DRIVER`.

## Migration Path

- Replacement: either build the sender the configuration declares, or stop
  declaring it.
  - `nucleus.New().FromConfigFile("nucleus.yml").WithoutDefaults().WithMail()…`
    (or `app.New(cfg, app.WithoutDefaults(), app.WithMail())`);
  - or remove `mail_driver` (and the `smtp_*` keys) and
    `NUCLEUS_MAIL_DRIVER` from that application's configuration.
- Behavior differences: with `WithMail()` the application builds the
  declared sender at boot, exactly as the default path does — circuit
  breaker included — so `mail_driver: log` outside `env: development`, or
  `smtp` without `smtp_host`, now fails the boot with the mail subsystem's
  own message.
- Required app changes: one builder call, or a few deleted keys. An
  application that shares one configuration file between a full
  application and a `WithoutDefaults()` worker that must not send mail
  removes the driver from the worker's configuration, or overrides it per
  process.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-015-ignored-mail-driver-to-withmail.md`
- Detection rule: the boot log line `mail_driver IGNORED` carrying
  `deprecation="DEP-2026-015…"`.
- Suggested rewrite: manual — add `.WithMail()` after `.WithoutDefaults()`
  in the composition root, or delete the driver.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that the combination
  still starts, builds no sender and logs exactly one ERROR line naming
  `WithMail()` and this notice; that nothing is said without a written
  driver, for `noop`, or with `WithMail()`; and that `LoadConfig` records
  `MailDeclared`. `pkg/nucleus` pins the same through the builder. The
  addition is listed in `contracts/baseline/api_exported_symbols.txt`
  (additions only).
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, plus the log line); after it, adding
  `WithMail()` or removing the driver restores a booting application.

## Timeline

- Announcement date: `2026-10-06`
- Review checkpoint: the set that certifies the release carrying this notice.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

Unlike DEP-2026-013, `nucleus doctor` does not report this combination: it
has no mail check to carry it, and the boot line is the detection rule.

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
