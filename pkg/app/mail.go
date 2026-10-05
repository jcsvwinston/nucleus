// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

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
// On an application built with the defaults it changes nothing: the mail
// sender is one of them.
func WithMail() Option {
	return func(o *appOptions) { o.withMail = true }
}
