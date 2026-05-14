package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fakeSM is a stand-in for *secretsmanager.Client. It satisfies the
// unexported secretsManagerAPI interface, so the resolver can be
// exercised without an AWS account or network.
type fakeSM struct {
	// byID maps a secret id to its SecretString. A missing id triggers
	// the err path.
	byID map[string]string
	// forceErr, when non-nil, is returned regardless of id.
	forceErr error
	// nilString, when true, returns an output with SecretString == nil
	// (the binary-secret case).
	nilString bool
	lastID    string
}

func (f *fakeSM) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput,
	_ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if in.SecretId != nil {
		f.lastID = *in.SecretId
	}
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	if f.nilString {
		return &secretsmanager.GetSecretValueOutput{SecretString: nil}, nil
	}
	val, ok := f.byID[*in.SecretId]
	if !ok {
		return nil, errors.New("ResourceNotFoundException: no such secret")
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: &val}, nil
}

func newTestAWSResolver(f *fakeSM) *AWSSecretsManagerResolver {
	return &AWSSecretsManagerResolver{api: f}
}

func TestAWSResolver_PlainSecret(t *testing.T) {
	f := &fakeSM{byID: map[string]string{"jwt/signing-key": "-----BEGIN PRIVATE KEY-----\n...\n"}}
	r := newTestAWSResolver(f)

	got, err := r.Resolve(context.Background(), "aws-sm:jwt/signing-key")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "-----BEGIN PRIVATE KEY-----\n...\n" {
		t.Fatalf("unexpected secret bytes: %q", got)
	}
	if f.lastID != "jwt/signing-key" {
		t.Fatalf("expected SecretId=jwt/signing-key, got %q", f.lastID)
	}
}

func TestAWSResolver_JSONKeyFragment(t *testing.T) {
	f := &fakeSM{byID: map[string]string{
		"jwt/keys": `{"current":"secret-A","previous":"secret-B"}`,
	}}
	r := newTestAWSResolver(f)

	got, err := r.Resolve(context.Background(), "aws-sm:jwt/keys#current")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "secret-A" {
		t.Fatalf("expected secret-A, got %q", got)
	}
}

func TestAWSResolver_JSONKeyMissing(t *testing.T) {
	f := &fakeSM{byID: map[string]string{"jwt/keys": `{"current":"secret-A"}`}}
	r := newTestAWSResolver(f)

	if _, err := r.Resolve(context.Background(), "aws-sm:jwt/keys#nonexistent"); err == nil {
		t.Fatal("expected error for missing JSON key")
	}
}

func TestAWSResolver_FragmentOnNonJSONSecret(t *testing.T) {
	f := &fakeSM{byID: map[string]string{"jwt/keys": "not-json-at-all"}}
	r := newTestAWSResolver(f)

	if _, err := r.Resolve(context.Background(), "aws-sm:jwt/keys#current"); err == nil {
		t.Fatal("expected error when fragment is requested on a non-JSON secret")
	}
}

func TestAWSResolver_MissingSecret(t *testing.T) {
	f := &fakeSM{byID: map[string]string{}}
	r := newTestAWSResolver(f)

	if _, err := r.Resolve(context.Background(), "aws-sm:does/not/exist"); err == nil {
		t.Fatal("expected error for a secret id the store does not have")
	}
}

func TestAWSResolver_BinarySecretRejected(t *testing.T) {
	f := &fakeSM{nilString: true}
	r := newTestAWSResolver(f)

	if _, err := r.Resolve(context.Background(), "aws-sm:binary/secret"); err == nil {
		t.Fatal("expected error for a secret with no SecretString (binary)")
	}
}

func TestAWSResolver_EmptySecretValue(t *testing.T) {
	f := &fakeSM{byID: map[string]string{"jwt/empty": ""}}
	r := newTestAWSResolver(f)

	if _, err := r.Resolve(context.Background(), "aws-sm:jwt/empty"); err == nil {
		t.Fatal("expected error for an empty SecretString")
	}
}

func TestAWSResolver_EmptyJSONKeyValue(t *testing.T) {
	f := &fakeSM{byID: map[string]string{"jwt/keys": `{"current":""}`}}
	r := newTestAWSResolver(f)

	if _, err := r.Resolve(context.Background(), "aws-sm:jwt/keys#current"); err == nil {
		t.Fatal("expected error for an empty JSON-key value")
	}
}

func TestAWSResolver_PropagatesSDKError(t *testing.T) {
	sentinel := errors.New("ThrottlingException: rate exceeded")
	f := &fakeSM{forceErr: sentinel}
	r := newTestAWSResolver(f)

	_, err := r.Resolve(context.Background(), "aws-sm:jwt/signing-key")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected the SDK error to be wrapped and propagated, got %v", err)
	}
}

func TestParseAWSSMRef(t *testing.T) {
	cases := []struct {
		ref     string
		id      string
		jsonKey string
		wantErr bool
	}{
		{"aws-sm:my/secret", "my/secret", "", false},
		{"aws-sm:my/secret#field", "my/secret", "field", false},
		{"  aws-sm:my/secret#field  ", "my/secret", "field", false},
		{"aws-sm:", "", "", true},
		{"aws-sm:#field", "", "", true},
		{"aws-sm:my/secret#", "", "", true},
		{"env:NOT_AWS", "", "", true},
	}
	for _, c := range cases {
		id, jsonKey, err := parseAWSSMRef(c.ref)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseAWSSMRef(%q): expected error, got id=%q key=%q", c.ref, id, jsonKey)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAWSSMRef(%q): unexpected error %v", c.ref, err)
			continue
		}
		if id != c.id || jsonKey != c.jsonKey {
			t.Errorf("parseAWSSMRef(%q) = (%q, %q), want (%q, %q)", c.ref, id, jsonKey, c.id, c.jsonKey)
		}
	}
}

// Compile-time proof that the real SDK client satisfies the narrowed
// interface — if AWS changes the GetSecretValue signature, this fails
// to build and we find out at CI time, not in production.
var _ secretsManagerAPI = (*secretsmanager.Client)(nil)
