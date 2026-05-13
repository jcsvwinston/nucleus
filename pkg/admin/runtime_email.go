package admin

import (
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/mail"
)

type emailRuntimeSnapshot struct {
	Enabled          bool     `json:"enabled"`
	Driver           string   `json:"driver"`
	From             string   `json:"from,omitempty"`
	Status           string   `json:"status"`
	Message          string   `json:"message"`
	ProviderType     string   `json:"provider_type"`
	BuiltinProviders []string `json:"builtin_providers"`
	SMTPHost         string   `json:"smtp_host,omitempty"`
}

func inspectEmailRuntime(cfg PanelConfig) emailRuntimeSnapshot {
	driver := strings.ToLower(strings.TrimSpace(cfg.MailDriver))
	if driver == "" {
		driver = "noop"
	}

	providers := mail.RegisteredProviders()
	snapshot := emailRuntimeSnapshot{
		Enabled:          driver != "noop",
		Driver:           driver,
		From:             strings.TrimSpace(cfg.MailFrom),
		Status:           "configured",
		Message:          "mail delivery is configured",
		ProviderType:     "external",
		BuiltinProviders: providers,
	}

	if containsString(providers, driver) {
		snapshot.ProviderType = "builtin"
	}

	switch driver {
	case "noop":
		snapshot.Enabled = false
		snapshot.Status = "disabled"
		snapshot.ProviderType = "builtin"
		snapshot.Message = "mail driver is noop"
	case "smtp":
		snapshot.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
		if snapshot.SMTPHost == "" {
			snapshot.Status = "degraded"
			snapshot.Message = "smtp driver is selected but smtp_host is empty"
		} else {
			snapshot.Message = "smtp delivery is configured"
		}
	default:
		// External provider — discovered via `nucleus-plugin-<driver>` or
		// registered programmatically through `mail.RegisterProvider`.
		// The admin panel reports configuration as "external" without
		// modelling per-provider knobs (those live in the plugin binary).
		snapshot.Message = "mail driver is configured"
	}

	if snapshot.Enabled && snapshot.From == "" {
		snapshot.Status = "degraded"
		snapshot.Message = "mail driver is configured but mail_from is empty"
	}

	return snapshot
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
