// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"log/slog"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/mail"
)

// WithMail builds the mail sender the configuration declares — mail_driver,
// the smtp_* keys, the circuit breaker — on an application built
// WithoutDefaults(), which builds none, so App.Mailer (and a module's
// Runtime.Mailer) is there to send through.
//
// It is what `nucleus add accounts` writes into main.go beside the account
// flows: the api starter is built WithoutDefaults(), and a flow that mails a
// confirmation link needs somewhere to send it. With mail_driver unset the
// sender is the noop driver, which delivers nothing; `mail_driver: log`
// writes each message to the application log in development.
//
// Without it, an application built WithoutDefaults() whose configuration
// writes a mail_driver other than noop still starts with the driver
// ignored, as it always did, but no longer without a word: the boot log
// carries one ERROR line naming this option (NU-114). From v2.0.0 that
// configuration refuses to start (DEP-2026-015).
//
// On an application built with the defaults it changes nothing: the mail
// sender is one of them.
func WithMail() Option {
	return func(o *appOptions) { o.withMail = true }
}

// mailDriverIgnored reports whether the configuration declares a mail
// driver that delivers somewhere — anything but noop — which an application
// built WithoutDefaults() without WithMail() never builds. Declared means
// written by the configuration (Config.MailDeclared), not filled in by
// someone else: nucleustest swaps the noop default for the memory driver on
// every application it starts.
func mailDriverIgnored(effective *Config) bool {
	if !effective.MailDeclared {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(effective.MailDriver)) {
	case "", mail.NoopDriver:
		return false
	}
	return true
}

// logMailIgnored is the NU-114 warning, NU-99's for mail: one structured
// ERROR line, once per application, saying the declared mail driver is
// IGNORED, which option builds it, and that the configuration stops booting
// at the major. ERROR for the reason storage's is: a sender that is never
// built is how a password-reset message ends up nowhere; not a refusal,
// because it booted yesterday.
func logMailIgnored(logger *slog.Logger, effective *Config) {
	logger.Error("mail_driver IGNORED: the configuration declares a mail driver and this application is built "+
		"WithoutDefaults() without WithMail(), so no mail sender is built",
		"driver", strings.ToLower(strings.TrimSpace(effective.MailDriver)),
		"fix", "add WithMail() beside WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithMail(), "+
			"or app.New(cfg, app.WithoutDefaults(), app.WithMail()) — or remove mail_driver",
		"deprecation", depMailIgnored+": from v2.0.0 this configuration refuses to start")
}

// depMailIgnored is the deprecation notice for an application built
// WithoutDefaults() whose configuration declares a mail driver it does not
// build: today the driver is ignored with an ERROR line at boot; from v2.0.0
// the application refuses to start (docs/deprecations/DEP-2026-015-*.md).
const depMailIgnored = "DEP-2026-015"
