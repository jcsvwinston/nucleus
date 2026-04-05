package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/plugins"
)

type externalSenderMode string

const (
	externalSenderModeLegacy     externalSenderMode = "legacy_mail_plugin"
	externalSenderModeCapability externalSenderMode = "capability_plugin"
)

type externalSender struct {
	driver  string
	binary  string
	timeout time.Duration
	mode    externalSenderMode
}

func newExternalSender(driver, binary string, timeout time.Duration, mode externalSenderMode) Sender {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if mode == "" {
		mode = externalSenderModeLegacy
	}
	return &externalSender{
		driver:  driver,
		binary:  binary,
		timeout: timeout,
		mode:    mode,
	}
}

func (s *externalSender) Send(ctx context.Context, msg Message) error {
	if err := validateMessage(msg); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if s.mode == externalSenderModeCapability {
		return s.sendCapability(ctx, msg)
	}
	return s.sendLegacy(ctx, msg)
}

func (s *externalSender) sendCapability(ctx context.Context, msg Message) error {
	request, err := plugins.NewRequestEnvelope(
		s.driver,
		plugins.CapabilityMailSend,
		s.timeout,
		plugins.MailSendPayload{
			From:    msg.From,
			To:      msg.To,
			Subject: msg.Subject,
			Body:    msg.Body,
			Headers: msg.Headers,
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("build plugin request envelope: %w", err)
	}

	response, err := plugins.ExecuteRequest(ctx, s.binary, request, s.timeout)
	if err != nil {
		return fmt.Errorf("capability plugin %s failed: %w", s.binary, err)
	}

	output, err := plugins.DecodeMailSendOutput(response.Output)
	if err != nil {
		return fmt.Errorf("decode capability plugin output: %w", err)
	}
	if !output.Accepted {
		return fmt.Errorf("capability plugin %s did not accept mail.send request", s.binary)
	}
	return nil
}

func (s *externalSender) sendLegacy(ctx context.Context, msg Message) error {
	type pluginPayload struct {
		Driver  string            `json:"driver"`
		From    string            `json:"from"`
		To      []string          `json:"to"`
		Subject string            `json:"subject"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers,omitempty"`
	}
	raw, err := json.Marshal(pluginPayload{
		Driver:  s.driver,
		From:    msg.From,
		To:      msg.To,
		Subject: msg.Subject,
		Body:    msg.Body,
		Headers: msg.Headers,
	})
	if err != nil {
		return fmt.Errorf("encode external mail payload: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, s.binary)
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return fmt.Errorf("mail plugin %s failed: %w (%s)", s.binary, err, stderrText)
		}
		return fmt.Errorf("mail plugin %s failed: %w", s.binary, err)
	}
	return nil
}
