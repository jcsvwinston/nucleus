// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// pluginBridgeFromConfig builds an outbox bridge of type plugin from its
// configuration entry:
//
//	outbox:
//	  bridges:
//	    - name: events
//	      type: plugin
//	      config:
//	        provider: nats            # runs nucleus-plugin-nats
//	        capability: queue.publish # or webhook.deliver
//	        pattern: "orders.*"
//	        timeout: 10s
//
// The configuration's plugins block decides whether the provider may run
// the capability, as it does for a mail plugin; a bridge it refuses, or one
// whose executable is missing or does not advertise the capability, stops
// the application from starting rather than failing every message later.
func pluginBridgeFromConfig(a *App, cfg *Config, bridgeCfg BridgeConfig) (*outbox.PluginBridge, error) {
	var timeout time.Duration
	if raw := strings.TrimSpace(getConfigString(bridgeCfg.Config, "timeout")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("outbox: plugin bridge %q: timeout %q is not a positive duration (e.g. 10s)", bridgeCfg.Name, raw)
		}
		timeout = d
	}
	policy := cfg.Plugins.Policy()
	pc := outbox.PluginConfig{
		Name:            bridgeCfg.Name,
		Provider:        getConfigString(bridgeCfg.Config, "provider"),
		Capability:      getConfigString(bridgeCfg.Config, "capability"),
		Timeout:         timeout,
		Policy:          policy,
		Headers:         getConfigStringMap(bridgeCfg.Config, "headers"),
		Topic:           getConfigString(bridgeCfg.Config, "topic"),
		URL:             getConfigString(bridgeCfg.Config, "url"),
		Method:          getConfigString(bridgeCfg.Config, "method"),
		Secret:          getConfigString(bridgeCfg.Config, "secret"),
		PayloadEncoding: getConfigString(bridgeCfg.Config, "payload_encoding"),
	}
	bridge, err := outbox.NewPluginBridge(pc)
	if err != nil {
		return nil, err
	}
	capability := strings.ToLower(strings.TrimSpace(pc.Capability))
	provider := strings.ToLower(strings.TrimSpace(pc.Provider))
	if capability == plugins.CapabilityWebhookDeliver && pc.Secret == "" {
		a.Logger.Warn("outbox: webhook bridge configured without a signing secret; its consumer must authenticate deliveries itself",
			"bridge", bridgeCfg.Name)
	}
	if !policy.ListsPlugins() {
		// DEP-2026-014, as for a mail plugin: once per bridge, at boot.
		a.Logger.Warn("outbox: plugin bridge runs an external plugin without an allowlist",
			"bridge", bridgeCfg.Name,
			"provider", provider,
			"fix", fmt.Sprintf("list it under plugins.allowed: [{provider: %s, capabilities: [%s]}]", provider, capability),
			"deprecation", "DEP-2026-014: from v2.0.0 an external plugin runs only when plugins.allowed lists it")
	}
	a.Logger.Info("outbox: plugin bridge configured", "bridge", bridgeCfg.Name, "provider", provider, "capability", capability)
	return bridge, nil
}
