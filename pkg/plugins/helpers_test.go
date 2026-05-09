package plugins

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestNormalizeToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  Test  ", "test"},
		{"TEST", "test"},
		{"TeSt", "test"},
		{"", ""},
		{"  ", ""},
		{"  Mixed Case  ", "mixed case"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeToken(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSupportsCapability(t *testing.T) {
	t.Run("capability exists", func(t *testing.T) {
		desc := Descriptor{
			Capabilities: []string{"mail.send", "queue.publish"},
		}
		if !SupportsCapability(desc, "mail.send") {
			t.Error("Expected true for existing capability")
		}
		if !SupportsCapability(desc, "  Mail.Send  ") {
			t.Error("Expected true for capability with whitespace")
		}
		if !SupportsCapability(desc, "MAIL.SEND") {
			t.Error("Expected true for uppercase capability")
		}
	})

	t.Run("capability does not exist", func(t *testing.T) {
		desc := Descriptor{
			Capabilities: []string{"mail.send"},
		}
		if SupportsCapability(desc, "queue.publish") {
			t.Error("Expected false for non-existing capability")
		}
	})

	t.Run("empty capability", func(t *testing.T) {
		desc := Descriptor{
			Capabilities: []string{"mail.send"},
		}
		if SupportsCapability(desc, "") {
			t.Error("Expected false for empty capability")
		}
	})

	t.Run("empty descriptor capabilities", func(t *testing.T) {
		desc := Descriptor{
			Capabilities: []string{},
		}
		if SupportsCapability(desc, "mail.send") {
			t.Error("Expected false for empty capabilities")
		}
	})
}

func TestParseCapabilitiesText(t *testing.T) {
	t.Run("comma separated", func(t *testing.T) {
		result := parseCapabilitiesText("mail.send, queue.publish, webhook.deliver")
		if len(result) != 3 {
			t.Errorf("Expected 3 capabilities, got %d", len(result))
		}
	})

	t.Run("newline separated", func(t *testing.T) {
		result := parseCapabilitiesText("mail.send\nqueue.publish\nwebhook.deliver")
		if len(result) != 3 {
			t.Errorf("Expected 3 capabilities, got %d", len(result))
		}
	})

	t.Run("mixed separators", func(t *testing.T) {
		result := parseCapabilitiesText("mail.send, queue.publish; webhook.deliver")
		if len(result) != 3 {
			t.Errorf("Expected 3 capabilities, got %d", len(result))
		}
	})

	t.Run("empty string", func(t *testing.T) {
		result := parseCapabilitiesText("")
		if result != nil {
			t.Errorf("Expected nil for empty string, got %v", result)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		result := parseCapabilitiesText("   \n\t  ")
		if result != nil {
			t.Errorf("Expected nil for whitespace only, got %v", result)
		}
	})

	t.Run("invalid capabilities (no dots)", func(t *testing.T) {
		result := parseCapabilitiesText("mailsend queuepublish")
		// Should filter out capabilities without dots
		if len(result) != 0 {
			t.Errorf("Expected 0 valid capabilities, got %d", len(result))
		}
	})
}

func TestParseCapabilitiesJSON(t *testing.T) {
	t.Run("object format", func(t *testing.T) {
		raw := `{"capabilities":["mail.send","queue.publish"]}`
		result, err := parseCapabilitiesJSON(raw)
		if err != nil {
			t.Fatalf("parseCapabilitiesJSON failed: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities, got %d", len(result))
		}
	})

	t.Run("array format", func(t *testing.T) {
		raw := `["mail.send","queue.publish"]`
		result, err := parseCapabilitiesJSON(raw)
		if err != nil {
			t.Fatalf("parseCapabilitiesJSON failed: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities, got %d", len(result))
		}
	})

	t.Run("nested format", func(t *testing.T) {
		raw := `{"output":{"capabilities":["mail.send"]}}`
		result, err := parseCapabilitiesJSON(raw)
		if err != nil {
			t.Fatalf("parseCapabilitiesJSON failed: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("Expected 1 capability, got %d", len(result))
		}
	})

	t.Run("empty JSON", func(t *testing.T) {
		_, err := parseCapabilitiesJSON("")
		if err == nil {
			t.Error("Expected error for empty JSON")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := parseCapabilitiesJSON("{invalid}")
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("no capabilities field", func(t *testing.T) {
		_, err := parseCapabilitiesJSON(`{"other":"data"}`)
		if err == nil {
			t.Error("Expected error when no capabilities field")
		}
	})
}

func TestExtractCapabilities(t *testing.T) {
	t.Run("map with capabilities key", func(t *testing.T) {
		value := map[string]any{"capabilities": []any{"mail.send", "queue.publish"}}
		result := extractCapabilities(value)
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities, got %d", len(result))
		}
	})

	t.Run("map with output key", func(t *testing.T) {
		value := map[string]any{"output": map[string]any{"capabilities": []any{"mail.send"}}}
		result := extractCapabilities(value)
		if len(result) != 1 {
			t.Errorf("Expected 1 capability, got %d", len(result))
		}
	})

	t.Run("array of strings", func(t *testing.T) {
		value := []any{"mail.send", "queue.publish"}
		result := extractCapabilities(value)
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities, got %d", len(result))
		}
	})

	t.Run("array of mixed types", func(t *testing.T) {
		value := []any{"mail.send", 123, true}
		result := extractCapabilities(value)
		if len(result) != 1 {
			t.Errorf("Expected 1 capability (filtered non-strings), got %d", len(result))
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		value := "string"
		result := extractCapabilities(value)
		if result != nil {
			t.Errorf("Expected nil for invalid type, got %v", result)
		}
	})

	t.Run("nil", func(t *testing.T) {
		result := extractCapabilities(nil)
		if result != nil {
			t.Errorf("Expected nil for nil input, got %v", result)
		}
	})
}

func TestToStringSlice(t *testing.T) {
	t.Run("[]string", func(t *testing.T) {
		value := []string{"mail.send", "queue.publish"}
		result := toStringSlice(value)
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}
	})

	t.Run("[]any with strings", func(t *testing.T) {
		value := []any{"mail.send", "queue.publish"}
		result := toStringSlice(value)
		if len(result) != 2 {
			t.Errorf("Expected 2 items, got %d", len(result))
		}
	})

	t.Run("[]any with mixed types", func(t *testing.T) {
		value := []any{"mail.send", 123, true}
		result := toStringSlice(value)
		if len(result) != 1 {
			t.Errorf("Expected 1 item (filtered non-strings), got %d", len(result))
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		result := toStringSlice("string")
		if result != nil {
			t.Errorf("Expected nil for invalid type, got %v", result)
		}
	})
}

func TestNormalizeCapabilities(t *testing.T) {
	t.Run("valid capabilities", func(t *testing.T) {
		input := []string{"mail.send", "queue.publish", "WEBHOOK.DELIVER"}
		result := normalizeCapabilities(input)
		if len(result) != 3 {
			t.Errorf("Expected 3 capabilities, got %d", len(result))
		}
		// Should be sorted
		if result[0] != "mail.send" {
			t.Errorf("Expected first item to be mail.send, got %s", result[0])
		}
	})

	t.Run("duplicates removed", func(t *testing.T) {
		input := []string{"mail.send", "MAIL.SEND", "queue.publish"}
		result := normalizeCapabilities(input)
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities (duplicates removed), got %d", len(result))
		}
	})

	t.Run("invalid capabilities filtered", func(t *testing.T) {
		input := []string{"mail.send", "invalid", "queue.publish"}
		result := normalizeCapabilities(input)
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities (invalid filtered), got %d", len(result))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result := normalizeCapabilities([]string{})
		if result != nil {
			t.Errorf("Expected nil for empty input, got %v", result)
		}
	})

	t.Run("all invalid", func(t *testing.T) {
		input := []string{"invalid", "another"}
		result := normalizeCapabilities(input)
		if len(result) != 0 {
			t.Errorf("Expected empty slice for all invalid, got %v", result)
		}
	})

	t.Run("empty strings filtered", func(t *testing.T) {
		input := []string{"mail.send", "", "queue.publish"}
		result := normalizeCapabilities(input)
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities (empty filtered), got %d", len(result))
		}
	})

	t.Run("whitespace only filtered", func(t *testing.T) {
		input := []string{"mail.send", "   ", "queue.publish"}
		result := normalizeCapabilities(input)
		if len(result) != 2 {
			t.Errorf("Expected 2 capabilities (whitespace filtered), got %d", len(result))
		}
	})
}

func TestSortDescriptors(t *testing.T) {
	t.Run("sort by provider", func(t *testing.T) {
		descriptors := []Descriptor{
			{Provider: "zebra", Source: SourceExternalGeneric},
			{Provider: "alpha", Source: SourceExternalGeneric},
		}
		sortDescriptors(descriptors)
		if descriptors[0].Provider != "alpha" {
			t.Errorf("Expected alpha first, got %s", descriptors[0].Provider)
		}
	})

	t.Run("sort by source when provider equal", func(t *testing.T) {
		descriptors := []Descriptor{
			{Provider: "test", Source: SourceBuiltinMail},
			{Provider: "test", Source: SourceExternalGeneric},
		}
		sortDescriptors(descriptors)
		if descriptors[0].Source != SourceExternalGeneric {
			t.Errorf("Expected SourceExternalGeneric first, got %s", descriptors[0].Source)
		}
	})

	t.Run("sort by binary path when provider and source equal", func(t *testing.T) {
		descriptors := []Descriptor{
			{Provider: "test", Source: SourceExternalGeneric, BinaryPath: "/path/b"},
			{Provider: "test", Source: SourceExternalGeneric, BinaryPath: "/path/a"},
		}
		sortDescriptors(descriptors)
		if descriptors[0].BinaryPath != "/path/a" {
			t.Errorf("Expected /path/a first, got %s", descriptors[0].BinaryPath)
		}
	})
}

func TestSourceSortOrder(t *testing.T) {
	tests := []struct {
		source   Source
		expected int
	}{
		{SourceExternalGeneric, 0},
		{SourceBuiltinMail, 1},
		{Source("unknown"), 2},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			result := sourceSortOrder(tt.source)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestDiscoverExternalEdgeCases(t *testing.T) {
	t.Run("empty path env", func(t *testing.T) {
		result := DiscoverExternal("", DefaultProbeTimeout)
		if len(result) != 0 {
			t.Errorf("Expected empty result for empty path env, got %d", len(result))
		}
	})

	t.Run("whitespace only path env", func(t *testing.T) {
		result := DiscoverExternal("   ", DefaultProbeTimeout)
		if len(result) != 0 {
			t.Errorf("Expected empty result for whitespace path env, got %d", len(result))
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		result := DiscoverExternal("/nonexistent/path", DefaultProbeTimeout)
		if len(result) != 0 {
			t.Errorf("Expected empty result for non-existent directory, got %d", len(result))
		}
	})

	t.Run("zero timeout uses default", func(t *testing.T) {
		result := DiscoverExternal("/nonexistent", 0)
		// Should not panic
		if result == nil {
			t.Error("Expected non-nil result")
		}
	})
}

func TestPluginConstants(t *testing.T) {
	if GenericBinaryPrefix != "nucleus-plugin-" {
		t.Errorf("Expected GenericBinaryPrefix=nucleus-plugin-, got %s", GenericBinaryPrefix)
	}
	if DefaultProbeTimeout != 2*time.Second {
		t.Errorf("Expected DefaultProbeTimeout=2s, got %v", DefaultProbeTimeout)
	}
}

func TestIsExecutableFile(t *testing.T) {
	t.Run("windows .exe", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("windows-specific test")
		}

		entry := &mockDirEntry{name: "test.exe", isDir: false}
		executable, err := IsExecutableFile("/path/test.exe", entry)
		if err != nil {
			t.Fatalf("IsExecutableFile failed: %v", err)
		}
		if !executable {
			t.Error("Expected .exe to be executable on Windows")
		}
	})

	t.Run("windows non-exe", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("windows-specific test")
		}

		entry := &mockDirEntry{name: "test.txt", isDir: false}
		executable, err := IsExecutableFile("/path/test.txt", entry)
		if err != nil {
			t.Fatalf("IsExecutableFile failed: %v", err)
		}
		if executable {
			t.Error("Expected .txt to not be executable on Windows")
		}
	})

	t.Run("unix executable", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix-specific test")
		}

		entry := &mockDirEntry{name: "test", isDir: false, mode: 0o755}
		executable, err := IsExecutableFile("/path/test", entry)
		if err != nil {
			t.Fatalf("IsExecutableFile failed: %v", err)
		}
		if !executable {
			t.Error("Expected executable with mode 0755")
		}
	})

	t.Run("unix non-executable", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix-specific test")
		}

		entry := &mockDirEntry{name: "test", isDir: false, mode: 0o644}
		executable, err := IsExecutableFile("/path/test", entry)
		if err != nil {
			t.Fatalf("IsExecutableFile failed: %v", err)
		}
		if executable {
			t.Error("Expected non-executable with mode 0644")
		}
	})

	t.Run("directory", func(t *testing.T) {
		entry := &mockDirEntry{name: "test", isDir: true}
		executable, err := IsExecutableFile("/path/test", entry)
		if err != nil {
			t.Fatalf("IsExecutableFile failed: %v", err)
		}
		if executable {
			t.Error("Expected directory to not be executable")
		}
	})
}

func TestParseProviderFromBinaryEdgeCases(t *testing.T) {
	t.Run("no prefix match", func(t *testing.T) {
		provider, ok := ParseProviderFromBinary("test-binary", GenericBinaryPrefix)
		if ok {
			t.Error("Expected false for no prefix match")
		}
		if provider != "" {
			t.Errorf("Expected empty provider, got %s", provider)
		}
	})

	t.Run("empty after prefix", func(t *testing.T) {
		provider, ok := ParseProviderFromBinary("nucleus-plugin-", GenericBinaryPrefix)
		if ok {
			t.Error("Expected false for empty provider")
		}
		if provider != "" {
			t.Errorf("Expected empty provider, got %s", provider)
		}
	})

	t.Run("whitespace after prefix", func(t *testing.T) {
		provider, ok := ParseProviderFromBinary("nucleus-plugin-  ", GenericBinaryPrefix)
		if ok {
			t.Error("Expected false for whitespace-only provider")
		}
		if provider != "" {
			t.Errorf("Expected empty provider, got %s", provider)
		}
	})

	t.Run("windows .exe suffix", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("windows-specific test")
		}
		provider, ok := ParseProviderFromBinary("nucleus-plugin-test.exe", GenericBinaryPrefix)
		if !ok {
			t.Error("Expected true for windows .exe")
		}
		if provider != "test" {
			t.Errorf("Expected test, got %s", provider)
		}
	})

	t.Run("contains path separator", func(t *testing.T) {
		provider, ok := ParseProviderFromBinary("nucleus-plugin-test/sub", GenericBinaryPrefix)
		if ok {
			t.Error("Expected false for provider with path separator")
		}
		if provider != "" {
			t.Errorf("Expected empty provider, got %s", provider)
		}
	})

	t.Run("contains backslash", func(t *testing.T) {
		provider, ok := ParseProviderFromBinary("nucleus-plugin-test\\sub", GenericBinaryPrefix)
		if ok {
			t.Error("Expected false for provider with backslash")
		}
		if provider != "" {
			t.Errorf("Expected empty provider, got %s", provider)
		}
	})
}

func TestBuiltinMailDescriptorsFromProvidersEdgeCases(t *testing.T) {
	t.Run("empty providers", func(t *testing.T) {
		descriptors := BuiltinMailDescriptorsFromProviders([]string{})
		if len(descriptors) != 0 {
			t.Errorf("Expected 0 descriptors, got %d", len(descriptors))
		}
	})

	t.Run("duplicate providers", func(t *testing.T) {
		descriptors := BuiltinMailDescriptorsFromProviders([]string{"noop", "noop", "SMTP", "smtp"})
		if len(descriptors) != 2 {
			t.Errorf("Expected 2 descriptors (deduplicated), got %d", len(descriptors))
		}
	})

	t.Run("empty provider name", func(t *testing.T) {
		descriptors := BuiltinMailDescriptorsFromProviders([]string{"", "noop"})
		if len(descriptors) != 1 {
			t.Errorf("Expected 1 descriptor (empty filtered), got %d", len(descriptors))
		}
	})

	t.Run("whitespace provider", func(t *testing.T) {
		descriptors := BuiltinMailDescriptorsFromProviders([]string{"  ", "noop"})
		if len(descriptors) != 1 {
			t.Errorf("Expected 1 descriptor (whitespace filtered), got %d", len(descriptors))
		}
	})
}

func TestProbeCapabilitiesEdgeCases(t *testing.T) {
	t.Run("empty binary path", func(t *testing.T) {
		_, err := ProbeCapabilities(context.Background(), "", DefaultProbeTimeout)
		if err == nil {
			t.Error("Expected error for empty binary path")
		}
	})

	t.Run("whitespace binary path", func(t *testing.T) {
		_, err := ProbeCapabilities(context.Background(), "   ", DefaultProbeTimeout)
		if err == nil {
			t.Error("Expected error for whitespace binary path")
		}
	})

	t.Run("zero timeout uses default", func(t *testing.T) {
		// This will fail with "no such file" but should not fail on timeout validation
		_, err := ProbeCapabilities(context.Background(), "/nonexistent", 0)
		if err == nil {
			t.Error("Expected error for nonexistent binary")
		}
	})

	t.Run("negative timeout uses default", func(t *testing.T) {
		_, err := ProbeCapabilities(context.Background(), "/nonexistent", -1)
		if err == nil {
			t.Error("Expected error for nonexistent binary")
		}
	})
}

// mockDirEntry is a mock implementation of os.DirEntry for testing
type mockDirEntry struct {
	name  string
	isDir bool
	mode  os.FileMode
}

func (m *mockDirEntry) Name() string {
	return m.name
}

func (m *mockDirEntry) IsDir() bool {
	return m.isDir
}

func (m *mockDirEntry) Type() os.FileMode {
	return m.mode
}

func (m *mockDirEntry) Info() (os.FileInfo, error) {
	return &mockFileInfo{mode: m.mode}, nil
}

// mockFileInfo is a mock implementation of os.FileInfo for testing
type mockFileInfo struct {
	mode os.FileMode
}

func (m *mockFileInfo) Name() string       { return "test" }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() any           { return nil }
