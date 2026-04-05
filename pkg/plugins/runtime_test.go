package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecuteRequestSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-success")
	writePluginRuntimeExecutable(t, pluginPath, `#!/bin/sh
cat >/dev/null
echo '{"version":"v1","ok":true,"output":{"accepted":true}}'
exit 0
`)

	request, err := NewRequestEnvelope(
		"sendgrid",
		CapabilityMailSend,
		time.Second,
		MailSendPayload{
			From:    "noreply@example.com",
			To:      []string{"dev@example.com"},
			Subject: "hello",
			Body:    "world",
		},
		map[string]string{"env": "test"},
	)
	if err != nil {
		t.Fatalf("NewRequestEnvelope failed: %v", err)
	}

	response, err := ExecuteRequest(context.Background(), pluginPath, request, time.Second)
	if err != nil {
		t.Fatalf("ExecuteRequest failed: %v", err)
	}
	if !response.OK {
		t.Fatalf("expected ok response, got: %+v", response)
	}

	output, err := DecodeMailSendOutput(response.Output)
	if err != nil {
		t.Fatalf("DecodeMailSendOutput failed: %v", err)
	}
	if !output.Accepted {
		t.Fatalf("expected accepted=true output, got: %+v", output)
	}
}

func TestExecuteRequestErrorExitCodeMapping(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-error")
	writePluginRuntimeExecutable(t, pluginPath, `#!/bin/sh
cat >/dev/null
echo '{"version":"v1","ok":false,"retriable":true,"error":{"code":"PROVIDER_RATE_LIMIT","message":"rate limited"}}'
exit 20
`)

	request, err := NewRequestEnvelope(
		"sendgrid",
		CapabilityMailSend,
		time.Second,
		MailSendPayload{
			From:    "noreply@example.com",
			To:      []string{"dev@example.com"},
			Subject: "hello",
			Body:    "world",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequestEnvelope failed: %v", err)
	}

	_, err = ExecuteRequest(context.Background(), pluginPath, request, time.Second)
	if err == nil {
		t.Fatal("expected ExecuteRequest to fail for non-zero exit")
	}

	execErr, ok := err.(*ExecutionError)
	if !ok {
		t.Fatalf("expected ExecutionError, got: %T (%v)", err, err)
	}
	if execErr.ExitCode != ExitCodeTransient {
		t.Fatalf("expected exit code %d, got %d", ExitCodeTransient, execErr.ExitCode)
	}
	if !execErr.Retriable {
		t.Fatalf("expected retriable=true, got false: %+v", execErr)
	}
	if execErr.Code != "PROVIDER_RATE_LIMIT" {
		t.Fatalf("unexpected error code: %s", execErr.Code)
	}
}

func TestExecuteRequestTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-timeout")
	writePluginRuntimeExecutable(t, pluginPath, `#!/bin/sh
sleep 2
cat >/dev/null
echo '{"version":"v1","ok":true}'
exit 0
`)

	request, err := NewRequestEnvelope("sendgrid", CapabilityMailSend, time.Second, map[string]string{"x": "y"}, nil)
	if err != nil {
		t.Fatalf("NewRequestEnvelope failed: %v", err)
	}

	_, err = ExecuteRequest(context.Background(), pluginPath, request, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	execErr, ok := err.(*ExecutionError)
	if !ok {
		t.Fatalf("expected ExecutionError, got: %T (%v)", err, err)
	}
	if execErr.Code != "DEADLINE_EXCEEDED" {
		t.Fatalf("unexpected timeout code: %s", execErr.Code)
	}
	if !execErr.Retriable {
		t.Fatalf("expected timeout to be retriable: %+v", execErr)
	}
}

func TestNewRequestEnvelopeAndSchemaTypes(t *testing.T) {
	payload := MailSendPayload{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "hello",
		Body:    "world",
	}
	request, err := NewRequestEnvelope("MailGun", CapabilityMailSend, 2*time.Second, payload, map[string]string{"trace_id": "abc"})
	if err != nil {
		t.Fatalf("NewRequestEnvelope failed: %v", err)
	}
	if request.Version != EnvelopeVersionV1 {
		t.Fatalf("unexpected envelope version: %s", request.Version)
	}
	if request.Provider != "mailgun" {
		t.Fatalf("expected normalized provider, got: %s", request.Provider)
	}
	if request.Capability != CapabilityMailSend {
		t.Fatalf("unexpected capability: %s", request.Capability)
	}
	if request.TimeoutMS != 2000 {
		t.Fatalf("unexpected timeout_ms: %d", request.TimeoutMS)
	}
	if strings.TrimSpace(request.RequestID) == "" {
		t.Fatal("expected request_id to be set")
	}
	if request.Metadata["trace_id"] != "abc" {
		t.Fatalf("unexpected metadata: %+v", request.Metadata)
	}

	var decoded MailSendPayload
	if err := json.Unmarshal(request.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal request payload failed: %v", err)
	}
	if decoded.Subject != "hello" {
		t.Fatalf("unexpected decoded payload: %+v", decoded)
	}
}

func writePluginRuntimeExecutable(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o755); err != nil {
		t.Fatalf("write plugin executable failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod plugin executable failed: %v", err)
		}
	}
}
