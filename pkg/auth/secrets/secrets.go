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
	"context"
	"fmt"
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

// Scheme prefixes recognised by reference strings.
const (
	schemeEnv   = "env:"
	schemeAWSSM = "aws-sm:"
)

// HasManagedScheme reports whether ref names a managed secret store
// (anything other than a plain env-var reference). App.New uses this to
// decide whether to construct the AWS resolver at all — if no key
// references a managed store, the SDK client is never built and no
// cloud credential chain is touched.
func HasManagedScheme(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), schemeAWSSM)
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

// Chain tries each resolver in order, routing a reference to the first
// resolver whose scheme matches. A bare or "env:" reference goes to the
// EnvResolver; an "aws-sm:" reference goes to the AWS resolver. If no
// resolver in the chain handles the scheme, Resolve returns an error
// naming the unknown scheme.
//
// Chain is the type App.New hands to jwt_setup.go: it always contains
// an EnvResolver and, when at least one key uses a managed scheme, an
// AWSSecretsManagerResolver.
type Chain struct {
	env EnvResolver
	aws Resolver // nil when no key references a managed store
}

// NewChain builds a resolver chain. awsResolver may be nil — pass nil
// when no JWT key references a managed-store scheme, so the AWS SDK is
// never linked into the running process's credential path.
func NewChain(awsResolver Resolver) *Chain {
	return &Chain{aws: awsResolver}
}

// Resolve dispatches ref to the resolver that owns its scheme.
func (c *Chain) Resolve(ctx context.Context, ref string) ([]byte, error) {
	trimmed := strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(trimmed, schemeAWSSM):
		if c.aws == nil {
			return nil, fmt.Errorf("secrets: reference %q uses the aws-sm scheme but no AWS resolver is configured", ref)
		}
		return c.aws.Resolve(ctx, trimmed)
	default:
		// Bare names and "env:" references both go to the env resolver.
		return c.env.Resolve(ctx, trimmed)
	}
}
