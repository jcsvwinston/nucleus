// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// Policy says which external executables may run: the capability plugins
// (`nucleus-plugin-<provider>`, run by the runtime and by `nucleus plugin`)
// and the external commands (`nucleus-<name>`, run by `nucleus <name>`).
//
// An external executable runs only when the policy lists it: the zero
// value allows none of them (DEP-2026-014). Before v2.0.0 the zero value
// allowed every one, which is what Nucleus did before the policy existed.
//
// The configuration keys are `plugins.allow_external`, `plugins.allowed`
// and `plugins.commands`; app.PluginsConfig.Policy builds a Policy from
// them.
type Policy struct {
	// DenyExternal refuses every external executable, whatever the lists
	// say (`plugins.allow_external: false`).
	DenyExternal bool

	// Allowed is the set of capability plugins that run: a provider runs a
	// capability only when an entry names the provider and lists the
	// capability. Empty allows no provider.
	Allowed []Allowance

	// Commands are the external commands the CLI dispatches. Empty allows
	// no `nucleus-<name>` on PATH.
	Commands []string
}

// Allowance is one entry of Policy.Allowed: a provider and the
// capabilities it may run.
type Allowance struct {
	Provider     string
	Capabilities []string
}

// RefusedError is the answer of a Policy for an executable it does not
// allow. It names the executable and the configuration key that refused it.
type RefusedError struct {
	// Kind is "plugin" for a capability plugin, "command" for an external
	// command.
	Kind string
	// Name is the provider of a plugin, or the name of a command.
	Name string
	// Capability is the capability that was asked for; empty when the
	// provider is refused outright.
	Capability string
	// Key is the configuration key whose value refused it.
	Key string
}

func (e *RefusedError) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Kind == "command":
		if e.Key == "plugins.allow_external" {
			return fmt.Sprintf("external command nucleus-%s is not allowed: plugins.allow_external is false", e.Name)
		}
		return fmt.Sprintf("external command nucleus-%s is not allowed: %s does not list %q", e.Name, e.Key, e.Name)
	case e.Key == "plugins.allow_external":
		return fmt.Sprintf("plugin %s%s is not allowed: plugins.allow_external is false", GenericBinaryPrefix, e.Name)
	case e.Capability == "":
		return fmt.Sprintf("plugin %s%s is not allowed: %s has no entry for provider %q", GenericBinaryPrefix, e.Name, e.Key, e.Name)
	default:
		return fmt.Sprintf("plugin %s%s is not allowed to run %s: %s does not list it for provider %q", GenericBinaryPrefix, e.Name, e.Capability, e.Key, e.Name)
	}
}

// AllowPlugin reports whether the capability plugin of provider may run
// capability. An empty capability asks whether the executable may run at
// all — to be asked for its capabilities — which an entry for the provider
// allows whatever capabilities it lists. A refusal is a *RefusedError.
func (p Policy) AllowPlugin(provider, capability string) error {
	provider = normalizeToken(provider)
	capability = normalizeToken(capability)
	if p.DenyExternal {
		return &RefusedError{Kind: "plugin", Name: provider, Capability: capability, Key: "plugins.allow_external"}
	}
	listed := false
	for _, a := range p.Allowed {
		if normalizeToken(a.Provider) != provider {
			continue
		}
		listed = true
		if capability == "" {
			return nil
		}
		for _, c := range a.Capabilities {
			if normalizeToken(c) == capability {
				return nil
			}
		}
	}
	if !listed {
		return &RefusedError{Kind: "plugin", Name: provider, Key: "plugins.allowed"}
	}
	return &RefusedError{Kind: "plugin", Name: provider, Capability: capability, Key: "plugins.allowed"}
}

// AllowCommand reports whether the CLI may dispatch `nucleus <name>` to the
// external command `nucleus-<name>`. A refusal is a *RefusedError.
func (p Policy) AllowCommand(name string) error {
	name = normalizeToken(name)
	if p.DenyExternal {
		return &RefusedError{Kind: "command", Name: name, Key: "plugins.allow_external"}
	}
	for _, c := range p.Commands {
		if normalizeToken(c) == name {
			return nil
		}
	}
	return &RefusedError{Kind: "command", Name: name, Key: "plugins.commands"}
}

// ListsPlugins reports whether the configuration said which capability
// plugins may run — an entry under plugins.allowed, or
// plugins.allow_external: false. A policy that lists none allows none
// (DEP-2026-014); `nucleus plugin doctor` tells the two apart so an
// operator with plugins on PATH and no block learns why none of them runs.
func (p Policy) ListsPlugins() bool {
	return p.DenyExternal || len(p.Allowed) > 0
}

// ListsCommands is ListsPlugins for the external commands.
func (p Policy) ListsCommands() bool {
	return p.DenyExternal || len(p.Commands) > 0
}

// String renders the policy the way its configuration reads, for logs and
// diagnostics.
func (p Policy) String() string {
	if p.DenyExternal {
		return "plugins.allow_external: false"
	}
	var parts []string
	if len(p.Allowed) > 0 {
		entries := make([]string, 0, len(p.Allowed))
		for _, a := range p.Allowed {
			caps := append([]string(nil), a.Capabilities...)
			sort.Strings(caps)
			entries = append(entries, normalizeToken(a.Provider)+"["+strings.Join(caps, ",")+"]")
		}
		parts = append(parts, "plugins.allowed: "+strings.Join(entries, " "))
	}
	if len(p.Commands) > 0 {
		parts = append(parts, "plugins.commands: "+strings.Join(p.Commands, ","))
	}
	if len(parts) == 0 {
		return "no allowlist (no external executable runs)"
	}
	return strings.Join(parts, "; ")
}
