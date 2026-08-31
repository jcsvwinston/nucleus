// Package secrets resolves opaque reference strings into raw secret
// bytes for the auth layer — JWT signing keys, primarily.
//
// A reference is a short string with an optional scheme prefix:
//
//   - "env:NAME" or a bare "NAME" — read environment variable NAME.
//   - "aws-sm:<secret-id>"        — read an AWS Secrets Manager secret.
//   - "aws-sm:<secret-id>#<key>"  — read one JSON key out of an AWS
//     Secrets Manager secret whose value is a JSON object.
//
// The package exists so the framework can pull key material from a
// managed secret store without that store's SDK leaking into any
// stable pkg/* surface (see ADR-005 and contracts/firewall_test.go).
// Every constructor returns the Resolver interface; no exported symbol
// names a third-party type.
package secrets

import (
	"sync"

	"context"
	"fmt"
	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"os"
	"strings"
)

// Resolver turns an opaque reference string into raw secret bytes.
// Implementations must be safe for concurrent use.
type Resolver interface {
	// Resolve returns the secret bytes for ref, or an error if the
	// reference is malformed, the secret is missing, or the backing
	// store is unreachable. A missing secret and an unreachable store
	// are both errors — callers decide whether to fail open or closed.
	Resolve(ctx context.Context, ref string) ([]byte, error)
}

// Scheme prefixes recognised by reference strings. Only the environment
// scheme is built in; managed stores register their own (see registry.go).
const (
	schemeEnv = "env:"
)

// HasManagedScheme reports whether ref names a managed secret store
// (anything other than a plain env-var reference). App.New uses this to
// decide whether to build a managed resolver at all — if no key references
// one, no client is constructed and no cloud credential chain is touched.
func HasManagedScheme(ref string) bool {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" || strings.HasPrefix(trimmed, schemeEnv) {
		return false
	}
	_, _, ok := lookupScheme(trimmed)
	return ok
}

// EnvResolver resolves "env:NAME" and bare "NAME" references from the
// process environment. It has no dependencies beyond the standard
// library and is always part of the resolver chain.
type EnvResolver struct{}

// Resolve reads the named environment variable. A bare reference (no
// scheme prefix) is treated as an env-var name, preserving the
// historical behaviour of JWTKeySpec.SecretEnv / PemEnv.
func (EnvResolver) Resolve(_ context.Context, ref string) ([]byte, error) {
	name := strings.TrimSpace(ref)
	name = strings.TrimPrefix(name, schemeEnv)
	if name == "" {
		return nil, fmt.Errorf("secrets: empty env reference %q", ref)
	}
	if strings.ContainsAny(name, " \t") {
		return nil, fmt.Errorf("secrets: malformed env reference %q", ref)
	}
	val := os.Getenv(name)
	if val == "" {
		return nil, fmt.Errorf("secrets: env var %q resolved to an empty value", name)
	}
	return []byte(val), nil
}

// Chain routes a reference to the resolver that owns its scheme. A bare or
// "env:" reference goes to the EnvResolver; anything else goes to whoever
// registered that scheme. If no resolver owns it, Resolve says so and names
// what IS registered — the error is where an operator learns that the
// managed store they configured lives in a module they have not imported.
type Chain struct {
	env      EnvResolver
	resolved map[string]Resolver // built lazily, one per scheme actually used
	mu       sync.Mutex
}

// NewChain builds a resolver chain over the registered schemes.
func NewChain() *Chain {
	return &Chain{resolved: map[string]Resolver{}}
}

// Resolve dispatches ref to the resolver that owns its scheme.
func (c *Chain) Resolve(ctx context.Context, ref string) ([]byte, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" || strings.HasPrefix(trimmed, schemeEnv) || !strings.Contains(trimmed, ":") {
		// Bare names and "env:" references both go to the env resolver.
		return c.env.Resolve(ctx, trimmed)
	}

	scheme, factory, ok := lookupScheme(trimmed)
	if !ok {
		if p, ours := knownproviders.SecretsResolver(schemeOf(trimmed)); ours {
			return nil, fmt.Errorf("secrets: reference %q uses the %q scheme, which ships as its own module and is not imported yet.\n\n"+
				"\tAdd it to your build:\n\n%s",
				ref, schemeOf(trimmed), p.InstallHint())
		}
		return nil, fmt.Errorf("secrets: reference %q uses an unregistered scheme (registered: %s) — register a managed store with secrets.RegisterResolver",
			ref, strings.Join(append([]string{schemeEnv}, RegisteredSchemes()...), ", "))
	}

	c.mu.Lock()
	r, built := c.resolved[scheme]
	if !built {
		var err error
		r, err = factory(ctx)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("secrets: building the %q resolver: %w", scheme, err)
		}
		c.resolved[scheme] = r
	}
	c.mu.Unlock()

	return r.Resolve(ctx, trimmed)
}

// schemeOf returns the "prefix:" part of a reference, for error messages.
func schemeOf(ref string) string {
	if i := strings.Index(ref, ":"); i >= 0 {
		return ref[:i+1]
	}
	return ref
}
