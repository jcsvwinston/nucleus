package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jcsvwinston/GoFrame/pkg/mail"
	"github.com/jcsvwinston/GoFrame/pkg/plugins"
)

type mailProviderInfo struct {
	Driver       string `json:"driver"`
	Registered   bool   `json:"registered"`
	ExternalPath string `json:"external_path,omitempty"`
	Active       bool   `json:"active"`
}

type mailProvidersReport struct {
	ActiveDriver string             `json:"active_driver"`
	Providers    []mailProviderInfo `json:"providers"`
}

func runMailProviders(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mailproviders", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("mailproviders does not accept positional arguments")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	activeDriver := resolveMailDriver(cfg.MailDriver)

	entries, err := collectMailProviderInfos(activeDriver)
	if err != nil {
		return err
	}

	report := mailProvidersReport{
		ActiveDriver: activeDriver,
		Providers:    entries,
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	fmt.Fprintf(stdout, "Active driver: %s\n", report.ActiveDriver)
	if len(report.Providers) == 0 {
		fmt.Fprintln(stdout, "No mail providers detected")
		return nil
	}

	fmt.Fprintln(stdout, "driver\tregistered\texternal\tactive")
	for _, provider := range report.Providers {
		external := "-"
		if provider.ExternalPath != "" {
			external = provider.ExternalPath
		}
		fmt.Fprintf(stdout, "%s\t%t\t%s\t%t\n", provider.Driver, provider.Registered, external, provider.Active)
	}
	return nil
}

func collectMailProviderInfos(activeDriver string) ([]mailProviderInfo, error) {
	registered := mail.RegisteredProviders()
	external, err := discoverExternalMailPlugins()
	if err != nil {
		return nil, err
	}

	byDriver := make(map[string]mailProviderInfo, len(registered)+len(external)+1)
	for _, driver := range registered {
		normalized := strings.ToLower(strings.TrimSpace(driver))
		if normalized == "" {
			continue
		}
		byDriver[normalized] = mailProviderInfo{
			Driver:     normalized,
			Registered: true,
		}
	}

	for driver, path := range external {
		info := byDriver[driver]
		info.Driver = driver
		info.ExternalPath = path
		byDriver[driver] = info
	}

	if activeDriver != "" {
		info := byDriver[activeDriver]
		info.Driver = activeDriver
		info.Active = true
		byDriver[activeDriver] = info
	}

	out := make([]mailProviderInfo, 0, len(byDriver))
	for _, info := range byDriver {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Driver < out[j].Driver
	})
	return out, nil
}

func discoverExternalMailPlugins() (map[string]string, error) {
	pathEnv := os.Getenv("PATH")
	if strings.TrimSpace(pathEnv) == "" {
		return map[string]string{}, nil
	}

	found := make(map[string]string)
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
			name := entry.Name()
			if !strings.HasPrefix(name, plugins.LegacyMailBinaryPrefix) {
				continue
			}

			driver, ok := parseMailPluginDriver(name)
			if !ok {
				continue
			}
			if _, exists := found[driver]; exists {
				continue
			}

			fullPath := filepath.Join(trimmedDir, name)
			available, err := plugins.IsExecutableFile(fullPath, entry)
			if err != nil || !available {
				continue
			}
			found[driver] = fullPath
		}
	}
	return found, nil
}

func parseMailPluginDriver(name string) (string, bool) {
	return plugins.ParseProviderFromBinary(name, plugins.LegacyMailBinaryPrefix)
}
