package observe

import (
	"context"
	"testing"
)

func TestParseOTLPEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		endpoint  string
		insecure  bool
		shouldErr bool
	}{
		{name: "hostport", input: "localhost:4318", endpoint: "localhost:4318", insecure: true},
		{name: "http url", input: "http://collector:4318", endpoint: "collector:4318", insecure: true},
		{name: "https url", input: "https://collector:4318", endpoint: "collector:4318", insecure: false},
		{name: "invalid scheme", input: "grpc://collector:4317", shouldErr: true},
		{name: "empty", input: "", shouldErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, insecure, err := parseOTLPEndpoint(tc.input)
			if tc.shouldErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if endpoint != tc.endpoint {
				t.Fatalf("expected endpoint %q, got %q", tc.endpoint, endpoint)
			}
			if insecure != tc.insecure {
				t.Fatalf("expected insecure=%v, got %v", tc.insecure, insecure)
			}
		})
	}
}

func TestSetupOpenTelemetry_EmptyEndpointIsNoop(t *testing.T) {
	shutdown, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should be nil error, got %v", err)
	}
}
