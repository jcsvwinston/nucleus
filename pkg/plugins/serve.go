// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Plugin is the plugin side of the contract: one handler per capability the
// executable provides. Serve advertises exactly the capabilities whose
// handler is set, decodes each request's payload into the capability's
// type, and writes the response envelope and the exit code the host reads.
//
// A handler that returns a nil error has accepted the request: Serve sets
// the output's Accepted field. To refuse or fail, return a *Failure (see
// Fail) — its exit code and code are what the host sees. Any other error is
// an internal failure (exit 50, retriable). Write logs to stderr: stdout
// carries the response envelope and nothing else.
type Plugin struct {
	MailSend       func(ctx context.Context, req RequestEnvelope, payload MailSendPayload) (MailSendOutput, error)
	QueuePublish   func(ctx context.Context, req RequestEnvelope, payload QueuePublishPayload) (QueuePublishOutput, error)
	WebhookDeliver func(ctx context.Context, req RequestEnvelope, payload WebhookDeliverPayload) (WebhookDeliverOutput, error)

	// Custom serves capabilities this package has no schema for (a
	// `sms.send`, say), keyed by capability name. The handler
	// decodes its own payload; its output is encoded as JSON.
	Custom map[string]func(ctx context.Context, req RequestEnvelope, payload json.RawMessage) (any, error)
}

// Failure is the error a handler returns to choose how the host sees a
// failed request: the process exit code, the machine-readable code and the
// message of the response's error, and whether the host may retry.
type Failure struct {
	ExitCode  int
	Code      string
	Message   string
	Retriable bool
}

func (f *Failure) Error() string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s (exit %d)", f.Code, f.Message, f.ExitCode)
}

// Fail builds a Failure. exitCode is one of the contract's codes —
// ExitCodeValidation (10), ExitCodeTransient (20), ExitCodeRejected (30),
// ExitCodeTimeout (40), ExitCodeInternal (50); any other value becomes
// ExitCodeInternal. Retriable follows the code's documented meaning: 20, 40
// and 50 are retriable.
func Fail(exitCode int, code, format string, args ...any) *Failure {
	switch exitCode {
	case ExitCodeValidation, ExitCodeTransient, ExitCodeRejected, ExitCodeTimeout, ExitCodeInternal:
	default:
		exitCode = ExitCodeInternal
	}
	if strings.TrimSpace(code) == "" {
		code = "PLUGIN_ERROR"
	}
	return &Failure{
		ExitCode:  exitCode,
		Code:      code,
		Message:   fmt.Sprintf(format, args...),
		Retriable: retriableByExitCode(exitCode),
	}
}

// maxRequestBytes bounds what Serve reads from stdin: a request larger than
// this is refused as a validation error rather than read into memory.
const maxRequestBytes = 32 << 20

// Serve runs the plugin side of the contract on the process: it answers
// `capabilities` (and `capabilities --json`) with the capabilities p
// serves, and with no arguments reads one request envelope from stdin,
// calls the handler, writes the response envelope to stdout and exits with
// the contract's exit code. It does not return.
//
//	func main() {
//		plugins.Serve(plugins.Plugin{
//			MailSend: func(ctx context.Context, req plugins.RequestEnvelope, m plugins.MailSendPayload) (plugins.MailSendOutput, error) {
//				// deliver m
//				return plugins.MailSendOutput{ProviderRequestID: "…"}, nil
//			},
//		})
//	}
//
// SIGINT and SIGTERM cancel the handler's context.
func Serve(p Plugin) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := ServeIO(ctx, p, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// ServeIO is Serve with the arguments and the streams passed in; it returns
// the exit code instead of exiting. A test drives a plugin through it.
func ServeIO(ctx context.Context, p Plugin, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	switch {
	case len(args) == 0:
		return serveRequest(ctx, p, stdin, stdout, stderr)
	case args[0] == "capabilities" && len(args) <= 2:
		caps := p.capabilities()
		if len(args) == 2 && args[1] != "--json" {
			break
		}
		if len(args) == 2 {
			raw, _ := json.Marshal(struct {
				Capabilities []string `json:"capabilities"`
			}{caps})
			fmt.Fprintln(stdout, string(raw))
			return ExitCodeSuccess
		}
		fmt.Fprintln(stdout, strings.Join(caps, "\n"))
		return ExitCodeSuccess
	}
	fmt.Fprintf(stderr, "usage: %s [capabilities [--json]]\n"+
		"  with no arguments, reads one request envelope (version %s) from stdin and writes the response to stdout\n",
		programName(), EnvelopeVersionV1)
	return ExitCodeValidation
}

// capabilities lists what p serves, sorted.
func (p Plugin) capabilities() []string {
	var out []string
	if p.MailSend != nil {
		out = append(out, CapabilityMailSend)
	}
	if p.QueuePublish != nil {
		out = append(out, CapabilityQueuePublish)
	}
	if p.WebhookDeliver != nil {
		out = append(out, CapabilityWebhookDeliver)
	}
	for name, h := range p.Custom {
		if h != nil && normalizeToken(name) != "" {
			out = append(out, normalizeToken(name))
		}
	}
	return normalizeCapabilities(out)
}

func serveRequest(ctx context.Context, p Plugin, stdin io.Reader, stdout, stderr io.Writer) int {
	started := time.Now()
	raw, err := io.ReadAll(io.LimitReader(stdin, maxRequestBytes+1))
	if err != nil {
		return respondFailure(stdout, stderr, "", Fail(ExitCodeValidation, "INVALID_ENVELOPE", "read the request from stdin: %v", err), started)
	}
	if len(raw) > maxRequestBytes {
		return respondFailure(stdout, stderr, "", Fail(ExitCodeValidation, "PAYLOAD_TOO_LARGE", "the request exceeds %d bytes", maxRequestBytes), started)
	}
	var req RequestEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(raw), &req); err != nil {
		return respondFailure(stdout, stderr, "", Fail(ExitCodeValidation, "INVALID_ENVELOPE", "the request is not a %s envelope: %v", EnvelopeVersionV1, err), started)
	}
	if v := strings.TrimSpace(req.Version); v != "" && v != EnvelopeVersionV1 {
		return respondFailure(stdout, stderr, req.RequestID, Fail(ExitCodeValidation, "UNSUPPORTED_VERSION", "envelope version %q; this plugin speaks %s", v, EnvelopeVersionV1), started)
	}
	if req.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	output, providerRequestID, err := p.dispatch(ctx, req)
	if err != nil {
		var failure *Failure
		switch {
		case errors.As(err, &failure):
		case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
			failure = Fail(ExitCodeTimeout, "DEADLINE_EXCEEDED", "%v", err)
		default:
			failure = Fail(ExitCodeInternal, "INTERNAL", "%v", err)
		}
		return respondFailure(stdout, stderr, req.RequestID, failure, started)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return respondFailure(stdout, stderr, req.RequestID, Fail(ExitCodeInternal, "INVALID_OUTPUT", "encode the output: %v", err), started)
	}
	writeResponse(stdout, ResponseEnvelope{
		Version:           EnvelopeVersionV1,
		RequestID:         req.RequestID,
		OK:                true,
		ProviderRequestID: providerRequestID,
		Output:            encoded,
		Metrics:           &ResponseMetrics{DurationMS: time.Since(started).Milliseconds()},
	})
	return ExitCodeSuccess
}

// dispatch decodes the payload for the request's capability and calls its
// handler.
func (p Plugin) dispatch(ctx context.Context, req RequestEnvelope) (any, string, error) {
	capability := normalizeToken(req.Capability)
	switch {
	case capability == CapabilityMailSend && p.MailSend != nil:
		var payload MailSendPayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, "", err
		}
		out, err := p.MailSend(ctx, req, payload)
		if err != nil {
			return nil, "", err
		}
		out.Accepted = true
		return out, out.ProviderRequestID, nil
	case capability == CapabilityQueuePublish && p.QueuePublish != nil:
		var payload QueuePublishPayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, "", err
		}
		out, err := p.QueuePublish(ctx, req, payload)
		if err != nil {
			return nil, "", err
		}
		out.Accepted = true
		return out, out.MessageID, nil
	case capability == CapabilityWebhookDeliver && p.WebhookDeliver != nil:
		var payload WebhookDeliverPayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, "", err
		}
		out, err := p.WebhookDeliver(ctx, req, payload)
		if err != nil {
			return nil, "", err
		}
		out.Accepted = true
		return out, "", nil
	}
	for name, h := range p.Custom {
		if h != nil && normalizeToken(name) == capability {
			out, err := h(ctx, req, req.Payload)
			return out, "", err
		}
	}
	return nil, "", Fail(ExitCodeValidation, "UNSUPPORTED_CAPABILITY", "capability %q is not served by this plugin (it serves %s)",
		req.Capability, strings.Join(p.capabilities(), ", "))
}

// decodePayload reads a payload permissively: an unknown field is ignored,
// so a host that adds a field to a schema does not break an older plugin.
func decodePayload(raw json.RawMessage, into any) error {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return Fail(ExitCodeValidation, "INVALID_PAYLOAD", "the request has no payload")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return Fail(ExitCodeValidation, "INVALID_PAYLOAD", "decode the payload: %v", err)
	}
	return nil
}

func respondFailure(stdout, stderr io.Writer, requestID string, f *Failure, started time.Time) int {
	writeResponse(stdout, ResponseEnvelope{
		Version:   EnvelopeVersionV1,
		RequestID: requestID,
		OK:        false,
		Retriable: f.Retriable,
		Error:     &ResponseError{Code: f.Code, Message: f.Message},
		Metrics:   &ResponseMetrics{DurationMS: time.Since(started).Milliseconds()},
	})
	fmt.Fprintf(stderr, "%s: %s: %s\n", programName(), f.Code, f.Message)
	return f.ExitCode
}

func writeResponse(w io.Writer, resp ResponseEnvelope) {
	raw, err := json.Marshal(resp)
	if err != nil {
		// Every field above is a plain value; this cannot fail.
		return
	}
	fmt.Fprintln(w, string(raw))
}

func programName() string {
	if len(os.Args) > 0 {
		name := os.Args[0]
		if i := strings.LastIndexAny(name, `/\`); i >= 0 {
			name = name[i+1:]
		}
		if name != "" {
			return name
		}
	}
	return "nucleus-plugin"
}
