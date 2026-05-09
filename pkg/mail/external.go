package mail

import (
	"context"
	"fmt"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

type externalSender struct {
	driver     string
	binary     string
	timeout    time.Duration
	pluginHost plugins.Host
}

func newExternalSender(driver, binary string, timeout time.Duration, host plugins.Host) Sender {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if host == nil {
		host = plugins.LocalHost{}
	}
	return &externalSender{
		driver:     driver,
		binary:     binary,
		timeout:    timeout,
		pluginHost: host,
	}
}

func (s *externalSender) Send(ctx context.Context, msg Message) error {
	if err := validateMessage(msg); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}

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

	response, err := s.pluginHost.ExecuteRequest(ctx, s.binary, request, s.timeout)
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
