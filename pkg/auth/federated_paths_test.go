package auth

import (
	"strings"
	"testing"
)

// TestFederatedPaths_AreWhatTheDocsTellYouToMount pins the division of
// responsibility that QCD-FW-29 found undocumented — and, worse, documented
// backwards.
//
// The framework does NOT register these routes. It owns the flow (Begin
// issues the anti-forgery state and holds the pending sign-in, Complete
// verifies it before the provider is consulted); the application mounts the
// two handlers, because what happens after a successful callback is the
// application's decision.
//
// Nothing in the framework referenced these helpers — not even a test — so
// the startup log said "federated sign-in ready" and printed callback URLs
// that answered 404. An operator registered them with their identity
// provider and found out in production.
//
// What this test guards is the property the fix relies on: the URL logged at
// startup and registered with the provider is DERIVED from the same helpers
// the docs tell you to mount, so the two cannot drift.
func TestFederatedPaths_AreWhatTheDocsTellYouToMount(t *testing.T) {
	const instance = "corp"

	start := FederatedStartPath(instance)
	callback := FederatedCallbackPath(instance)

	if want := "/auth/corp/start"; start != want {
		t.Errorf("FederatedStartPath = %q, want %q", start, want)
	}
	if want := "/auth/corp/callback"; callback != want {
		t.Errorf("FederatedCallbackPath = %q, want %q", callback, want)
	}

	// The instance name is escaped, so a name that would otherwise break the
	// path cannot smuggle a segment in.
	if got := FederatedCallbackPath("a/b"); strings.Count(got, "/") != 3 {
		t.Errorf("the instance name must be escaped, got %q", got)
	}

	// CallbackURL must be the base plus exactly the path an application
	// mounts. This is the whole reason the docs say to use the helper: the
	// URL an operator registers with the identity provider has to be the one
	// their route serves.
	set := &FederatedSet{base: "https://app.example.com"}
	if got, want := set.CallbackURL(instance), "https://app.example.com"+callback; got != want {
		t.Errorf("CallbackURL = %q, want %q — the logged URL must match the mounted path", got, want)
	}
}
