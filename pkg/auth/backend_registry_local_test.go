package auth

import (
	"strings"
	"testing"
)

// AN-05: `auth_backends: [local]` is the configuration the README leads
// with, but until an application passes app.WithUserProvider the name
// "local" is not in the registry — and the generic unknown-backend error
// pointed at auth.RegisterBackend and the published directory backends,
// neither of which is the remedy. The fix for "local" is one specific
// option; the error must name it.
func TestUnknownBackendErrorNamesWithUserProviderForLocal(t *testing.T) {
	_, err := NewChainFrom(ChainConfig{Backends: []string{"local"}})
	if err == nil {
		t.Skip("a backend named local is registered in this process; the remedy path is unreachable")
	}
	msg := err.Error()
	if !strings.Contains(msg, "WithUserProvider") {
		t.Fatalf("unknown-backend error for %q does not mention app.WithUserProvider:\n%s", "local", msg)
	}
	if !strings.Contains(msg, `"local"`) {
		t.Fatalf("unknown-backend error does not name the backend:\n%s", msg)
	}
}
