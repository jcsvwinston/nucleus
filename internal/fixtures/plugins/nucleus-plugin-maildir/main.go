// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Command nucleus-plugin-maildir is the example external plugin of the
// Nucleus plugin SDK: a mail provider that delivers each message into a
// Maildir — one file per message, written to tmp/ and renamed into new/, the
// way a local mail system delivers — where a mail client, a test or a
// person can read it.
//
// It is built and run by its test through the real runtime (`mail_driver:
// maildir` finds it on PATH; `nucleus plugin test --execute` sends it a
// request envelope), so it is the starting point to copy: everything the
// contract asks of a plugin is the plugins.Serve call and one handler.
//
// Install and select it:
//
//	go build -o "$(go env GOPATH)/bin/nucleus-plugin-maildir" ./internal/fixtures/plugins/nucleus-plugin-maildir
//	# nucleus.yml
//	mail_driver: maildir
//	plugins:
//	  allowed:
//	    - provider: maildir
//	      capabilities: [mail.send]
//
// Configuration, read from the environment as a plugin reads its
// credentials: MAILDIR is the Maildir to deliver into, $HOME/Maildir when it
// is unset. The directory and its tmp/, new/ and cur/ are created when
// missing.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

func main() {
	plugins.Serve(plugins.Plugin{MailSend: deliver})
}

// deliver writes one message into the Maildir. A message the contract
// cannot carry (no sender, no recipient, a line break in a header) is a
// validation failure (exit 10); a Maildir that cannot be written is a
// transient one (exit 20), which the host may retry.
func deliver(_ context.Context, req plugins.RequestEnvelope, m plugins.MailSendPayload) (plugins.MailSendOutput, error) {
	if err := validate(m); err != nil {
		return plugins.MailSendOutput{}, err
	}
	dir, err := maildir()
	if err != nil {
		return plugins.MailSendOutput{}, err
	}
	for _, sub := range []string{"tmp", "new", "cur"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return plugins.MailSendOutput{}, plugins.Fail(plugins.ExitCodeTransient, "MAILDIR_UNWRITABLE", "create %s: %v", filepath.Join(dir, sub), err)
		}
	}

	name, host := uniqueName()
	tmp := filepath.Join(dir, "tmp", name)
	if err := writeMessage(tmp, render(req, m, name, host)); err != nil {
		_ = os.Remove(tmp)
		return plugins.MailSendOutput{}, plugins.Fail(plugins.ExitCodeTransient, "MAILDIR_UNWRITABLE", "write %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "new", name)); err != nil {
		_ = os.Remove(tmp)
		return plugins.MailSendOutput{}, plugins.Fail(plugins.ExitCodeTransient, "MAILDIR_UNWRITABLE", "deliver into %s: %v", filepath.Join(dir, "new"), err)
	}
	return plugins.MailSendOutput{ProviderRequestID: name}, nil
}

func validate(m plugins.MailSendPayload) error {
	if strings.TrimSpace(m.From) == "" {
		return plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "the message has no sender")
	}
	if len(m.To) == 0 {
		return plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "the message has no recipient")
	}
	fields := map[string]string{"from": m.From, "subject": m.Subject}
	for i, to := range m.To {
		if strings.TrimSpace(to) == "" {
			return plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "recipient %d is empty", i)
		}
		fields[fmt.Sprintf("to[%d]", i)] = to
	}
	for k, v := range m.Headers {
		fields["header "+k] = k + v
	}
	for field, value := range fields {
		if strings.ContainsAny(value, "\r\n") {
			return plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "%s contains a line break", field)
		}
	}
	return nil
}

func maildir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("MAILDIR")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", plugins.Fail(plugins.ExitCodeValidation, "MAILDIR_UNSET", "MAILDIR is not set and there is no home directory to default to: %v", err)
	}
	return filepath.Join(home, "Maildir"), nil
}

// uniqueName is a Maildir file name: seconds, then a part unique to this
// delivery, then the host — the convention every Maildir reader expects.
func uniqueName() (name, host string) {
	host, _ = os.Hostname()
	if host == "" {
		host = "localhost"
	}
	host = strings.NewReplacer("/", `\057`, ":", `\072`).Replace(host)
	var random [6]byte
	_, _ = rand.Read(random[:])
	now := time.Now()
	return fmt.Sprintf("%d.M%dP%dR%s.%s", now.Unix(), now.Nanosecond()/1000, os.Getpid(), hex.EncodeToString(random[:]), host), host
}

func render(req plugins.RequestEnvelope, m plugins.MailSendPayload, name, host string) []byte {
	var b strings.Builder
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\n", k, v) }
	header("From", m.From)
	header("To", strings.Join(m.To, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", "<"+name+"@"+host+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "8bit")
	if req.RequestID != "" {
		header("X-Nucleus-Request-Id", req.RequestID)
	}
	keys := make([]string, 0, len(m.Headers))
	for k := range m.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		header(k, m.Headers[k])
	}
	b.WriteString("\n")
	b.WriteString(m.Body)
	if !strings.HasSuffix(m.Body, "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// writeMessage writes and syncs the file before it is renamed into new/, so
// a reader never sees a partial message.
func writeMessage(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
