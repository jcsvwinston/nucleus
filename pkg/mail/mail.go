package mail

import (
	"context"
	"fmt"
	"net/mail"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// Message represents one outbound email.
type Message struct {
	From    string
	To      []string
	Subject string
	Body    string

	// Headers holds optional custom headers appended after the
	// framework-generated ones (From, To, Subject, MIME-Version,
	// Content-Type). The built-in senders (SMTP and external
	// plugins) validate the map on Send: a key must be non-empty
	// and neither key nor value may contain CR or LF, so
	// caller-supplied input cannot inject additional headers (e.g.
	// an extra Bcc). Values are trimmed; a header whose value is
	// empty after trimming is omitted. Custom providers registered
	// via RegisterProvider are responsible for their own emission.
	Headers map[string]string
}

// Sender sends outbound email messages.
type Sender interface {
	Send(ctx context.Context, message Message) error
}

// HealthChecker is an optional interface a Sender may implement to
// expose a non-destructive liveness check. The /healthz handler in
// pkg/app type-asserts for this interface; senders that do not
// implement it are not probed (so the response stays free of
// information-free "skipped" rows).
//
// Implementations should keep Healthy cheap and non-destructive — at
// minimum, no actual mail is sent. For SMTP that means a TCP dial
// plus HELO/QUIT; for HTTP API providers it typically means a HEAD
// against a documented health endpoint.
type HealthChecker interface {
	Healthy(ctx context.Context) error
}

// ProviderFactory builds a Sender from provider-specific configuration.
type ProviderFactory func(cfg Config) (Sender, error)

// Config holds provider-agnostic and provider-specific mail settings.
// Only protocol-universal providers (SMTP) ship in-tree; provider-
// specific senders (SendGrid, Mailgun, AWS SES, Postmark, …) are
// installed as `nucleus-plugin-<provider>` binaries on PATH and
// discovered via the external sender. The `mail.send` capability
// contract is documented in `docs/reference/PLUGIN_SDK.md`.
type Config struct {
	Driver  string
	Timeout time.Duration

	// SMTP
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string

	// CircuitBreaker, when Enabled, wraps the returned Sender.Send
	// with a pkg/circuit breaker. Healthy (if the underlying provider
	// implements HealthChecker) bypasses the breaker so /healthz
	// observes a recovering dependency.
	CircuitBreaker CircuitBreakerConfig
}

var (
	providersMu sync.RWMutex
	providers   = map[string]ProviderFactory{}

	pluginHostMu sync.RWMutex
	pluginHost   plugins.Host = plugins.LocalHost{}
)

func init() {
	_ = RegisterProvider("noop", newNoopSender)
	_ = RegisterProvider("smtp", newSMTPSender)
}

// RegisterProvider registers a named mail provider factory.
func RegisterProvider(name string, factory ProviderFactory) error {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("provider factory cannot be nil")
	}

	providersMu.Lock()
	defer providersMu.Unlock()
	if _, exists := providers[normalized]; exists {
		return fmt.Errorf("mail provider %q already registered", normalized)
	}
	providers[normalized] = factory
	return nil
}

// RegisteredProviders returns the currently registered provider names sorted
// alphabetically. Built-in providers are included.
func RegisteredProviders() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()

	out := make([]string, 0, len(providers))
	for name := range providers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// SetPluginHost overrides the plugin runtime host used for external providers.
// Passing nil resets the default local executable host.
func SetPluginHost(host plugins.Host) {
	if host == nil {
		host = plugins.LocalHost{}
	}
	pluginHostMu.Lock()
	pluginHost = host
	pluginHostMu.Unlock()
}

func currentPluginHost() plugins.Host {
	pluginHostMu.RLock()
	host := pluginHost
	pluginHostMu.RUnlock()
	if host == nil {
		return plugins.LocalHost{}
	}
	return host
}

// NewSender resolves and constructs a mail sender for the given configuration.
//
// Resolution order:
// 1) built-in or registered provider
// 2) executable plugin on PATH named nucleus-plugin-<driver> with capability mail.send
func NewSender(cfg Config) (Sender, error) {
	normalized := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if normalized == "" {
		normalized = "noop"
	}
	cfg.Driver = normalized
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	providersMu.RLock()
	factory := providers[normalized]
	providersMu.RUnlock()
	if factory != nil {
		sender, err := factory(cfg)
		if err != nil {
			return nil, err
		}
		return maybeWrapBreaker(sender, normalized, cfg), nil
	}
	host := currentPluginHost()

	genericBinary := plugins.GenericBinaryPrefix + normalized
	if path, err := exec.LookPath(genericBinary); err == nil {
		if capabilities, capErr := host.ProbeCapabilities(context.Background(), path, cfg.Timeout); capErr == nil {
			if containsCapability(capabilities, plugins.CapabilityMailSend) {
				sender := newExternalSender(normalized, path, cfg.Timeout, host)
				return maybeWrapBreaker(sender, normalized, cfg), nil
			}
		}
	}

	return nil, fmt.Errorf(
		"unknown mail driver %q (register provider or install %s on PATH)",
		normalized,
		genericBinary,
	)
}

// maybeWrapBreaker returns sender unchanged when the circuit breaker is
// disabled or the driver is "noop"; otherwise it returns a breaker-
// decorated sender. The noop driver is excluded so dev-mode loops do
// not accumulate state in a breaker that has no real dependency.
func maybeWrapBreaker(sender Sender, driver string, cfg Config) Sender {
	if !cfg.CircuitBreaker.Enabled {
		return sender
	}
	if driver == "noop" {
		return sender
	}
	return wrapWithBreaker(sender, cfg.CircuitBreaker)
}

func containsCapability(values []string, capability string) bool {
	target := strings.ToLower(strings.TrimSpace(capability))
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func validateMessage(msg Message) error {
	from := strings.TrimSpace(msg.From)
	if from == "" {
		return fmt.Errorf("message from is required")
	}
	if strings.ContainsAny(from, "\r\n") {
		return fmt.Errorf("message from cannot contain newlines")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("invalid from address %q", from)
	}

	if len(msg.To) == 0 {
		return fmt.Errorf("message must have at least one recipient")
	}
	for _, recipient := range msg.To {
		trimmed := strings.TrimSpace(recipient)
		if trimmed == "" {
			return fmt.Errorf("message recipient cannot be empty")
		}
		if _, err := mail.ParseAddress(trimmed); err != nil {
			return fmt.Errorf("invalid recipient address %q", recipient)
		}
	}

	subject := strings.TrimSpace(msg.Subject)
	if subject == "" {
		return fmt.Errorf("message subject is required")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("message subject cannot contain newlines")
	}

	if strings.TrimSpace(msg.Body) == "" {
		return fmt.Errorf("message body is required")
	}

	for key, value := range msg.Headers {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("message header key cannot be empty")
		}
		if strings.ContainsAny(key, "\r\n") {
			return fmt.Errorf("message header key %q cannot contain newlines", key)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("message header %q value cannot contain newlines", key)
		}
	}
	return nil
}
