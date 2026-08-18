// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-5, app side: the outbox configuration must let the operator set the
// lease owner and the missing-route policy from nucleus.yml, and the DEFAULT
// lease owner must be per-instance (derived from the hostname/pid) instead
// of the constant "nucleus-app" every process shared — which made lease rows
// untraceable and let any co-tenant process steal messages it cannot deliver.
package app

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func outboxKoanfKeys(t *testing.T) map[string]struct{} {
	t.Helper()
	out := map[string]struct{}{}
	typ := reflect.TypeOf(OutboxConfig{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.TrimSpace(typ.Field(i).Tag.Get("koanf"))
		if tag != "" && tag != "-" {
			out[tag] = struct{}{}
		}
	}
	return out
}

func TestOutboxConfigExposesLeaseOwnerAndMissingRoutePolicy(t *testing.T) {
	keys := outboxKoanfKeys(t)
	for _, want := range []string{"lease_owner", "missing_route_policy"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("OutboxConfig has no %q key — the knob is unreachable from nucleus.yml (QCD-FW-5)", want)
		}
	}
}

// The derived default must be per-instance and traceable — never the shared
// literal every process used to carry.
func TestDefaultOutboxLeaseOwnerIsPerInstance(t *testing.T) {
	owner := defaultOutboxLeaseOwner()
	if owner == "nucleus-app" {
		t.Fatal("default lease owner is still the shared constant \"nucleus-app\" (QCD-FW-5)")
	}
	if !strings.HasPrefix(owner, "nucleus-") {
		t.Errorf("lease owner %q should keep the nucleus- prefix for operator recognition", owner)
	}
	if host, err := os.Hostname(); err == nil && host != "" && !strings.Contains(owner, host) {
		t.Errorf("lease owner %q does not carry the hostname %q — traceability was the point", owner, host)
	}
}
