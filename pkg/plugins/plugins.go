package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/mail"
)

const (
	GenericBinaryPrefix    = "goframe-plugin-"
	LegacyMailBinaryPrefix = "goframe-mail-"
	DefaultProbeTimeout    = 2 * time.Second
	mailSendCapability     = "mail.send"
)

type Source string

const (
	SourceBuiltinMail        Source = "builtin_mail"
	SourceExternalGeneric    Source = "external_generic"
	SourceExternalLegacyMail Source = "external_legacy_mail"
)

type Descriptor struct {
	Provider     string   `json:"provider"`
	Capabilities []string `json:"capabilities"`
	Source       Source   `json:"source"`
	BinaryPath   string   `json:"binary_path,omitempty"`
	ProbeError   string   `json:"probe_error,omitempty"`
}

func BuiltinMailDescriptors() []Descriptor {
	registered := mail.RegisteredProviders()
	seen := make(map[string]struct{}, len(registered))
	out := make([]Descriptor, 0, len(registered))

	for _, driver := range registered {
		normalized := normalizeToken(driver)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, Descriptor{
			Provider:     normalized,
			Capabilities: []string{mailSendCapability},
			Source:       SourceBuiltinMail,
		})
	}

	sortDescriptors(out)
	return out
}

func DiscoverExternal(pathEnv string, probeTimeout time.Duration) []Descriptor {
	if strings.TrimSpace(pathEnv) == "" {
		return []Descriptor{}
	}
	if probeTimeout <= 0 {
		probeTimeout = DefaultProbeTimeout
	}

	seen := map[string]struct{}{}
	out := make([]Descriptor, 0, 8)

	for _, dir := range filepath.SplitList(pathEnv) {
		trimmedDir := strings.TrimSpace(dir)
		if trimmedDir == "" {
			continue
		}

		entries, err := os.ReadDir(trimmedDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			var (
				provider string
				source   Source
				ok       bool
			)

			switch {
			case strings.HasPrefix(entry.Name(), GenericBinaryPrefix):
				provider, ok = ParseProviderFromBinary(entry.Name(), GenericBinaryPrefix)
				source = SourceExternalGeneric
			case strings.HasPrefix(entry.Name(), LegacyMailBinaryPrefix):
				provider, ok = ParseProviderFromBinary(entry.Name(), LegacyMailBinaryPrefix)
				source = SourceExternalLegacyMail
			}
			if !ok || provider == "" {
				continue
			}

			fullPath := filepath.Join(trimmedDir, entry.Name())
			executable, err := IsExecutableFile(fullPath, entry)
			if err != nil || !executable {
				continue
			}

			key := string(source) + "|" + provider
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}

			desc := Descriptor{
				Provider:   provider,
				Source:     source,
				BinaryPath: fullPath,
			}

			switch source {
			case SourceExternalLegacyMail:
				desc.Capabilities = []string{mailSendCapability}
			case SourceExternalGeneric:
				caps, err := ProbeCapabilities(context.Background(), fullPath, probeTimeout)
				if err != nil {
					desc.ProbeError = err.Error()
				}
				desc.Capabilities = caps
			}

			out = append(out, desc)
		}
	}

	sortDescriptors(out)
	return out
}

func CollectInventory(pathEnv string, probeTimeout time.Duration) []Descriptor {
	out := BuiltinMailDescriptors()
	out = append(out, DiscoverExternal(pathEnv, probeTimeout)...)
	sortDescriptors(out)
	return out
}

func ProbeCapabilities(ctx context.Context, binaryPath string, timeout time.Duration) ([]string, error) {
	trimmedPath := strings.TrimSpace(binaryPath)
	if trimmedPath == "" {
		return nil, fmt.Errorf("binary path cannot be empty")
	}
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}

	jsonOutput, jsonErr := runProbe(ctx, trimmedPath, timeout, "capabilities", "--json")
	if jsonErr == nil {
		caps, err := parseCapabilitiesJSON(jsonOutput)
		if err == nil && len(caps) > 0 {
			return caps, nil
		}
		if caps := parseCapabilitiesText(jsonOutput); len(caps) > 0 {
			return caps, nil
		}
		if err == nil {
			jsonErr = fmt.Errorf("capabilities --json returned no capabilities")
		} else {
			jsonErr = fmt.Errorf("parse capabilities --json output: %w", err)
		}
	}

	plainOutput, plainErr := runProbe(ctx, trimmedPath, timeout, "capabilities")
	if plainErr != nil {
		if jsonErr != nil {
			return nil, fmt.Errorf("%v; fallback failed: %w", jsonErr, plainErr)
		}
		return nil, plainErr
	}
	caps := parseCapabilitiesText(plainOutput)
	if len(caps) == 0 {
		if jsonErr != nil {
			return nil, fmt.Errorf("%v; fallback capabilities output is empty", jsonErr)
		}
		return nil, fmt.Errorf("capabilities output is empty")
	}
	return caps, nil
}

func SupportsCapability(desc Descriptor, capability string) bool {
	target := normalizeToken(capability)
	if target == "" {
		return false
	}
	for _, cap := range desc.Capabilities {
		if normalizeToken(cap) == target {
			return true
		}
	}
	return false
}

func ParseProviderFromBinary(name, prefix string) (string, bool) {
	provider := strings.TrimPrefix(name, prefix)
	if provider == name {
		return "", false
	}
	provider = strings.TrimSpace(provider)
	if runtime.GOOS == "windows" {
		provider = strings.TrimSuffix(provider, ".exe")
	}
	provider = normalizeToken(provider)
	if provider == "" {
		return "", false
	}
	if strings.ContainsAny(provider, `/\`) {
		return "", false
	}
	return provider, true
}

func IsExecutableFile(path string, entry os.DirEntry) (bool, error) {
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Ext(path), ".exe"), nil
	}
	return info.Mode().Perm()&0o111 != 0, nil
}

func runProbe(ctx context.Context, binaryPath string, timeout time.Duration, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, binaryPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return strings.TrimSpace(stdout.String()), fmt.Errorf("%w (%s)", err, stderrText)
		}
		return strings.TrimSpace(stdout.String()), err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func parseCapabilitiesJSON(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty JSON output")
	}

	var objectPayload struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(trimmed), &objectPayload); err == nil {
		if len(objectPayload.Capabilities) > 0 {
			return normalizeCapabilities(objectPayload.Capabilities), nil
		}
	}

	var listPayload []string
	if err := json.Unmarshal([]byte(trimmed), &listPayload); err == nil {
		return normalizeCapabilities(listPayload), nil
	}

	var generic map[string]any
	if err := json.Unmarshal([]byte(trimmed), &generic); err != nil {
		return nil, err
	}
	if caps := extractCapabilities(generic); len(caps) > 0 {
		return normalizeCapabilities(caps), nil
	}
	return nil, fmt.Errorf("capabilities not found in JSON payload")
}

func extractCapabilities(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		if rawCaps, ok := typed["capabilities"]; ok {
			return toStringSlice(rawCaps)
		}
		if output, ok := typed["output"]; ok {
			return extractCapabilities(output)
		}
	case []any:
		return toStringSlice(typed)
	}
	return nil
}

func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func parseCapabilitiesText(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case '\n', '\r', '\t', ' ', ',', ';':
			return true
		default:
			return false
		}
	})
	return normalizeCapabilities(tokens)
}

func normalizeCapabilities(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeToken(value)
		if normalized == "" {
			continue
		}
		if !strings.Contains(normalized, ".") {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortDescriptors(descriptors []Descriptor) {
	sort.Slice(descriptors, func(i, j int) bool {
		left := descriptors[i]
		right := descriptors[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.Source != right.Source {
			return sourceSortOrder(left.Source) < sourceSortOrder(right.Source)
		}
		return left.BinaryPath < right.BinaryPath
	})
}

func sourceSortOrder(source Source) int {
	switch source {
	case SourceExternalGeneric:
		return 0
	case SourceExternalLegacyMail:
		return 1
	case SourceBuiltinMail:
		return 2
	default:
		return 3
	}
}
