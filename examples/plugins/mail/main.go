package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/plugins"
)

const mailProviderName = "examplemail"

type clockFunc func() time.Time

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now clockFunc) int {
	if now == nil {
		now = time.Now
	}

	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "capabilities":
			return runCapabilities(args[1:], stdout, stderr, []string{plugins.CapabilityMailSend})
		case "help", "-h", "--help":
			fmt.Fprintln(stdout, "Usage: goframe-plugin-examplemail [capabilities [--json]]")
			fmt.Fprintln(stdout, "Reads SDK v1 envelope from stdin when no subcommand is given.")
			return plugins.ExitCodeSuccess
		default:
			fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
			return plugins.ExitCodeValidation
		}
	}

	return runMailExecute(stdin, stdout, stderr, now)
}

func runCapabilities(args []string, stdout, stderr io.Writer, capabilities []string) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "capabilities accepts only optional --json")
		return plugins.ExitCodeValidation
	}
	if len(args) == 1 {
		if strings.TrimSpace(args[0]) != "--json" {
			fmt.Fprintf(stderr, "unknown flag %q\n", args[0])
			return plugins.ExitCodeValidation
		}
		payload := map[string][]string{"capabilities": capabilities}
		if err := json.NewEncoder(stdout).Encode(payload); err != nil {
			fmt.Fprintf(stderr, "encode capabilities JSON: %v\n", err)
			return plugins.ExitCodeInternal
		}
		return plugins.ExitCodeSuccess
	}

	fmt.Fprintln(stdout, strings.Join(capabilities, " "))
	return plugins.ExitCodeSuccess
}

func runMailExecute(stdin io.Reader, stdout, stderr io.Writer, now clockFunc) int {
	started := now().UTC()

	raw, err := io.ReadAll(stdin)
	if err != nil {
		return emitErrorResponse(stdout, stderr, plugins.RequestEnvelope{}, plugins.ExitCodeInternal, "READ_ERROR", fmt.Sprintf("read request: %v", err), true)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return emitErrorResponse(stdout, stderr, plugins.RequestEnvelope{}, plugins.ExitCodeValidation, "EMPTY_REQUEST", "request payload is empty", false)
	}

	var request plugins.RequestEnvelope
	if err := json.Unmarshal(raw, &request); err != nil {
		return emitErrorResponse(stdout, stderr, plugins.RequestEnvelope{}, plugins.ExitCodeValidation, "INVALID_JSON", fmt.Sprintf("decode request envelope: %v", err), false)
	}
	if strings.TrimSpace(request.Version) == "" {
		request.Version = plugins.EnvelopeVersionV1
	}

	if request.Capability != plugins.CapabilityMailSend {
		return emitErrorResponse(stdout, stderr, request, plugins.ExitCodeValidation, "CAPABILITY_UNSUPPORTED", fmt.Sprintf("capability %q is not supported", request.Capability), false)
	}
	if provider := strings.TrimSpace(request.Provider); provider != "" && provider != mailProviderName {
		return emitErrorResponse(stdout, stderr, request, plugins.ExitCodeValidation, "PROVIDER_MISMATCH", fmt.Sprintf("provider %q does not match %q", provider, mailProviderName), false)
	}

	var payload plugins.MailSendPayload
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		return emitErrorResponse(stdout, stderr, request, plugins.ExitCodeValidation, "PAYLOAD_INVALID", fmt.Sprintf("decode mail payload: %v", err), false)
	}
	if err := validateMailPayload(payload); err != nil {
		return emitErrorResponse(stdout, stderr, request, plugins.ExitCodeValidation, "PAYLOAD_INVALID", err.Error(), false)
	}

	providerRequestID := fmt.Sprintf("%s-%d", mailProviderName, now().UTC().UnixNano())
	output, err := json.Marshal(plugins.MailSendOutput{
		Accepted:          true,
		ProviderRequestID: providerRequestID,
	})
	if err != nil {
		return emitErrorResponse(stdout, stderr, request, plugins.ExitCodeInternal, "ENCODE_OUTPUT_FAILED", fmt.Sprintf("encode output: %v", err), true)
	}

	durationMS := now().UTC().Sub(started).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}

	response := plugins.ResponseEnvelope{
		Version:           request.Version,
		RequestID:         request.RequestID,
		OK:                true,
		ProviderRequestID: providerRequestID,
		Retriable:         false,
		Output:            output,
		Metrics: &plugins.ResponseMetrics{
			DurationMS: durationMS,
		},
	}
	if err := json.NewEncoder(stdout).Encode(response); err != nil {
		fmt.Fprintf(stderr, "encode response: %v\n", err)
		return plugins.ExitCodeInternal
	}
	return plugins.ExitCodeSuccess
}

func emitErrorResponse(stdout, stderr io.Writer, request plugins.RequestEnvelope, exitCode int, code, message string, retriable bool) int {
	if strings.TrimSpace(request.Version) == "" {
		request.Version = plugins.EnvelopeVersionV1
	}
	response := plugins.ResponseEnvelope{
		Version:   request.Version,
		RequestID: request.RequestID,
		OK:        false,
		Retriable: retriable,
		Error: &plugins.ResponseError{
			Code:    strings.TrimSpace(code),
			Message: strings.TrimSpace(message),
		},
	}
	if err := json.NewEncoder(stdout).Encode(response); err != nil {
		fmt.Fprintf(stderr, "encode error response: %v\n", err)
		return plugins.ExitCodeInternal
	}
	if exitCode == 0 {
		return plugins.ExitCodeInternal
	}
	return exitCode
}

func validateMailPayload(payload plugins.MailSendPayload) error {
	if strings.TrimSpace(payload.From) == "" {
		return fmt.Errorf("payload.from is required")
	}
	if len(payload.To) == 0 {
		return fmt.Errorf("payload.to must have at least one recipient")
	}
	for _, recipient := range payload.To {
		if strings.TrimSpace(recipient) == "" {
			return fmt.Errorf("payload.to contains empty recipient")
		}
	}
	if strings.TrimSpace(payload.Subject) == "" {
		return fmt.Errorf("payload.subject is required")
	}
	if strings.TrimSpace(payload.Body) == "" {
		return fmt.Errorf("payload.body is required")
	}
	return nil
}
