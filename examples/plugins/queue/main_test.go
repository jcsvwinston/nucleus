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
	if len(payload.Capabilities) != 1 || payload.Capabilities[0] != plugins.CapabilityQueuePublish {
		t.Fatalf("unexpected capabilities: %v", payload.Capabilities)
	}
}

func TestRunQueueExecuteSuccess(t *testing.T) {
	rawRequest := mustJSON(t, plugins.RequestEnvelope{
		Version:    plugins.EnvelopeVersionV1,
		RequestID:  "req_queue_success",
		Capability: plugins.CapabilityQueuePublish,
		Provider:   queueProviderName,
		Payload: mustJSON(t, plugins.QueuePublishPayload{
			Topic: "events.users",
			Body:  mustJSON(t, map[string]any{"event": "user.created"}),
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
	if response.RequestID != "req_queue_success" {
		t.Fatalf("unexpected request id: %s", response.RequestID)
	}

	var output plugins.QueuePublishOutput
	if err := json.Unmarshal(response.Output, &output); err != nil {
		t.Fatalf("decode queue output: %v", err)
	}
	if !output.Accepted {
		t.Fatalf("expected accepted output: %+v", output)
	}
	if !strings.HasPrefix(output.MessageID, queueProviderName+"-") {
		t.Fatalf("unexpected message id: %s", output.MessageID)
	}
}

func TestRunQueueExecuteInvalidPayload(t *testing.T) {
	rawRequest := mustJSON(t, plugins.RequestEnvelope{
		Version:    plugins.EnvelopeVersionV1,
		RequestID:  "req_queue_invalid",
		Capability: plugins.CapabilityQueuePublish,
		Provider:   queueProviderName,
		Payload: mustJSON(t, plugins.QueuePublishPayload{
			Topic: "",
			Body:  nil,
		}),
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
	if response.Error == nil || response.Error.Code != "PAYLOAD_INVALID" {
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
