// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// PluginsConfig is the `plugins` block: which external executables may run.
//
//	plugins:
//	  allow_external: true          # false refuses every external executable
//	  allowed:                      # when set, the only capability plugins that run
//	    - provider: sendgrid
//	      capabilities: [mail.send]
//	  commands: [lint]              # when set, the only nucleus-<name> commands dispatched
//
// Nothing set allows every external executable — the behaviour before the
// block existed, kept until v2.0.0. From v2.0.0 an external plugin or
// command runs only when listed (DEP-2026-014).
type PluginsConfig struct {
	// AllowExternal false refuses every external executable, whatever the
	// lists say. Nil (unset) is true.
	AllowExternal *bool `koanf:"allow_external"`
	// Allowed, when it has entries, is the only set of capability plugins
	// that run: a provider runs a capability only when an entry lists both.
	Allowed []PluginAllowance `koanf:"allowed"`
	// Commands, when it has entries, are the only external commands
	// `nucleus <name>` dispatches to `nucleus-<name>`.
	Commands []string `koanf:"commands"`
}

// PluginAllowance is one entry of `plugins.allowed`.
type PluginAllowance struct {
	Provider     string   `koanf:"provider"`
	Capabilities []string `koanf:"capabilities"`
}

// Policy is the plugins.Policy this block describes, the one the runtime
// and the CLI enforce.
func (c PluginsConfig) Policy() plugins.Policy {
	p := plugins.Policy{
		DenyExternal: c.AllowExternal != nil && !*c.AllowExternal,
		Commands:     append([]string(nil), c.Commands...),
	}
	for _, a := range c.Allowed {
		p.Allowed = append(p.Allowed, plugins.Allowance{
			Provider:     a.Provider,
			Capabilities: append([]string(nil), a.Capabilities...),
		})
	}
	return p
}

// validatePlugins rejects an allowlist entry that could never match: an
// entry with no provider, no capabilities, or a capability that is not a
// `domain.action` name. A misspelt key inside an entry (`capabilites:`)
// decodes as an entry with no capabilities, so this is also where that typo
// is caught.
func validatePlugins(c PluginsConfig) error {
	for i, a := range c.Allowed {
		provider := strings.TrimSpace(a.Provider)
		if provider == "" {
			return fmt.Errorf("%w: plugins.allowed[%d] has no provider — name the provider of nucleus-plugin-<provider>", ErrInvalidConfigValue, i)
		}
		if strings.ContainsAny(provider, `/\ `) {
			return fmt.Errorf("%w: plugins.allowed[%d].provider %q is not a provider name — use the <provider> of nucleus-plugin-<provider>, not a path", ErrInvalidConfigValue, i, a.Provider)
		}
		if len(a.Capabilities) == 0 {
			return fmt.Errorf("%w: plugins.allowed[%d] (provider %q) lists no capabilities — an entry allows only the capabilities it names, e.g. capabilities: [%s]", ErrInvalidConfigValue, i, provider, plugins.CapabilityMailSend)
		}
		for j, capability := range a.Capabilities {
			if !strings.Contains(strings.TrimSpace(capability), ".") {
				return fmt.Errorf("%w: plugins.allowed[%d].capabilities[%d] %q is not a capability — capabilities are named domain.action, e.g. %s", ErrInvalidConfigValue, i, j, capability, plugins.CapabilityMailSend)
			}
		}
	}
	for i, name := range c.Commands {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || strings.ContainsAny(trimmed, `/\ `) {
			return fmt.Errorf("%w: plugins.commands[%d] %q is not a command name — use the <name> of nucleus-<name>", ErrInvalidConfigValue, i, name)
		}
	}
	return nil
}
