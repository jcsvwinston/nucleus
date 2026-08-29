package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/sessionstore"
)

// TestValidateSemantics_SessionStoreUsesTheRegistry pins that the semantic
// layer asks the registry which stores exist instead of consulting a
// hand-written enum.
//
// Layer 3 validated `session_store` against the literal {memory,sql,redis},
// so a store registered by an application — the whole point of
// auth.RegisterSessionStore — was accepted by app.New and rejected by
// nucleus.Run and by every one of the 38 CLI subcommands that go through
// LoadConfig:
//
//	auth.RegisteredSessionStores() = [memory redis reprostore sql]
//	A — app.New(cfg)                 -> err = <nil>
//	B — app.ValidateSemantics(cfg)   -> session_store "reprostore" is not one of [memory sql redis]
//
// The container builds it and the runner refuses it: the same file, two
// verdicts, which is exactly what ADR-010 §2 layers 3–4 exist to close.
//
// The three sibling seams of the same arc never had this enum:
// auth_backends only checks empty/duplicate, http_interceptors is not
// validated, and storage.provider passes verbatim. session_store was the
// only one left closed.
func TestValidateSemantics_SessionStoreUsesTheRegistry(t *testing.T) {
	const name = "reprostore"
	if err := auth.RegisterSessionStore(name, func(auth.SessionStoreParams) (auth.SessionStore, func(context.Context) error, error) {
		return nil, nil, nil
	}); err != nil {
		t.Fatalf("RegisterSessionStore: %v", err)
	}
	t.Cleanup(func() { sessionstore.Unregister(name) })

	if _, ok := sessionstore.Lookup(name); !ok {
		t.Fatalf("precondition: %q must be registered", name)
	}

	cfg := &Config{SessionStore: name}
	if err := ValidateSemantics(cfg); err != nil {
		t.Errorf("a REGISTERED session store must pass layer 3, got: %v", err)
	}

	// The built-ins keep passing.
	for _, builtin := range []string{"memory", "sql", "redis", "", "REDIS"} {
		if err := ValidateSemantics(&Config{SessionStore: builtin}); err != nil {
			t.Errorf("built-in session_store %q must pass: %v", builtin, err)
		}
	}

	// And an unknown name is still a boot-time error — the validation must
	// get looser, not disappear.
	err := ValidateSemantics(&Config{SessionStore: "no-existe"})
	if !errors.Is(err, ErrInvalidConfigValue) {
		t.Fatalf("an UNREGISTERED session store must still be rejected, got: %v", err)
	}
	// The message has to name what IS available, or the operator is left
	// guessing which names the registry knows.
	if !strings.Contains(err.Error(), name) {
		t.Errorf("the error must list the registered stores (missing %q): %v", name, err)
	}
}
