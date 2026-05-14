package secrets

import (
	"context"
	"testing"
)

func TestEnvResolver_BareAndPrefixedNames(t *testing.T) {
	t.Setenv("NUCLEUS_SECRETS_TEST", "hunter2")

	var r EnvResolver
	for _, ref := range []string{"NUCLEUS_SECRETS_TEST", "env:NUCLEUS_SECRETS_TEST"} {
		got, err := r.Resolve(context.Background(), ref)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", ref, err)
		}
		if string(got) != "hunter2" {
			t.Fatalf("Resolve(%q) = %q, want hunter2", ref, got)
		}
	}
}

func TestEnvResolver_EmptyAndMissing(t *testing.T) {
	var r EnvResolver
	if _, err := r.Resolve(context.Background(), "env:"); err == nil {
		t.Fatal("empty env reference should error")
	}
	if _, err := r.Resolve(context.Background(), "NUCLEUS_SECRETS_DEFINITELY_UNSET"); err == nil {
		t.Fatal("unset env var should error")
	}
	if _, err := r.Resolve(context.Background(), "has spaces"); err == nil {
		t.Fatal("malformed env reference should error")
	}
}

func TestHasManagedScheme(t *testing.T) {
	cases := map[string]bool{
		"env:FOO":              false,
		"FOO":                  false,
		"aws-sm:my/secret":     true,
		"  aws-sm:my/secret  ": true,
		"":                     false,
	}
	for ref, want := range cases {
		if got := HasManagedScheme(ref); got != want {
			t.Errorf("HasManagedScheme(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestChain_RoutesByScheme(t *testing.T) {
	t.Setenv("NUCLEUS_CHAIN_TEST", "from-env")

	awsStub := resolverFunc(func(_ context.Context, ref string) ([]byte, error) {
		return []byte("from-aws:" + ref), nil
	})
	chain := NewChain(awsStub)

	// Bare + env: → EnvResolver.
	got, err := chain.Resolve(context.Background(), "NUCLEUS_CHAIN_TEST")
	if err != nil || string(got) != "from-env" {
		t.Fatalf("bare ref: got %q err %v", got, err)
	}

	// aws-sm: → AWS resolver.
	got, err = chain.Resolve(context.Background(), "aws-sm:my/secret")
	if err != nil || string(got) != "from-aws:aws-sm:my/secret" {
		t.Fatalf("aws-sm ref: got %q err %v", got, err)
	}
}

func TestChain_AWSSchemeWithoutResolver(t *testing.T) {
	chain := NewChain(nil) // no AWS resolver configured
	if _, err := chain.Resolve(context.Background(), "aws-sm:my/secret"); err == nil {
		t.Fatal("aws-sm reference with no AWS resolver in the chain should error")
	}
}

// resolverFunc adapts a function to the Resolver interface for tests.
type resolverFunc func(context.Context, string) ([]byte, error)

func (f resolverFunc) Resolve(ctx context.Context, ref string) ([]byte, error) {
	return f(ctx, ref)
}

var _ Resolver = resolverFunc(nil)
