package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/plugins"
)

func TestRunCapabilitiesJSON(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	exitCode := run([]string{"capabilities", "--json"}, strings.NewReader(""), &out, &errOut, fixedNow)
	if exitCode != plugins.ExitCodeSuccess {
		t.Fatalf("expected success exit code, got %d (stderr=%s)", exitCode, errOut.String())
	}

	var payload struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode capabilities output: %v", err)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0] != plugins.CapabilityMailSend {
		t.Fatalf("unexpected capabilities: %v", payload.Capabilities)
	}
}

func TestRunMailExecuteSuccess(t *testing.T) {
	rawRequest := mustJSON(t, plugins.RequestEnvelope{
		Version:    plugins.EnvelopeVersionV1,
		RequestID:  "req_mail_success",
		Capability: plugins.CapabilityMailSend,
		Provider:   mailProviderName,
		Payload: mustJSON(t, plugins.MailSendPayload{
			From:    "noreply@example.com",
			To:      []string{"dev@example.com"},
			Subject: "mail smoke",
			Body:    "hello",
		}),
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	exitCode := run(nil, bytes.NewReader(rawRequest), &out, &errOut, fixedNow)
	if exitCode != plugins.ExitCodeSuccess {
		t.Fatalf("expected success exit code, got %d (stderr=%s)", exitCode, errOut.String())
	}

	var response plugins.ResponseEnvelope
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK {
		t.Fatalf("expected ok response, got %+v", response)
	}
	if response.RequestID != "req_mail_success" {
		t.Fatalf("unexpected request id: %s", response.RequestID)
	}
	if !strings.HasPrefix(response.ProviderRequestID, mailProviderName+"-") {
		t.Fatalf("unexpected provider request id: %s", response.ProviderRequestID)
	}

	output, err := plugins.DecodeMailSendOutput(response.Output)
	if err != nil {
		t.Fatalf("decode mail output: %v", err)
	}
	if !output.Accepted {
		t.Fatalf("expected accepted output: %+v", output)
	}
}

func TestRunMailExecuteUnsupportedCapability(t *testing.T) {
	rawRequest := mustJSON(t, plugins.RequestEnvelope{
		Version:    plugins.EnvelopeVersionV1,
		RequestID:  "req_mail_cap",
		Capability: plugins.CapabilityQueuePublish,
		Provider:   mailProviderName,
		Payload:    mustJSON(t, map[string]any{"topic": "events"}),
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	exitCode := run(nil, bytes.NewReader(rawRequest), &out, &errOut, fixedNow)
	if exitCode != plugins.ExitCodeValidation {
		t.Fatalf("expected validation exit code, got %d", exitCode)
	}

	var response plugins.ResponseEnvelope
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OK {
		t.Fatalf("expected non-ok response")
	}
	if response.Error == nil || response.Error.Code != "CAPABILITY_UNSUPPORTED" {
		t.Fatalf("unexpected error payload: %+v", response.Error)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}
	return raw
}

func fixedNow() time.Time {
	return time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
}
