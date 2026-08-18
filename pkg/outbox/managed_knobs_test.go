// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-5 (external coverage demo, 2026-08-17): NewManagedOutbox hardcoded
// MissingRoutePolicy=error with no knob on ManagedConfig, and pkg/app wired
// LeaseOwner="nucleus-app" — identical for every instance. Any process
// sharing the outbox table without a bridge registered for some topic
// LEASES and FAILS messages that belong to another process (verified by the
// demo: a stray process produced attempts=2 plus timeouts where the
// homogeneous fleet gives attempts=1), stealing them from the instance that
// can actually deliver, and the shared owner string destroys traceability.
package outbox

import (
	"reflect"
	"testing"
)

// ManagedConfig must expose the missing-route policy instead of pinning
// "error" internally: a deliberately heterogeneous fleet (only some
// processes register bridges) needs "ignore" so unrouted messages are left
// for the instance that owns them.
func TestManagedConfigExposesMissingRoutePolicy(t *testing.T) {
	if _, ok := reflect.TypeOf(ManagedConfig{}).FieldByName("MissingRoutePolicy"); !ok {
		t.Fatal("ManagedConfig has no MissingRoutePolicy field — the policy is hardcoded to \"error\" and a mixed fleet leases-and-fails messages it cannot deliver (QCD-FW-5)")
	}
}
