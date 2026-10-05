// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// PluginBridge delivers outbox messages through an external capability
// plugin — an executable `nucleus-plugin-<provider>` speaking the plugin
// SDK's envelope (pkg/plugins) — so a message can reach a broker or an
// endpoint the framework has no code for.
//
// It serves two capabilities:
//
//   - plugins.CapabilityQueuePublish: each message becomes a
//     plugins.QueuePublishPayload — the queue topic (the message's own topic
//     unless PluginConfig.Topic names another), the message id as the key,
//     the payload JSON as the body, and PluginConfig.Headers.
//   - plugins.CapabilityWebhookDeliver: each message becomes a
//     plugins.WebhookDeliverPayload carrying exactly the request the
//     webhook bridge would send — the same JSON body, the same
//     X-Outbox-Payload-Encoding header and, with a Secret, the same
//     X-Nucleus-Signature — for the plugin to deliver.
//
// The envelope's metadata names the message: outbox_message_id,
// outbox_topic, outbox_attempt and outbox_bridge. Delivery is at least
// once, as everywhere in the outbox: a plugin that must not act twice
// deduplicates on outbox_message_id (also the queue key).
//
// What a failure means is the plugin's to say, through the contract's exit
// codes. A validation error (10) or a rejection (30) that the response does
// not mark retriable is Permanent: the message goes to the dead letter on
// that attempt. Everything else — a transient error, a timeout, a crash, a
// binary that went missing — is retried with the outbox's backoff until
// its attempts run out, as a webhook that answers 503 is.
type PluginBridge struct {
	name       string
	provider   string
	capability string
	binary     string
	timeout    time.Duration
	host       plugins.Host
	headers    map[string]string
	topic      string
	url        string
	method     string
	secret     string
	encoding   string
}

// PluginConfig configures a PluginBridge.
type PluginConfig struct {
	// Name is the bridge's name: what a route points at.
	Name string
	// Provider is the <provider> of the nucleus-plugin-<provider>
	// executable.
	Provider string
	// Capability is what the plugin is asked to do with each message:
	// plugins.CapabilityQueuePublish or plugins.CapabilityWebhookDeliver.
	Capability string
	// Binary is the executable to run. Empty looks up
	// nucleus-plugin-<Provider> on PATH, once, when the bridge is built.
	Binary string
	// Timeout bounds one delivery: it is the envelope's timeout_ms, and the
	// host kills the plugin when it runs out. Zero is 10 seconds.
	Timeout time.Duration
	// Policy is the configuration's plugins allowlist (plugins.*). A
	// provider it does not allow to run Capability is refused before it is
	// executed. The zero Policy allows every plugin.
	Policy plugins.Policy
	// Host runs the plugin. Nil is plugins.LocalHost{}.
	Host plugins.Host
	// Headers are, for queue.publish, the headers of every published
	// message; for webhook.deliver, extra HTTP headers of every delivery —
	// the contract headers win over them, as on the webhook bridge.
	Headers map[string]string

	// Topic is, for queue.publish, the queue topic every message is
	// published under. Empty publishes each message under its own outbox
	// topic.
	Topic string

	// URL, Method, Secret and PayloadEncoding are, for webhook.deliver,
	// what the webhook bridge takes (see WebhookConfig): the endpoint
	// (required), the HTTP method (POST when empty), the HMAC signing
	// secret and the wire shape of the payload field.
	URL             string
	Method          string
	Secret          string
	PayloadEncoding string
}

// NewPluginBridge builds the bridge and checks, once, that it can deliver:
// the configuration is complete, the policy allows the provider to run the
// capability, the executable exists, and asked for its capabilities it
// lists this one. A bridge that cannot deliver is an error here, not a
// message that fails later.
func NewPluginBridge(cfg PluginConfig) (*PluginBridge, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, errors.New("outbox: plugin bridge: name is required")
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, fmt.Errorf("outbox: plugin bridge %q: provider is required — the <provider> of %s<provider>", name, plugins.GenericBinaryPrefix)
	}
	if strings.ContainsAny(provider, `/\ `) {
		return nil, fmt.Errorf("outbox: plugin bridge %q: provider %q is not a provider name — use the <provider> of %s<provider>, not a path", name, cfg.Provider, plugins.GenericBinaryPrefix)
	}
	capability := strings.ToLower(strings.TrimSpace(cfg.Capability))
	switch capability {
	case plugins.CapabilityQueuePublish, plugins.CapabilityWebhookDeliver:
	case "":
		return nil, fmt.Errorf("outbox: plugin bridge %q: capability is required (%s or %s)", name, plugins.CapabilityQueuePublish, plugins.CapabilityWebhookDeliver)
	default:
		return nil, fmt.Errorf("outbox: plugin bridge %q: capability %q is not one the outbox delivers through (%s or %s)", name, cfg.Capability, plugins.CapabilityQueuePublish, plugins.CapabilityWebhookDeliver)
	}

	b := &PluginBridge{
		name:       name,
		provider:   provider,
		capability: capability,
		timeout:    cfg.Timeout,
		host:       cfg.Host,
		headers:    cloneHeaders(cfg.Headers),
		topic:      strings.TrimSpace(cfg.Topic),
	}
	if b.timeout <= 0 {
		b.timeout = 10 * time.Second
	}
	if b.host == nil {
		b.host = plugins.LocalHost{}
	}
	if capability == plugins.CapabilityWebhookDeliver {
		b.url = strings.TrimSpace(cfg.URL)
		if b.url == "" {
			return nil, fmt.Errorf("outbox: plugin bridge %q: url is required for %s", name, capability)
		}
		b.method = strings.ToUpper(strings.TrimSpace(cfg.Method))
		if b.method == "" {
			b.method = http.MethodPost
		}
		b.secret = cfg.Secret
		encoding, err := normalizePayloadEncoding(cfg.PayloadEncoding)
		if err != nil {
			return nil, fmt.Errorf("outbox: plugin bridge %q: %w", name, err)
		}
		b.encoding = encoding
	}

	if refused := cfg.Policy.AllowPlugin(provider, capability); refused != nil {
		return nil, fmt.Errorf("outbox: plugin bridge %q: %w — list it under plugins.allowed with capabilities [%s]", name, refused, capability)
	}

	b.binary = strings.TrimSpace(cfg.Binary)
	if b.binary == "" {
		path, err := exec.LookPath(plugins.GenericBinaryPrefix + provider)
		if err != nil {
			return nil, fmt.Errorf("outbox: plugin bridge %q: %s%s is not on PATH: %w", name, plugins.GenericBinaryPrefix, provider, err)
		}
		b.binary = path
	}
	caps, err := b.host.ProbeCapabilities(context.Background(), b.binary, b.timeout)
	if err != nil {
		return nil, fmt.Errorf("outbox: plugin bridge %q: ask %s for its capabilities: %w", name, b.binary, err)
	}
	if !containsCapability(caps, capability) {
		return nil, fmt.Errorf("outbox: plugin bridge %q: %s advertises %v, not %s", name, b.binary, caps, capability)
	}
	return b, nil
}

// Name returns the bridge name.
func (b *PluginBridge) Name() string { return b.name }

// Send hands one message to the plugin and waits for its answer.
func (b *PluginBridge) Send(ctx context.Context, msg Message) error {
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := b.payload(msg)
	if err != nil {
		return err
	}
	request, err := plugins.NewRequestEnvelope(b.provider, b.capability, b.timeout, payload, map[string]string{
		"outbox_message_id": msg.ID,
		"outbox_topic":      msg.Topic,
		"outbox_attempt":    strconv.Itoa(msg.Attempts),
		"outbox_bridge":     b.name,
	})
	if err != nil {
		return Permanent(fmt.Errorf("outbox: plugin bridge %q: message %s: %w", b.name, msg.ID, err))
	}
	response, err := b.host.ExecuteRequest(ctx, b.binary, request, b.timeout)
	if err != nil {
		var execErr *plugins.ExecutionError
		if ctx.Err() == nil && errors.As(err, &execErr) && !execErr.Retriable &&
			(execErr.ExitCode == plugins.ExitCodeValidation || execErr.ExitCode == plugins.ExitCodeRejected) {
			return Permanent(fmt.Errorf("outbox: plugin bridge %q: %s refused message %s and does not mark it retriable: %w", b.name, b.binary, msg.ID, err))
		}
		return fmt.Errorf("outbox: plugin bridge %q: %s did not deliver message %s: %w", b.name, b.binary, msg.ID, err)
	}
	return b.accepted(msg, response.Output)
}

// payload is the capability's request body for msg.
func (b *PluginBridge) payload(msg Message) (any, error) {
	if b.capability == plugins.CapabilityQueuePublish {
		body := json.RawMessage(msg.Payload)
		if len(body) == 0 {
			body = json.RawMessage("null")
		}
		if !json.Valid(body) {
			// Store.Enqueue always stores JSON; only a message built by hand
			// reaches here, and no attempt will make it JSON.
			return nil, Permanent(fmt.Errorf("outbox: plugin bridge %q: message %s: the payload is not JSON, and %s carries a JSON body", b.name, msg.ID, b.capability))
		}
		topic := b.topic
		if topic == "" {
			topic = msg.Topic
		}
		return plugins.QueuePublishPayload{Topic: topic, Key: msg.ID, Body: body, Headers: cloneHeaders(b.headers)}, nil
	}

	body, encoding, err := webhookBody(msg, b.encoding)
	if err != nil {
		return nil, Permanent(fmt.Errorf("outbox: plugin bridge %q: message %s: %w", b.name, msg.ID, err))
	}
	headers := map[string]string{"Content-Type": "application/json"}
	for k, v := range b.headers {
		headers[http.CanonicalHeaderKey(k)] = v
	}
	headers[WebhookPayloadEncodingHeader] = encoding
	if b.secret != "" {
		headers[WebhookSignatureHeader] = signWebhookBody(b.secret, body)
	}
	return plugins.WebhookDeliverPayload{
		URL:     b.url,
		Method:  b.method,
		Headers: headers,
		Body:    string(body),
		// The request has to end before the plugin does: the host kills the
		// plugin at the envelope's timeout, and a plugin killed mid-request
		// cannot say what the endpoint answered.
		TimeoutMS: (b.timeout - b.timeout/5).Milliseconds(),
	}, nil
}

// accepted reads the plugin's output: a delivery counts when the plugin
// accepted it and, for webhook.deliver, the status it reports (if any) is a
// 2xx — the rule the webhook bridge applies to the response it reads.
func (b *PluginBridge) accepted(msg Message, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if b.capability == plugins.CapabilityQueuePublish {
		var out plugins.QueuePublishOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			return fmt.Errorf("outbox: plugin bridge %q: decode the %s output for message %s: %w", b.name, b.capability, msg.ID, err)
		}
		if !out.Accepted {
			return fmt.Errorf("outbox: plugin bridge %q: %s did not accept message %s", b.name, b.binary, msg.ID)
		}
		return nil
	}
	var out plugins.WebhookDeliverOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("outbox: plugin bridge %q: decode the %s output for message %s: %w", b.name, b.capability, msg.ID, err)
	}
	if !out.Accepted {
		return fmt.Errorf("outbox: plugin bridge %q: %s did not accept message %s", b.name, b.binary, msg.ID)
	}
	if out.StatusCode != 0 && (out.StatusCode < 200 || out.StatusCode >= 300) {
		return fmt.Errorf("outbox: plugin bridge %q: the endpoint answered %d to message %s", b.name, out.StatusCode, msg.ID)
	}
	return nil
}

// Healthy reports whether the executable is still there to run. It does
// not run it: a health check that spawned a process on every probe would
// cost more than the deliveries.
func (b *PluginBridge) Healthy(context.Context) error {
	info, err := os.Stat(b.binary)
	if err != nil {
		return fmt.Errorf("outbox: plugin bridge %q: %w", b.name, err)
	}
	if info.IsDir() {
		return fmt.Errorf("outbox: plugin bridge %q: %s is a directory", b.name, b.binary)
	}
	return nil
}

// Close releases nothing: each delivery is its own process.
func (b *PluginBridge) Close() error { return nil }

func containsCapability(caps []string, capability string) bool {
	for _, c := range caps {
		if strings.EqualFold(strings.TrimSpace(c), capability) {
			return true
		}
	}
	return false
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
