// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-5, app side: the outbox configuration must let the operator set the
// lease owner and the missing-route policy from nucleus.yml, and the DEFAULT
// lease owner must be per-instance (derived from the hostname/pid) instead
// of the constant "nucleus-app" every process shared — which made lease rows
// untraceable and let any co-tenant process steal messages it cannot deliver.
package app

import (
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
