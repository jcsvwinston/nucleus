package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EnvelopeVersionV1 = "v1"

	CapabilityMailSend       = "mail.send"
	CapabilityQueuePublish   = "queue.publish"
	CapabilityWebhookDeliver = "webhook.deliver"
)

const (
	ExitCodeSuccess    = 0
	ExitCodeValidation = 10
	ExitCodeTransient  = 20
	ExitCodeRejected   = 30
	ExitCodeTimeout    = 40
	ExitCodeInternal   = 50
)

type RequestEnvelope struct {
	Version    string            `json:"version"`
	RequestID  string            `json:"request_id"`
	Timestamp  string            `json:"timestamp"`
	Capability string            `json:"capability"`
	Provider   string            `json:"provider"`
	TimeoutMS  int64             `json:"timeout_ms"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Payload    json.RawMessage   `json:"payload"`
}

type ResponseEnvelope struct {
	Version           string           `json:"version"`
	RequestID         string           `json:"request_id,omitempty"`
	OK                bool             `json:"ok"`
	ProviderRequestID string           `json:"provider_request_id,omitempty"`
	Retriable         bool             `json:"retriable,omitempty"`
	Output            json.RawMessage  `json:"output,omitempty"`
	Error             *ResponseError   `json:"error,omitempty"`
	Metrics           *ResponseMetrics `json:"metrics,omitempty"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResponseMetrics struct {
	DurationMS int64 `json:"duration_ms,omitempty"`
}

type MailSendPayload struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

type MailSendOutput struct {
	Accepted          bool   `json:"accepted"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
}

type QueuePublishPayload struct {
	Topic   string            `json:"topic"`
	Key     string            `json:"key,omitempty"`
	Body    json.RawMessage   `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

type QueuePublishOutput struct {
	Accepted  bool   `json:"accepted"`
	MessageID string `json:"message_id,omitempty"`
}

type WebhookDeliverPayload struct {
	URL       string            `json:"url"`
	Method    string            `json:"method,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
}

type WebhookDeliverOutput struct {
	Accepted   bool `json:"accepted"`
	StatusCode int  `json:"status_code,omitempty"`
}

func NewRequestEnvelope(provider, capability string, timeout time.Duration, payload any, metadata map[string]string) (RequestEnvelope, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	capability = strings.ToLower(strings.TrimSpace(capability))
	if provider == "" {
		return RequestEnvelope{}, fmt.Errorf("provider cannot be empty")
	}
	if capability == "" {
		return RequestEnvelope{}, fmt.Errorf("capability cannot be empty")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return RequestEnvelope{}, fmt.Errorf("encode payload: %w", err)
	}

	envelope := RequestEnvelope{
		Version:    EnvelopeVersionV1,
		RequestID:  newRequestID(),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Capability: capability,
		Provider:   provider,
		TimeoutMS:  timeout.Milliseconds(),
		Payload:    rawPayload,
	}
	if len(metadata) > 0 {
		envelope.Metadata = cloneMetadata(metadata)
	}
	return envelope, nil
}

func DecodeMailSendOutput(raw json.RawMessage) (MailSendOutput, error) {
	if len(raw) == 0 {
		return MailSendOutput{Accepted: true}, nil
	}
	var out MailSendOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return MailSendOutput{}, fmt.Errorf("decode mail.send output: %w", err)
	}
	return out, nil
}

func cloneMetadata(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
