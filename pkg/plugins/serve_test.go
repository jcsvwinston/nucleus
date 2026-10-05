// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// serveHelperEnv turns this test binary into a plugin built on Serve, so the
// host side (ProbeCapabilities, ExecuteRequest) can talk to the real Serve —
// os.Exit and all — without a separate build.
const serveHelperEnv = "NUCLEUS_PLUGINS_SERVE_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(serveHelperEnv) == "1" {
		Serve(testPlugin())
	}
	os.Exit(m.Run())
}

// testPlugin serves the three typed capabilities and one custom one. Its
// mail.send fails on demand, through the subject, so every exit path is
// reachable from a request.
func testPlugin() Plugin {
	return Plugin{
		MailSend: func(ctx context.Context, req RequestEnvelope, m MailSendPayload) (MailSendOutput, error) {
			switch m.Subject {
			case "reject":
				return MailSendOutput{}, Fail(ExitCodeRejected, "PROVIDER_REJECTED", "recipient %s is suppressed", m.To[0])
			case "boom":
				return MailSendOutput{}, errors.New("something broke")
			case "slow":
				<-ctx.Done()
				return MailSendOutput{}, ctx.Err()
			}
			return MailSendOutput{ProviderRequestID: "prov-" + req.RequestID}, nil
		},
		QueuePublish: func(_ context.Context, _ RequestEnvelope, q QueuePublishPayload) (QueuePublishOutput, error) {
			return QueuePublishOutput{MessageID: "msg-" + q.Topic}, nil
		},
		WebhookDeliver: func(_ context.Context, _ RequestEnvelope, w WebhookDeliverPayload) (WebhookDeliverOutput, error) {
			return WebhookDeliverOutput{StatusCode: 204}, nil
		},
		Custom: map[string]func(context.Context, RequestEnvelope, json.RawMessage) (any, error){
			"Audit.Record": func(_ context.Context, _ RequestEnvelope, raw json.RawMessage) (any, error) {
				var in struct {
					Plan string `json:"plan"`
				}
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, Fail(ExitCodeValidation, "INVALID_PAYLOAD", "%v", err)
				}
				return map[string]string{"recorded": in.Plan}, nil
			},
		},
	}
}

func serveOnce(t *testing.T, p Plugin, req any) (int, ResponseEnvelope, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := ServeIO(context.Background(), p, nil, bytes.NewReader(raw), &out, &errb)
	var resp ResponseEnvelope
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("stdout is not one response envelope: %v\n%s", err, out.String())
	}
	return code, resp, errb.String()
}

func mustEnvelope(t *testing.T, capability string, payload any) RequestEnvelope {
	t.Helper()
	req, err := NewRequestEnvelope("test", capability, 5*time.Second, payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestServeIO_Capabilities(t *testing.T) {
	var out bytes.Buffer
	if code := ServeIO(context.Background(), testPlugin(), []string{"capabilities", "--json"}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("capabilities --json exited %d: %s", code, out.String())
	}
	caps, err := parseCapabilitiesJSON(out.String())
	if err != nil {
		t.Fatal(err)
	}
	want := "audit.record,mail.send,queue.publish,webhook.deliver"
	if strings.Join(caps, ",") != want {
		t.Errorf("capabilities --json = %v, want %s", caps, want)
	}

	out.Reset()
	if code := ServeIO(context.Background(), Plugin{MailSend: testPlugin().MailSend}, []string{"capabilities"}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("capabilities exited %d", code)
	}
	if strings.TrimSpace(out.String()) != CapabilityMailSend {
		t.Errorf("capabilities = %q, want only mail.send: a nil handler is not a capability", out.String())
	}

	var errb bytes.Buffer
	if code := ServeIO(context.Background(), testPlugin(), []string{"serve", "--now"}, strings.NewReader(""), &out, &errb); code != ExitCodeValidation {
		t.Errorf("unknown arguments exited %d, want %d", code, ExitCodeValidation)
	}
	if !strings.Contains(errb.String(), "usage:") {
		t.Errorf("unknown arguments print no usage: %q", errb.String())
	}
}

func TestServeIO_Success(t *testing.T) {
	req := mustEnvelope(t, CapabilityMailSend, MailSendPayload{From: "a@x.test", To: []string{"b@x.test"}, Subject: "hi", Body: "b"})
	code, resp, _ := serveOnce(t, testPlugin(), req)
	if code != ExitCodeSuccess || !resp.OK {
		t.Fatalf("exit %d, response %+v", code, resp)
	}
	if resp.RequestID != req.RequestID {
		t.Errorf("request_id %q is not echoed (got %q)", req.RequestID, resp.RequestID)
	}
	if resp.ProviderRequestID != "prov-"+req.RequestID {
		t.Errorf("provider_request_id %q is not lifted from the output", resp.ProviderRequestID)
	}
	out, err := DecodeMailSendOutput(resp.Output)
	if err != nil || !out.Accepted {
		t.Errorf("a nil error must mean accepted: %+v %v", out, err)
	}

	for capability, payload := range map[string]any{
		CapabilityQueuePublish:   QueuePublishPayload{Topic: "orders", Body: json.RawMessage(`{}`)},
		CapabilityWebhookDeliver: WebhookDeliverPayload{URL: "https://x.test"},
		"audit.record":           map[string]string{"plan": "pro"},
	} {
		code, resp, stderr := serveOnce(t, testPlugin(), mustEnvelope(t, capability, payload))
		if code != 0 || !resp.OK {
			t.Errorf("%s: exit %d, response %+v, stderr %q", capability, code, resp, stderr)
		}
	}
}

func TestServeIO_Failures(t *testing.T) {
	cases := []struct {
		name      string
		req       any
		exit      int
		code      string
		retriable bool
	}{
		{"handler refuses", mustEnvelope(t, CapabilityMailSend, MailSendPayload{From: "a", To: []string{"b"}, Subject: "reject"}), ExitCodeRejected, "PROVIDER_REJECTED", false},
		{"handler errors", mustEnvelope(t, CapabilityMailSend, MailSendPayload{From: "a", To: []string{"b"}, Subject: "boom"}), ExitCodeInternal, "INTERNAL", true},
		{"capability not served", mustEnvelope(t, "sms.send", map[string]string{}), ExitCodeValidation, "UNSUPPORTED_CAPABILITY", false},
		{"payload of the wrong shape", mustEnvelope(t, CapabilityMailSend, []int{1}), ExitCodeValidation, "INVALID_PAYLOAD", false},
		{"no payload", RequestEnvelope{Version: EnvelopeVersionV1, RequestID: "r1", Capability: CapabilityMailSend}, ExitCodeValidation, "INVALID_PAYLOAD", false},
		{"another envelope version", RequestEnvelope{Version: "v2", RequestID: "r2", Capability: CapabilityMailSend, Payload: json.RawMessage(`{}`)}, ExitCodeValidation, "UNSUPPORTED_VERSION", false},
		{"not an envelope", "this is not an envelope", ExitCodeValidation, "INVALID_ENVELOPE", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, stderr := serveOnce(t, testPlugin(), tc.req)
			if code != tc.exit {
				t.Errorf("exit %d, want %d", code, tc.exit)
			}
			if resp.OK || resp.Error == nil || resp.Error.Code != tc.code || resp.Retriable != tc.retriable {
				t.Errorf("response %+v (error %+v), want code %s retriable %t", resp, resp.Error, tc.code, tc.retriable)
			}
			if !strings.Contains(stderr, tc.code) {
				t.Errorf("stderr does not name the failure: %q", stderr)
			}
		})
	}
}

func TestServeIO_TimeoutIsExit40(t *testing.T) {
	req := mustEnvelope(t, CapabilityMailSend, MailSendPayload{From: "a", To: []string{"b"}, Subject: "slow"})
	req.TimeoutMS = 20
	code, resp, _ := serveOnce(t, testPlugin(), req)
	if code != ExitCodeTimeout || resp.Error == nil || resp.Error.Code != "DEADLINE_EXCEEDED" || !resp.Retriable {
		t.Fatalf("exit %d, response %+v", code, resp)
	}
}

func TestFail_UnknownExitCodeIsInternal(t *testing.T) {
	if f := Fail(7, "", "x"); f.ExitCode != ExitCodeInternal || f.Code != "PLUGIN_ERROR" || !f.Retriable {
		t.Errorf("Fail(7) = %+v", f)
	}
	if f := Fail(ExitCodeRejected, "NO", "x"); f.Retriable {
		t.Errorf("a rejection is not retriable: %+v", f)
	}
}

// TestServe_ThroughTheHost runs Serve in a real process — this test binary
// re-executed as a plugin — and talks to it with the host side.
func TestServe_ThroughTheHost(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	t.Setenv(serveHelperEnv, "1")

	caps, err := ProbeCapabilities(context.Background(), bin, 10*time.Second)
	if err != nil || len(caps) != 4 {
		t.Fatalf("ProbeCapabilities = %v, %v", caps, err)
	}

	req := mustEnvelope(t, CapabilityMailSend, MailSendPayload{From: "a", To: []string{"b"}, Subject: "hi"})
	resp, err := ExecuteRequest(context.Background(), bin, req, 10*time.Second)
	if err != nil || !resp.OK || resp.RequestID != req.RequestID {
		t.Fatalf("ExecuteRequest = %+v, %v", resp, err)
	}

	req = mustEnvelope(t, CapabilityMailSend, MailSendPayload{From: "a", To: []string{"dev@x.test"}, Subject: "reject"})
	_, err = ExecuteRequest(context.Background(), bin, req, 10*time.Second)
	var execErr *ExecutionError
	if !errors.As(err, &execErr) || execErr.ExitCode != ExitCodeRejected || execErr.Code != "PROVIDER_REJECTED" ||
		execErr.Retriable || !strings.Contains(execErr.Stderr, "dev@x.test is suppressed") {
		t.Fatalf("a refusal through the host: %v", err)
	}
}
