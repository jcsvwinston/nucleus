package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

const pluginMailCapability = plugins.CapabilityMailSend

type pluginListItem struct {
	Provider     string         `json:"provider"`
	Capabilities []string       `json:"capabilities"`
	Source       plugins.Source `json:"source"`
	BinaryPath   string         `json:"binary_path,omitempty"`
	ActiveMail   bool           `json:"active_mail"`
	ProbeError   string         `json:"probe_error,omitempty"`
}

type pluginListReport struct {
	ActiveMailDriver string           `json:"active_mail_driver"`
	Providers        []pluginListItem `json:"providers"`
}

type pluginDoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

type pluginDoctorReport struct {
	Status    string              `json:"status"`
	CheckedAt string              `json:"checked_at"`
	Checks    []pluginDoctorCheck `json:"checks"`
}

type pluginTestReport struct {
	Provider   string         `json:"provider"`
	Capability string         `json:"capability"`
	Mode       string         `json:"mode"`
	Status     string         `json:"status"`
	Source     plugins.Source `json:"source,omitempty"`
	BinaryPath string         `json:"binary_path,omitempty"`
	CheckedAt  string         `json:"checked_at"`
	Details    string         `json:"details,omitempty"`
}

func runPlugin(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printPluginUsage(stdout)
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "help", "-h", "--help":
		printPluginUsage(stdout)
		return nil
	case "list":
		return runPluginList(args[1:], stdout, stderr)
	case "doctor":
		return runPluginDoctor(args[1:], stdout, stderr)
	case "test":
		return runPluginTest(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown plugin subcommand %q", args[0])
	}
}

func printPluginUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  nucleus plugin <list|doctor|test> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  list      List discovered providers and capabilities")
	fmt.Fprintln(w, "  doctor    Validate plugin runtime and configuration wiring")
	fmt.Fprintln(w, "  test      Run provider/capability smoke checks")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  nucleus plugin list --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin doctor --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin test --provider sendgrid --capability mail.send")
}

func runPluginList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	timeout := fs.Duration("timeout", plugins.DefaultProbeTimeout, "Capability probe timeout")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("plugin list does not accept positional arguments")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	activeMailDriver := resolveMailDriver(cfg.MailDriver)
	inventory := plugins.CollectInventory(os.Getenv("PATH"), mail.RegisteredProviders(), *timeout)
	report := pluginListReport{
		ActiveMailDriver: activeMailDriver,
		Providers:        toPluginListItems(inventory, activeMailDriver),
	}

	if outputWantsJSON(*asJSON) {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if outputIsPretty() {
		fmt.Fprintf(stdout, "Plugins (active mail driver: %s)\n", report.ActiveMailDriver)
		if len(report.Providers) == 0 {
			fmt.Fprintln(stdout, "  none")
			return nil
		}
		for _, item := range report.Providers {
			status := "info"
			if item.ActiveMail {
				status = "ok"
			}
			if strings.TrimSpace(item.ProbeError) != "" {
				status = "warning"
			}
			capabilities := "-"
			if len(item.Capabilities) > 0 {
				capabilities = strings.Join(item.Capabilities, ",")
			}
			fmt.Fprintf(stdout, "  %s  %s (%s) caps=%s", statusTag(stdout, status), item.Provider, item.Source, capabilities)
			if strings.TrimSpace(item.BinaryPath) != "" {
				fmt.Fprintf(stdout, " bin=%s", item.BinaryPath)
			}
			if strings.TrimSpace(item.ProbeError) != "" {
				fmt.Fprintf(stdout, " probe=%s", item.ProbeError)
			}
			fmt.Fprintln(stdout)
		}
		return nil
	}

	fmt.Fprintf(stdout, "Active mail driver: %s\n", report.ActiveMailDriver)
	if len(report.Providers) == 0 {
		fmt.Fprintln(stdout, "No plugin providers detected")
		return nil
	}

	fmt.Fprintln(stdout, "provider\tcapabilities\tsource\tbinary\tactive_mail\tprobe")
	for _, item := range report.Providers {
		binary := "-"
		if strings.TrimSpace(item.BinaryPath) != "" {
			binary = item.BinaryPath
		}
		probe := "-"
		if strings.TrimSpace(item.ProbeError) != "" {
			probe = item.ProbeError
		}
		capabilities := "-"
		if len(item.Capabilities) > 0 {
			capabilities = strings.Join(item.Capabilities, ",")
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%t\t%s\n", item.Provider, capabilities, item.Source, binary, item.ActiveMail, probe)
	}
	return nil
}

func runPluginDoctor(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	timeout := fs.Duration("timeout", plugins.DefaultProbeTimeout, "Capability probe timeout")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("plugin doctor does not accept positional arguments")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	activeDriver := resolveMailDriver(cfg.MailDriver)
	inventory := plugins.CollectInventory(os.Getenv("PATH"), mail.RegisteredProviders(), *timeout)

	report := pluginDoctorReport{
		Status:    "ok",
		CheckedAt: nowRFC3339(),
	}

	pathValue := strings.TrimSpace(os.Getenv("PATH"))
	if pathValue == "" {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.path",
			Status:  "error",
			Details: "PATH is empty; external plugins cannot be discovered",
		})
	} else {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.path",
			Status:  "ok",
			Details: "PATH is configured",
		})
	}

	externalCount := 0
	for _, desc := range inventory {
		if desc.Source == plugins.SourceExternalGeneric {
			externalCount++
		}
	}
	if externalCount == 0 {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.discovery",
			Status:  "warning",
			Details: "no external plugins discovered on PATH",
		})
	} else {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.discovery",
			Status:  "ok",
			Details: fmt.Sprintf("detected %d external plugin(s)", externalCount),
		})
	}

	probeErrors := 0
	for _, desc := range inventory {
		if desc.Source == plugins.SourceExternalGeneric && strings.TrimSpace(desc.ProbeError) != "" {
			probeErrors++
		}
	}
	if probeErrors > 0 {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.capabilities",
			Status:  "warning",
			Details: fmt.Sprintf("%d generic plugin(s) failed capability probe", probeErrors),
		})
	} else {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.capabilities",
			Status:  "ok",
			Details: "capability probes succeeded",
		})
	}

	if activeDriver == "noop" {
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.mail_driver",
			Status:  "warning",
			Details: "mail_driver is noop; no outbound delivery plugin/provider selected",
		})
	} else {
		_, senderErr := mail.NewSender(mail.Config{
			Driver:           activeDriver,
			Timeout:          *timeout,
			SMTPHost:         strings.TrimSpace(cfg.SMTPHost),
			SMTPPort:         cfg.SMTPPort,
			SMTPUser:         strings.TrimSpace(cfg.SMTPUser),
			SMTPPass:         cfg.SMTPPass,
			SendGridAPIKey:   strings.TrimSpace(cfg.SendGridAPIKey),
			SendGridEndpoint: strings.TrimSpace(cfg.SendGridEndpoint),
		})
		if senderErr != nil {
			addPluginDoctorCheck(&report, pluginDoctorCheck{
				Name:    "plugin.mail_driver",
				Status:  "error",
				Details: senderErr.Error(),
			})
		} else {
			sourceSummary := summarizeProviderSources(inventory, activeDriver)
			if sourceSummary == "" {
				sourceSummary = "registered runtime provider"
			}
			addPluginDoctorCheck(&report, pluginDoctorCheck{
				Name:    "plugin.mail_driver",
				Status:  "ok",
				Details: fmt.Sprintf("mail_driver=%s resolved via %s", activeDriver, sourceSummary),
			})
		}
	}

	if outputWantsJSON(*asJSON) {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		if outputIsPretty() {
			fmt.Fprintf(stdout, "Plugin doctor: %s\n", statusTag(stdout, report.Status))
			for _, check := range report.Checks {
				fmt.Fprintf(stdout, "  %s  %s", statusTag(stdout, check.Status), check.Name)
				if strings.TrimSpace(check.Details) != "" {
					fmt.Fprintf(stdout, " - %s", check.Details)
				}
				fmt.Fprintln(stdout)
			}
		} else {
			fmt.Fprintf(stdout, "overall\t%s\n", report.Status)
			for _, check := range report.Checks {
				fmt.Fprintf(stdout, "%s\t%s\t%s\n", check.Name, check.Status, check.Details)
			}
		}
	}

	if report.Status == "degraded" {
		return fmt.Errorf("plugin doctor failed")
	}
	return nil
}

func runPluginTest(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin test", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	provider := fs.String("provider", "", "Provider name")
	capability := fs.String("capability", "", "Capability name (domain.action)")
	timeout := fs.Duration("timeout", plugins.DefaultProbeTimeout, "Capability probe timeout")
	execute := fs.Bool("execute", false, "Run execute-mode smoke check when supported")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("plugin test does not accept positional arguments")
	}
	if strings.TrimSpace(*provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(*capability) == "" {
		return fmt.Errorf("capability is required")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}
	if _, err := loadConfig(*configPath); err != nil {
		return err
	}

	targetProvider := strings.ToLower(strings.TrimSpace(*provider))
	targetCapability := strings.ToLower(strings.TrimSpace(*capability))
	mode := "discovery"
	if *execute {
		mode = "execute"
	}

	report := pluginTestReport{
		Provider:   targetProvider,
		Capability: targetCapability,
		Mode:       mode,
		Status:     "error",
		CheckedAt:  nowRFC3339(),
	}

	inventory := plugins.CollectInventory(os.Getenv("PATH"), mail.RegisteredProviders(), *timeout)
	desc, ok := selectPluginDescriptor(inventory, targetProvider, targetCapability)
	if !ok {
		report.Details = fmt.Sprintf("provider=%s capability=%s was not discovered", targetProvider, targetCapability)
		return emitPluginTestResult(report, *asJSON, stdout)
	}

	report.Source = desc.Source
	report.BinaryPath = desc.BinaryPath

	if !*execute {
		report.Status = "ok"
		report.Details = "discovery smoke check passed"
		if strings.TrimSpace(desc.ProbeError) != "" {
			report.Status = "warning"
			report.Details = fmt.Sprintf("provider discovered but capability probe returned warning: %s", desc.ProbeError)
		}
		return emitPluginTestResult(report, *asJSON, stdout)
	}

	switch desc.Source {
	case plugins.SourceExternalGeneric:
		caps, err := plugins.ProbeCapabilities(context.Background(), desc.BinaryPath, *timeout)
		if err != nil {
			report.Status = "error"
			report.Details = fmt.Sprintf("execute smoke probe failed: %v", err)
			return emitPluginTestResult(report, *asJSON, stdout)
		}
		if !capabilityInSlice(caps, targetCapability) {
			report.Status = "error"
			report.Details = fmt.Sprintf("execute smoke probe did not report capability %s", targetCapability)
			return emitPluginTestResult(report, *asJSON, stdout)
		}
		report.Status = "ok"
		report.Details = "execute smoke check passed via capability probe"
	case plugins.SourceBuiltinMail:
		report.Status = "warning"
		report.Details = "built-in provider supports discovery only; execute smoke is for external plugins"
	default:
		report.Status = "warning"
		report.Details = "unknown plugin source; execute smoke skipped"
	}

	return emitPluginTestResult(report, *asJSON, stdout)
}

func toPluginListItems(inventory []plugins.Descriptor, activeDriver string) []pluginListItem {
	items := make([]pluginListItem, 0, len(inventory))
	for _, desc := range inventory {
		item := pluginListItem{
			Provider:     desc.Provider,
			Capabilities: append([]string(nil), desc.Capabilities...),
			Source:       desc.Source,
			BinaryPath:   desc.BinaryPath,
			ActiveMail:   desc.Provider == activeDriver && plugins.SupportsCapability(desc, pluginMailCapability),
			ProbeError:   desc.ProbeError,
		}
		sort.Strings(item.Capabilities)
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ActiveMail != items[j].ActiveMail {
			return items[i].ActiveMail
		}
		if items[i].Provider != items[j].Provider {
			return items[i].Provider < items[j].Provider
		}
		if items[i].Source != items[j].Source {
			return pluginSourcePriority(items[i].Source) < pluginSourcePriority(items[j].Source)
		}
		return items[i].BinaryPath < items[j].BinaryPath
	})
	return items
}

func addPluginDoctorCheck(report *pluginDoctorReport, check pluginDoctorCheck) {
	report.Checks = append(report.Checks, check)
	switch check.Status {
	case "error":
		report.Status = "degraded"
	case "warning":
		if report.Status == "ok" {
			report.Status = "warning"
		}
	}
}

func summarizeProviderSources(inventory []plugins.Descriptor, provider string) string {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		return ""
	}

	seen := map[plugins.Source]struct{}{}
	for _, desc := range inventory {
		if desc.Provider != normalizedProvider {
			continue
		}
		seen[desc.Source] = struct{}{}
	}
	if len(seen) == 0 {
		return ""
	}

	sources := make([]plugins.Source, 0, len(seen))
	for source := range seen {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return pluginSourcePriority(sources[i]) < pluginSourcePriority(sources[j])
	})

	labels := make([]string, 0, len(sources))
	for _, source := range sources {
		labels = append(labels, string(source))
	}
	return strings.Join(labels, ", ")
}

func selectPluginDescriptor(inventory []plugins.Descriptor, provider, capability string) (plugins.Descriptor, bool) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	normalizedCapability := strings.ToLower(strings.TrimSpace(capability))
	if normalizedProvider == "" || normalizedCapability == "" {
		return plugins.Descriptor{}, false
	}

	candidates := make([]plugins.Descriptor, 0, 4)
	for _, desc := range inventory {
		if desc.Provider != normalizedProvider {
			continue
		}
		if !plugins.SupportsCapability(desc, normalizedCapability) {
			continue
		}
		candidates = append(candidates, desc)
	}
	if len(candidates) == 0 {
		return plugins.Descriptor{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.Source != right.Source {
			return pluginSourcePriority(left.Source) < pluginSourcePriority(right.Source)
		}
		if strings.TrimSpace(left.ProbeError) == "" && strings.TrimSpace(right.ProbeError) != "" {
			return true
		}
		if strings.TrimSpace(left.ProbeError) != "" && strings.TrimSpace(right.ProbeError) == "" {
			return false
		}
		return left.BinaryPath < right.BinaryPath
	})
	return candidates[0], true
}

func capabilityInSlice(values []string, target string) bool {
	normalizedTarget := strings.ToLower(strings.TrimSpace(target))
	if normalizedTarget == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == normalizedTarget {
			return true
		}
	}
	return false
}

func emitPluginTestResult(report pluginTestReport, asJSON bool, stdout io.Writer) error {
	if outputWantsJSON(asJSON) {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		if outputIsPretty() {
			fmt.Fprintf(stdout, "Plugin test: %s\n", statusTag(stdout, report.Status))
			fmt.Fprintf(stdout, "  provider:   %s\n", report.Provider)
			fmt.Fprintf(stdout, "  capability: %s\n", report.Capability)
			fmt.Fprintf(stdout, "  mode:       %s\n", report.Mode)
			if report.Source != "" {
				fmt.Fprintf(stdout, "  source:     %s\n", report.Source)
			}
			if strings.TrimSpace(report.BinaryPath) != "" {
				fmt.Fprintf(stdout, "  binary:     %s\n", report.BinaryPath)
			}
			if strings.TrimSpace(report.Details) != "" {
				fmt.Fprintf(stdout, "  details:    %s\n", report.Details)
			}
		} else {
			fmt.Fprintf(stdout, "provider\t%s\n", report.Provider)
			fmt.Fprintf(stdout, "capability\t%s\n", report.Capability)
			fmt.Fprintf(stdout, "mode\t%s\n", report.Mode)
			fmt.Fprintf(stdout, "status\t%s\n", report.Status)
			if report.Source != "" {
				fmt.Fprintf(stdout, "source\t%s\n", report.Source)
			}
			if strings.TrimSpace(report.BinaryPath) != "" {
				fmt.Fprintf(stdout, "binary\t%s\n", report.BinaryPath)
			}
			if strings.TrimSpace(report.Details) != "" {
				fmt.Fprintf(stdout, "details\t%s\n", report.Details)
			}
		}
	}

	if report.Status == "error" {
		return fmt.Errorf("plugin test failed")
	}
	return nil
}

func pluginSourcePriority(source plugins.Source) int {
	switch source {
	case plugins.SourceExternalGeneric:
		return 0
	case plugins.SourceBuiltinMail:
		return 1
	default:
		return 2
	}
}
