package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/mail"
)

type mailProviderInfo struct {
	Driver     string `json:"driver"`
	Registered bool   `json:"registered"`
	Active     bool   `json:"active"`
}

type mailProvidersReport struct {
	ActiveDriver string             `json:"active_driver"`
	Providers    []mailProviderInfo `json:"providers"`
}

func runMailProviders(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mailproviders", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
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

	entries := collectMailProviderInfos(activeDriver)

	report := mailProvidersReport{
		ActiveDriver: activeDriver,
		Providers:    entries,
	}

	if outputWantsJSON(*asJSON) {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if outputIsPretty() {
		fmt.Fprintf(stdout, "Mail providers (active: %s)\n", report.ActiveDriver)
		if len(report.Providers) == 0 {
			fmt.Fprintln(stdout, "  none")
			return nil
		}
		for _, provider := range report.Providers {
			state := "info"
			if provider.Active {
				state = "ok"
			}
			fmt.Fprintf(stdout, "  %s  %s", statusTag(stdout, state), provider.Driver)
			if provider.Registered {
				fmt.Fprint(stdout, " [registered]")
			}
			fmt.Fprintln(stdout)
		}
		return nil
	}

	fmt.Fprintf(stdout, "Active driver: %s\n", report.ActiveDriver)
	if len(report.Providers) == 0 {
		fmt.Fprintln(stdout, "No mail providers detected")
		return nil
	}

	fmt.Fprintln(stdout, "driver\tregistered\tactive")
	for _, provider := range report.Providers {
		fmt.Fprintf(stdout, "%s\t%t\t%t\n", provider.Driver, provider.Registered, provider.Active)
	}
	return nil
}

func collectMailProviderInfos(activeDriver string) []mailProviderInfo {
	registered := mail.RegisteredProviders()

	byDriver := make(map[string]mailProviderInfo, len(registered)+1)
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
	return out
}
