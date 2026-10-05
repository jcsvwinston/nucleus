// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mail

import (
	"context"
	"log/slog"
	"strings"
)

// LogDriver is the mail_driver that writes every message to the
// application log instead of delivering it.
//
// It exists for development: the confirmation and reset links the account
// flows send reach the person reading the terminal, the way Django's
// console backend and Laravel's log mailer do, with nothing to install.
// The framework refuses it outside `env: development` (the configuration
// check and the mail subsystem both say so at boot), because a message in
// a log is a message in every system the log is shipped to — and a
// password-reset link there is an account takeover.
const LogDriver = "log"

// NoopDriver is the default mail_driver: it accepts every message and
// delivers none.
const NoopDriver = "noop"

type logSender struct{ logger *slog.Logger }

func newLogSender(cfg Config) (Sender, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return logSender{logger: logger}, nil
}

// Send writes the message to the log at WARN — it was not delivered, and
// the line is there to be read — and never fails.
func (s logSender) Send(ctx context.Context, m Message) error {
	attrs := []any{
		"driver", LogDriver,
		"from", m.From,
		"to", strings.Join(m.To, ", "),
		"subject", m.Subject,
		"body", m.Body,
	}
	if len(m.Attachments) > 0 {
		attrs = append(attrs, "attachments", len(m.Attachments))
	}
	s.logger.WarnContext(ctx, "mail not delivered: mail_driver is log (development only)", attrs...)
	return nil
}

// Discards reports whether a sender delivers nothing at all: the noop
// driver, the default. A module whose flows depend on mail reaching a
// person — a confirmation link, a password reset — asks this at boot and
// refuses to start, rather than answering every request as if the mail had
// gone out.
func Discards(s Sender) bool {
	if s == nil {
		return true
	}
	_, noop := s.(noopSender)
	return noop
}
