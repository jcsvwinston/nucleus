package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// secretsManagerAPI is the slice of the AWS Secrets Manager SDK that the
// resolver actually uses. The real *secretsmanager.Client satisfies it;
// tests substitute a fake. Confining the SDK dependency to this one
// interface keeps AWS types out of every exported signature in the
// package — the dependency firewall (contracts/firewall_test.go) and
// ADR-005 require it.
type secretsManagerAPI interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput,
		optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// AWSSecretsManagerResolver resolves "aws-sm:<secret-id>" and
// "aws-sm:<secret-id>#<json-key>" references via AWS Secrets Manager.
//
// The "#<json-key>" form is for secrets stored as a JSON object — a
// common pattern when one Secrets Manager entry holds several related
// values. Without the fragment the whole SecretString is returned.
//
// Construct it with NewAWSSecretsManagerResolver, which wires the real
// SDK client using the default AWS credential chain (env vars, shared
// config, IAM role, etc.). The returned value is the Resolver
// interface — callers never see an AWS SDK type.
type AWSSecretsManagerResolver struct {
	api secretsManagerAPI
}

// NewAWSSecretsManagerResolver builds a resolver backed by the AWS SDK.
// It loads the default AWS configuration (the standard credential and
// region resolution chain). It returns the Resolver interface so the
// concrete type — and the AWS SDK — never appear in a caller's
// signatures.
//
// This is the only function in the framework that constructs an AWS
// SDK client. It is called lazily by App.New: only when at least one
// jwt_keys[] entry uses an "aws-sm:" reference. Deployments that do not
// use AWS Secrets Manager never reach this code and never trigger AWS
// credential resolution.
func NewAWSSecretsManagerResolver(ctx context.Context) (Resolver, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("secrets: load AWS config: %w", err)
	}
	return &AWSSecretsManagerResolver{
		api: secretsmanager.NewFromConfig(cfg),
	}, nil
}

// Resolve fetches the secret named by an "aws-sm:" reference.
//
// Reference forms:
//
//   - "aws-sm:my/secret"            → the raw SecretString.
//   - "aws-sm:my/secret#signing"    → the "signing" field of a secret
//     whose SecretString is a JSON object.
func (r *AWSSecretsManagerResolver) Resolve(ctx context.Context, ref string) ([]byte, error) {
	id, jsonKey, err := parseAWSSMRef(ref)
	if err != nil {
		return nil, err
	}

	out, err := r.api.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &id,
	})
	if err != nil {
		return nil, fmt.Errorf("secrets: GetSecretValue %q: %w", id, err)
	}
	if out.SecretString == nil {
		// Binary secrets are not supported for JWT key material — every
		// supported algorithm wants either a UTF-8 HMAC secret or a PEM
		// document, both of which are text.
		return nil, fmt.Errorf("secrets: AWS secret %q has no SecretString (binary secrets are not supported)", id)
	}
	raw := *out.SecretString

	if jsonKey == "" {
		if raw == "" {
			return nil, fmt.Errorf("secrets: AWS secret %q resolved to an empty value", id)
		}
		return []byte(raw), nil
	}

	// Fragment form: the SecretString must be a JSON object and the
	// fragment names one of its string-valued keys.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, fmt.Errorf("secrets: AWS secret %q is not a JSON object but reference requested key %q: %w", id, jsonKey, err)
	}
	rawVal, ok := obj[jsonKey]
	if !ok {
		return nil, fmt.Errorf("secrets: AWS secret %q has no JSON key %q", id, jsonKey)
	}
	var strVal string
	if err := json.Unmarshal(rawVal, &strVal); err != nil {
		return nil, fmt.Errorf("secrets: AWS secret %q key %q is not a JSON string: %w", id, jsonKey, err)
	}
	if strVal == "" {
		return nil, fmt.Errorf("secrets: AWS secret %q key %q resolved to an empty value", id, jsonKey)
	}
	return []byte(strVal), nil
}

// parseAWSSMRef splits "aws-sm:<id>[#<json-key>]" into its parts. The
// scheme prefix is required; callers route to this resolver based on
// that prefix, so an absent prefix is a programming error worth
// surfacing rather than silently tolerating.
func parseAWSSMRef(ref string) (id, jsonKey string, err error) {
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, schemeAWSSM) {
		return "", "", fmt.Errorf("secrets: %q is not an aws-sm reference", ref)
	}
	body := strings.TrimPrefix(trimmed, schemeAWSSM)
	if body == "" {
		return "", "", fmt.Errorf("secrets: aws-sm reference %q has no secret id", ref)
	}
	if i := strings.IndexByte(body, '#'); i >= 0 {
		id = strings.TrimSpace(body[:i])
		jsonKey = strings.TrimSpace(body[i+1:])
		if id == "" {
			return "", "", fmt.Errorf("secrets: aws-sm reference %q has an empty secret id", ref)
		}
		if jsonKey == "" {
			return "", "", fmt.Errorf("secrets: aws-sm reference %q has a '#' but no JSON key after it", ref)
		}
		return id, jsonKey, nil
	}
	return strings.TrimSpace(body), "", nil
}
