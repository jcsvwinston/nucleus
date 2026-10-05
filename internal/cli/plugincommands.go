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
	"time"

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
	Refused      string         `json:"refused,omitempty"`
}

type pluginListReport struct {
	ActiveMailDriver string           `json:"active_mail_driver"`
	Policy           string           `json:"policy"`
	Providers        []pluginListItem `json:"providers"`
	// Commands are the external `nucleus-<name>` commands on PATH, which
	// `nucleus <name>` dispatches to.
	Commands []externalCommand `json:"commands"`
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
	// ExitCode and Stderr are the plugin's, from the first check that
	// failed; the command ends with that exit code.
	ExitCode int    `json:"exit_code,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	// Checks has one entry per capability an execute run sent an envelope
	// for.
	Checks []pluginTestCheck `json:"checks,omitempty"`
}

// pluginTestCheck is one request envelope `plugin test --execute` sent and
// what came back.
type pluginTestCheck struct {
	Capability string `json:"capability"`
	Status     string `json:"status"`
	RequestID  string `json:"request_id"`
	ExitCode   int    `json:"exit_code"`
	Code       string `json:"code,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Details    string `json:"details"`
}

// pluginTestFailure ends `nucleus plugin test` with the plugin's own exit
// code when the plugin is what failed.
type pluginTestFailure struct{ code int }

func (e pluginTestFailure) Error() string {
	return fmt.Sprintf("plugin test failed (the plugin exited %d)", e.code)
}

func (e pluginTestFailure) ExitCode() int { return e.code }

func runPlugin(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
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
		return runPluginTest(args[1:], stdin, stdout, stderr)
	default:
		return fmt.Errorf("unknown plugin subcommand %q", args[0])
	}
}

func printPluginUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  nucleus plugin <list|doctor|test> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  list      List the capability plugins (nucleus-plugin-<provider>) and the external commands (nucleus-<name>) on PATH")
	fmt.Fprintln(w, "  doctor    Validate plugin runtime and configuration wiring")
	fmt.Fprintln(w, "  test      Check a plugin: discovery, or with --execute one request envelope per capability")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "plugin test --execute sends the plugin a real request envelope for --capability, or for every")
	fmt.Fprintln(w, "capability it advertises, with a sample payload (--payload <file|-> sends yours), and fails with")
	fmt.Fprintln(w, "the plugin's exit code and stderr. A plugin the configuration's plugins block does not allow is")
	fmt.Fprintln(w, "refused without being executed.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  nucleus plugin list --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin doctor --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin test --provider sendgrid --capability mail.send")
	fmt.Fprintln(w, "  nucleus plugin test --provider sendgrid --execute")
	fmt.Fprintln(w, "  nucleus plugin test --provider sendgrid --capability mail.send --execute --payload message.json")
}

// pluginInventory is what the plugin commands inspect: the built-in mail
// providers and the external plugins on PATH, those the policy refuses
// listed and never executed.
func pluginInventory(policy plugins.Policy, timeout time.Duration) []plugins.Descriptor {
	inventory := plugins.BuiltinMailDescriptorsFromProviders(mail.RegisteredProviders())
	return append(inventory, plugins.DiscoverAllowed(os.Getenv("PATH"), timeout, policy)...)
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
	policy := cfg.Plugins.Policy()
	inventory := pluginInventory(policy, *timeout)
	report := pluginListReport{
		ActiveMailDriver: activeMailDriver,
		Policy:           policy.String(),
		Providers:        toPluginListItems(inventory, activeMailDriver),
		Commands:         discoverExternalCommands(os.Getenv("PATH"), policy),
	}
	if report.Commands == nil {
		report.Commands = []externalCommand{}
	}

	if outputWantsJSON(*asJSON) {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if outputIsPretty() {
		fmt.Fprintf(stdout, "Plugins (active mail driver: %s; %s)\n", report.ActiveMailDriver, report.Policy)
		if len(report.Providers) == 0 {
			fmt.Fprintln(stdout, "  none")
		}
		for _, item := range report.Providers {
			status := "info"
			if item.ActiveMail {
				status = "ok"
			}
			if strings.TrimSpace(item.ProbeError) != "" || item.Refused != "" {
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
			if item.Refused != "" {
				fmt.Fprintf(stdout, " refused=%s", item.Refused)
			}
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stdout, "External commands (nucleus <name> runs nucleus-<name>)")
		if len(report.Commands) == 0 {
			fmt.Fprintln(stdout, "  none")
		}
		for _, c := range report.Commands {
			status := "info"
			if c.Shadowed || c.Refused != "" {
				status = "warning"
			}
			fmt.Fprintf(stdout, "  %s  %s bin=%s", statusTag(stdout, status), c.Name, c.BinaryPath)
			if c.Shadowed {
				fmt.Fprint(stdout, " shadowed=a built-in command has this name")
			}
			if c.Refused != "" {
				fmt.Fprintf(stdout, " refused=%s", c.Refused)
			}
			fmt.Fprintln(stdout)
		}
		return nil
	}

	fmt.Fprintf(stdout, "Active mail driver: %s\n", report.ActiveMailDriver)
	fmt.Fprintf(stdout, "Policy: %s\n", report.Policy)
	if len(report.Providers) == 0 {
		fmt.Fprintln(stdout, "No plugin providers detected")
	} else {
		fmt.Fprintln(stdout, "provider\tcapabilities\tsource\tbinary\tactive_mail\tprobe\trefused")
		for _, item := range report.Providers {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%t\t%s\t%s\n", item.Provider, dashIfEmpty(strings.Join(item.Capabilities, ",")),
				item.Source, dashIfEmpty(item.BinaryPath), item.ActiveMail, dashIfEmpty(item.ProbeError), dashIfEmpty(item.Refused))
		}
	}
	if len(report.Commands) == 0 {
		fmt.Fprintln(stdout, "No external commands detected")
		return nil
	}
	fmt.Fprintln(stdout, "command\tbinary\tshadowed\trefused")
	for _, c := range report.Commands {
		fmt.Fprintf(stdout, "%s\t%s\t%t\t%s\n", c.Name, c.BinaryPath, c.Shadowed, dashIfEmpty(c.Refused))
	}
	return nil
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
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
	policy := cfg.Plugins.Policy()
	inventory := pluginInventory(policy, *timeout)

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

	// DEP-2026-014: the allowlist is opt-in until v2.0.0. A plugin that
	// runs unlisted today is refused from then on, so say it now.
	switch {
	case policy.ListsPlugins():
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.allowlist",
			Status:  "ok",
			Details: policy.String(),
		})
	case externalCount > 0:
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:   "plugin.allowlist",
			Status: "warning",
			Details: fmt.Sprintf("%d external plugin(s) on PATH run without an allowlist; list the ones this application uses under "+
				"plugins.allowed — from v2.0.0 an unlisted plugin does not run (DEP-2026-014)", externalCount),
		})
	default:
		addPluginDoctorCheck(&report, pluginDoctorCheck{
			Name:    "plugin.allowlist",
			Status:  "ok",
			Details: "no allowlist, and no external plugin on PATH",
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
			Driver:   activeDriver,
			Timeout:  *timeout,
			SMTPHost: strings.TrimSpace(cfg.SMTPHost),
			SMTPPort: cfg.SMTPPort,
			SMTPUser: strings.TrimSpace(cfg.SMTPUser),
			SMTPPass: cfg.SMTPPass,
			Plugins:  policy,
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

func runPluginTest(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin test", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	provider := fs.String("provider", "", "Provider name (the <provider> of nucleus-plugin-<provider>)")
	capability := fs.String("capability", "", "Capability name (domain.action); empty checks every capability the plugin advertises")
	timeout := fs.Duration("timeout", plugins.DefaultProbeTimeout, "Timeout of the capability probe and of each request")
	execute := fs.Bool("execute", false, "Send the plugin a request envelope per capability and check the response")
	payloadPath := fs.String("payload", "", "With --execute and --capability: a file (or - for stdin) holding the JSON payload to send instead of the sample one")
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
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0")
	}
	if *payloadPath != "" && (!*execute || strings.TrimSpace(*capability) == "") {
		return fmt.Errorf("--payload needs --execute and --capability: a payload belongs to one capability")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	var payload json.RawMessage
	if *payloadPath != "" {
		if payload, err = readPluginTestPayload(*payloadPath, stdin); err != nil {
			return err
		}
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

	policy := cfg.Plugins.Policy()
	inventory := pluginInventory(policy, *timeout)
	desc, ok := selectPluginDescriptor(inventory, targetProvider, targetCapability)
	if !ok {
		if refused := refusedDescriptor(inventory, targetProvider); refused != nil {
			report.Source = refused.Source
			report.BinaryPath = refused.BinaryPath
			report.Details = refused.Refused + " (it was not executed)"
			return emitPluginTestResult(report, *asJSON, stdout)
		}
		if targetCapability == "" {
			report.Details = fmt.Sprintf("provider=%s was not discovered", targetProvider)
		} else {
			report.Details = fmt.Sprintf("provider=%s capability=%s was not discovered", targetProvider, targetCapability)
		}
		return emitPluginTestResult(report, *asJSON, stdout)
	}

	report.Source = desc.Source
	report.BinaryPath = desc.BinaryPath

	if !*execute {
		report.Status = "ok"
		report.Details = "discovery smoke check passed"
		if targetCapability == "" {
			report.Details = fmt.Sprintf("discovery smoke check passed: advertises %s", strings.Join(desc.Capabilities, ", "))
		}
		if strings.TrimSpace(desc.ProbeError) != "" {
			report.Status = "warning"
			report.Details = fmt.Sprintf("provider discovered but capability probe returned warning: %s", desc.ProbeError)
		}
		return emitPluginTestResult(report, *asJSON, stdout)
	}

	switch desc.Source {
	case plugins.SourceExternalGeneric:
		capabilities := desc.Capabilities
		if targetCapability != "" {
			capabilities = []string{targetCapability}
		}
		report.Status = "ok"
		var details []string
		for _, c := range capabilities {
			check := executePluginCheck(desc, c, payload, policy, *timeout)
			report.Checks = append(report.Checks, check)
			details = append(details, c+": "+check.Details)
			switch check.Status {
			case "error":
				if report.Status != "error" {
					report.Status = "error"
					report.ExitCode = check.ExitCode
					report.Stderr = check.Stderr
				}
			case "warning":
				if report.Status == "ok" {
					report.Status = "warning"
				}
			}
		}
		report.Details = strings.Join(details, "; ")
	case plugins.SourceBuiltinMail:
		report.Status = "warning"
		report.Details = "built-in provider supports discovery only; execute smoke is for external plugins"
	default:
		report.Status = "warning"
		report.Details = "unknown plugin source; execute smoke skipped"
	}

	if err := emitPluginTestResult(report, *asJSON, stdout); err != nil {
		if report.ExitCode > 0 {
			return pluginTestFailure{code: report.ExitCode}
		}
		return err
	}
	return nil
}

// executePluginCheck sends the plugin one request envelope for capability
// and reads what comes back: the exit code, the response envelope, and the
// output the capability's schema defines.
func executePluginCheck(desc plugins.Descriptor, capability string, payload json.RawMessage, policy plugins.Policy, timeout time.Duration) pluginTestCheck {
	check := pluginTestCheck{Capability: capability, Status: "error"}
	if refused := policy.AllowPlugin(desc.Provider, capability); refused != nil {
		check.Details = refused.Error() + " (it was not executed)"
		return check
	}
	var body any = samplePluginPayload(capability)
	if len(payload) > 0 {
		body = payload
	}
	request, err := plugins.NewRequestEnvelope(desc.Provider, capability, timeout, body, map[string]string{"source": "nucleus plugin test"})
	if err != nil {
		check.Details = fmt.Sprintf("build the request envelope: %v", err)
		return check
	}
	check.RequestID = request.RequestID

	response, err := plugins.ExecuteRequest(context.Background(), desc.BinaryPath, request, timeout)
	if err != nil {
		var execErr *plugins.ExecutionError
		if errors.As(err, &execErr) {
			check.ExitCode = execErr.ExitCode
			check.Code = execErr.Code
			check.Stderr = execErr.Stderr
			reason := strings.TrimSpace(execErr.Message)
			if execErr.Code != "" {
				reason = execErr.Code + ": " + reason
			}
			if reason == "" {
				reason = "no error in the response"
			}
			if execErr.ExitCode > 0 {
				check.Details = fmt.Sprintf("the plugin exited %d (%s)", execErr.ExitCode, reason)
			} else {
				check.Details = fmt.Sprintf("the plugin failed the request (%s)", reason)
			}
			if check.Stderr != "" {
				check.Details += "; stderr: " + firstLine(check.Stderr)
			}
			return check
		}
		check.Details = fmt.Sprintf("the plugin exited 0 without a valid response envelope: %v", err)
		return check
	}
	if accepted, known := responseAccepted(capability, response.Output); known && !accepted {
		check.Details = "the plugin answered ok and did not accept the request (output.accepted is false)"
		return check
	}
	check.Status = "ok"
	check.Details = "the plugin answered the request envelope"
	switch response.RequestID {
	case request.RequestID:
	case "":
		check.Status = "warning"
		check.Details += ", and its response does not echo request_id"
	default:
		check.Status = "warning"
		check.Details += fmt.Sprintf(", and its response carries request_id %q, not the request's %q", response.RequestID, request.RequestID)
	}
	return check
}

// samplePluginPayload is the request body `plugin test --execute` sends for
// a capability: one that satisfies the capability's schema and is addressed
// nowhere real (example.invalid never resolves).
func samplePluginPayload(capability string) any {
	switch capability {
	case plugins.CapabilityMailSend:
		return plugins.MailSendPayload{
			From:    "nucleus-plugin-test@example.invalid",
			To:      []string{"nucleus-plugin-test@example.invalid"},
			Subject: "nucleus plugin test",
			Body:    "Sent by nucleus plugin test --execute to check the mail.send contract.",
		}
	case plugins.CapabilityQueuePublish:
		return plugins.QueuePublishPayload{Topic: "nucleus.plugin_test", Body: json.RawMessage(`{"source":"nucleus plugin test"}`)}
	case plugins.CapabilityWebhookDeliver:
		return plugins.WebhookDeliverPayload{URL: "https://nucleus-plugin-test.example.invalid/hook", Method: "POST", Body: `{"source":"nucleus plugin test"}`}
	}
	return map[string]string{"source": "nucleus plugin test"}
}

// responseAccepted reads the accepted field of a capability whose output
// schema has one. known is false for a capability without a schema here.
func responseAccepted(capability string, output json.RawMessage) (accepted, known bool) {
	switch capability {
	case plugins.CapabilityMailSend, plugins.CapabilityQueuePublish, plugins.CapabilityWebhookDeliver:
	default:
		return false, false
	}
	if len(output) == 0 {
		return true, true
	}
	var out struct {
		Accepted *bool `json:"accepted"`
	}
	if err := json.Unmarshal(output, &out); err != nil || out.Accepted == nil {
		return true, true
	}
	return *out.Accepted, true
}

func readPluginTestPayload(path string, stdin io.Reader) (json.RawMessage, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read --payload: %w", err)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("--payload %s is not JSON", path)
	}
	return json.RawMessage(raw), nil
}

// refusedDescriptor is the external plugin of provider the policy refused,
// when there is one.
func refusedDescriptor(inventory []plugins.Descriptor, provider string) *plugins.Descriptor {
	for i := range inventory {
		if inventory[i].Provider == provider && inventory[i].Refused != "" {
			return &inventory[i]
		}
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
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
	if normalizedProvider == "" {
		return plugins.Descriptor{}, false
	}

	// An empty capability selects the provider whatever it advertises.
	candidates := make([]plugins.Descriptor, 0, 4)
	for _, desc := range inventory {
		if desc.Provider != normalizedProvider || desc.Refused != "" {
			continue
		}
		if normalizedCapability == "" && len(desc.Capabilities) == 0 {
			continue
		}
		if normalizedCapability != "" && !plugins.SupportsCapability(desc, normalizedCapability) {
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
		capability := report.Capability
		if capability == "" {
			capability = "(every one it advertises)"
			if len(report.Checks) > 0 {
				names := make([]string, 0, len(report.Checks))
				for _, c := range report.Checks {
					names = append(names, c.Capability)
				}
				capability = strings.Join(names, ",")
			}
		}
		if outputIsPretty() {
			fmt.Fprintf(stdout, "Plugin test: %s\n", statusTag(stdout, report.Status))
			fmt.Fprintf(stdout, "  provider:   %s\n", report.Provider)
			fmt.Fprintf(stdout, "  capability: %s\n", capability)
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
			fmt.Fprintf(stdout, "capability\t%s\n", capability)
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
