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

// CAT-01: a directory backend this project publishes, configured and not
// linked, is refused with the command that installs it beside the recipe.
func TestUnknownBackendErrorNamesNucleusAddForAPublishedBackend(t *testing.T) {
	_, err := NewChainFrom(ChainConfig{Backends: []string{"ldap"}})
	if err == nil {
		t.Skip("a backend named ldap is registered in this process; the refusal is unreachable")
	}
	for _, want := range []string{"go get github.com/jcsvwinston/nucleus/providers/ldap", "nucleus add ldap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}
