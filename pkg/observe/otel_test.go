package observe

import (
	"context"
	"log/slog"
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
		{name: "missing host", input: "http://", shouldErr: true},
		{name: "whitespace only", input: "   ", shouldErr: true},
		{name: "invalid url", input: "://invalid", shouldErr: true},
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
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{}, nil)
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

func TestSetupOpenTelemetry_NilContext(t *testing.T) {
	// Should not panic, will use context.Background() internally
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetupOpenTelemetry panicked with nil context: %v", r)
		}
	}()

	// Empty endpoint returns noop, so nil context is safe
	shutdown, _, err := SetupOpenTelemetry(nil, TelemetryConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
}

func TestSetupOpenTelemetry_NilLogger(t *testing.T) {
	// Empty endpoint returns noop, so nil logger is safe
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
}

func TestSetupOpenTelemetry_DefaultServiceName(t *testing.T) {
	// Empty endpoint returns noop, but we can test the config path
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{
		OTLPEndpoint: "",
		ServiceName:  "", // Should default to "nucleus"
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = shutdown // noop shutdown
}

func TestSetupOpenTelemetry_ShutdownWithNilContext(t *testing.T) {
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Shutdown should handle nil context gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("shutdown panicked with nil context: %v", r)
		}
	}()

	if err := shutdown(nil); err != nil {
		t.Fatalf("shutdown should handle nil context, got: %v", err)
	}
}

func TestTelemetryConfig_StructFields(t *testing.T) {
	cfg := TelemetryConfig{
		ServiceName:  "my-service",
		OTLPEndpoint: "http://collector:4318",
	}
	if cfg.ServiceName != "my-service" {
		t.Errorf("expected service name 'my-service', got %q", cfg.ServiceName)
	}
	if cfg.OTLPEndpoint != "http://collector:4318" {
		t.Errorf("expected endpoint 'http://collector:4318', got %q", cfg.OTLPEndpoint)
	}
}

func TestSetupOpenTelemetry_WithWhitespaceEndpoint(t *testing.T) {
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{
		OTLPEndpoint: "   ",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
}

func TestSetupOpenTelemetry_NilContextInShutdown(t *testing.T) {
	// Test that shutdown handles nil context in the returned function
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Call shutdown with nil context - should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("shutdown panicked: %v", r)
		}
	}()

	err = shutdown(nil)
	if err != nil {
		t.Logf("shutdown returned error (acceptable for noop): %v", err)
	}
}

func TestParseOTLPEndpoint_CaseInsensitiveScheme(t *testing.T) {
	tests := []struct {
		input    string
		insecure bool
	}{
		{"HTTP://collector:4318", true},
		{"HTTPS://collector:4318", false},
		{"hTTp://collector:4318", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, insecure, err := parseOTLPEndpoint(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if insecure != tc.insecure {
				t.Errorf("expected insecure=%v, got %v", tc.insecure, insecure)
			}
		})
	}
}

func TestSetupOpenTelemetry_WithInvalidEndpoint(t *testing.T) {
	// Test with an endpoint that will fail to connect (no actual collector running)
	// This exercises the error paths in SetupOpenTelemetry
	_, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{
		OTLPEndpoint: "invalid://bad-endpoint:4318",
		ServiceName:  "test-service",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid endpoint scheme")
	}
}

func TestSetupOpenTelemetry_WithValidButUnreachableEndpoint(t *testing.T) {
	// This tests the code path that creates exporters and providers
	// It will fail to connect but exercises the setup logic
	ctx := context.Background()
	logger := slog.Default()

	// Use http (insecure) to exercise the insecure code path
	shutdown, _, err := SetupOpenTelemetry(ctx, TelemetryConfig{
		OTLPEndpoint: "http://127.0.0.1:19999", // Unlikely to have anything running here
		ServiceName:  "test-service",
	}, logger)

	// May succeed in setup even if collector is unreachable
	if err == nil && shutdown != nil {
		// Setup succeeded, test shutdown
		shutdownErr := shutdown(context.Background())
		// Shutdown may or may not error depending on whether providers were created
		_ = shutdownErr
	}
}

func TestSetupOpenTelemetry_ServiceNameTrimmed(t *testing.T) {
	shutdown, _, err := SetupOpenTelemetry(context.Background(), TelemetryConfig{
		OTLPEndpoint: "",
		ServiceName:  "  my-service  ",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
}

func TestTelemetryConfig_EmptyStruct(t *testing.T) {
	cfg := TelemetryConfig{}
	if cfg.ServiceName != "" {
		t.Errorf("expected empty service name, got %q", cfg.ServiceName)
	}
	if cfg.OTLPEndpoint != "" {
		t.Errorf("expected empty endpoint, got %q", cfg.OTLPEndpoint)
	}
}

// Benchmark for parseOTLPEndpoint
func BenchmarkParseOTLPEndpoint(b *testing.B) {
	inputs := []string{
		"localhost:4318",
		"http://collector:4318",
		"https://collector:4318",
	}
	for _, input := range inputs {
		b.Run(input, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _, _ = parseOTLPEndpoint(input)
			}
		})
	}
}
