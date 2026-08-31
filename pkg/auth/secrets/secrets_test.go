package secrets

import (
	"context"
	"strings"
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
	// HasManagedScheme answers over the REGISTERED schemes: before the cloud
	// resolvers became their own modules it hard-coded "aws-sm:", which meant
	// the framework had to know the name of every managed store to answer.
	const scheme = "hms-test:"
	if err := RegisterResolver(scheme, func(_ context.Context) (Resolver, error) {
		return resolverFunc(func(_ context.Context, _ string) ([]byte, error) { return nil, nil }), nil
	}); err != nil {
		t.Fatalf("RegisterResolver: %v", err)
	}

	cases := map[string]bool{
		"env:FOO":                     false,
		"FOO":                         false,
		"":                            false,
		scheme + "my/secret":          true,
		"  " + scheme + "my/secret  ": true,
		// Not registered in this binary: not a managed scheme as far as the
		// caller is concerned, which is what keeps App.New from building a
		// client for a store nobody wired.
		"aws-sm:my/secret": false,
	}
	for ref, want := range cases {
		if got := HasManagedScheme(ref); got != want {
			t.Errorf("HasManagedScheme(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestChain_RoutesByScheme(t *testing.T) {
	t.Setenv("NUCLEUS_CHAIN_TEST", "from-env")

	// A managed store registers the scheme it owns, the way the AWS module
	// does at init. Registering it here proves the routing without linking
	// any SDK into this test binary.
	const scheme = "test-sm:"
	if err := RegisterResolver(scheme, func(_ context.Context) (Resolver, error) {
		return resolverFunc(func(_ context.Context, ref string) ([]byte, error) {
			return []byte("from-managed:" + ref), nil
		}), nil
	}); err != nil {
		t.Fatalf("RegisterResolver: %v", err)
	}

	chain := NewChain()

	// Bare + env: → EnvResolver.
	got, err := chain.Resolve(context.Background(), "NUCLEUS_CHAIN_TEST")
	if err != nil || string(got) != "from-env" {
		t.Fatalf("bare ref: got %q err %v", got, err)
	}

	// Registered scheme → its resolver, built lazily on first use.
	got, err = chain.Resolve(context.Background(), scheme+"my/secret")
	if err != nil || string(got) != "from-managed:"+scheme+"my/secret" {
		t.Fatalf("managed ref: got %q err %v", got, err)
	}
}

// A scheme this project publishes as its own module must be answered with
// the import line, not with "unregistered": the reference is not wrong.
func TestChain_FirstPartySchemeNotImported_SaysHowToInstallIt(t *testing.T) {
	chain := NewChain()
	_, err := chain.Resolve(context.Background(), "aws-sm:my/secret")
	if err == nil {
		t.Fatal("an aws-sm reference with the module absent must fail, not fall back to the environment")
	}
	for _, want := range []string{"ships as its own module", "go get github.com/jcsvwinston/nucleus/providers/secrets-aws", "import _"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error must carry the install recipe (missing %q):\n%v", want, err)
		}
	}
}

func TestChain_UnknownSchemeNamesWhatIsRegistered(t *testing.T) {
	chain := NewChain()
	_, err := chain.Resolve(context.Background(), "vault:my/secret")
	if err == nil {
		t.Fatal("an unregistered scheme must fail")
	}
	if !strings.Contains(err.Error(), "vault:") || !strings.Contains(err.Error(), "env:") {
		t.Errorf("the error must name the unknown scheme and what IS registered: %v", err)
	}
}

// resolverFunc adapts a function to the Resolver interface for tests.
type resolverFunc func(context.Context, string) ([]byte, error)

func (f resolverFunc) Resolve(ctx context.Context, ref string) ([]byte, error) {
	return f(ctx, ref)
}

var _ Resolver = resolverFunc(nil)
