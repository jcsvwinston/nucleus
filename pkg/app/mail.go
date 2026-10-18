// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
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
// writes a mail_driver other than noop refuses to start: New returns an
// error naming this option (NU-114, DEP-2026-015).
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

// mailIgnoredError is the NU-114 refusal, NU-99's for mail: the error New
// returns when the configuration declares a mail driver the application
// built WithoutDefaults() without WithMail() would ignore. It names the
// driver, the option that builds it and the notice; the headline is the
// v1.x log line's, so the detection rule the migration assistant documents
// still finds it.
func mailIgnoredError(effective *Config) error {
	return fmt.Errorf("mail_driver IGNORED: the configuration declares a mail driver and this application is built "+
		"WithoutDefaults() without WithMail(), so no mail sender is built (driver=%s) — add WithMail() beside "+
		"WithoutDefaults() — nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults().WithMail(), or "+
		"app.New(cfg, app.WithoutDefaults(), app.WithMail()) — or remove mail_driver (%s: refused since v2.0.0)",
		strings.ToLower(strings.TrimSpace(effective.MailDriver)), depMailIgnored)
}

// depMailIgnored is the deprecation notice for an application built
// WithoutDefaults() whose configuration declares a mail driver it does not
// build: the combination was reported with an ERROR line on the v1.x line
// and refuses to start since v2.0.0 (docs/deprecations/DEP-2026-015-*.md).
const depMailIgnored = "DEP-2026-015"
