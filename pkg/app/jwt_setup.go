package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/secrets"
)

// buildJWTManager constructs the JWTManager that App.New attaches to
// the App struct.
//
// Two paths coexist:
//
//   - Legacy single-secret HS256: `jwt_secret` is set in nucleus.yml
//     and the new `jwt_keys` slice is empty. The manager is built via
//     `auth.NewJWTManager(secret, expiry, issuer)`; tokens carry no
//     `kid` header.
//   - Multi-key with rotation: at least one entry in `jwt_keys` and a
//     non-empty `jwt_current_kid`. The manager is built via
//     `auth.NewJWTManagerFromKeys` and supports rotation via
//     `RotateKey` / `RemoveKey` at runtime.
//
// When both `jwt_secret` and `jwt_keys` are set, `jwt_keys` wins —
// keeping `jwt_secret` populated does not hurt (it is simply ignored)
// so existing nucleus.yml files do not need editing to migrate.
//
// When neither is set, buildJWTManager returns nil with no error. App.New
// records that fact and leaves App.JWT == nil. Calling code that depends
// on JWT must check `a.JWT != nil` and surface a clear error rather than
// relying on a phantom manager — an empty HMAC secret would otherwise
// produce a globally-known signing key, which is a security footgun.
//
// Key material in `secret_env` / `pem_env` is resolved through a
// secrets.Resolver chain (see ADR-005). A bare name or `env:NAME` reads
// the process environment; an `aws-sm:<id>` reference reads AWS Secrets
// Manager. The AWS resolver is constructed lazily — only when at least
// one jwt_keys[] entry uses an `aws-sm:` reference — so deployments that
// do not use AWS Secrets Manager never trigger AWS credential resolution.
func buildJWTManager(ctx context.Context, cfg *Config) (*auth.JWTManager, error) {
	expiry := cfg.JWTExpiry
	if expiry <= 0 {
		expiry = defaultJWTExpiry
	}
	issuer := strings.TrimSpace(cfg.JWTIssuer)

	if len(cfg.JWTKeys) == 0 {
		secret := strings.TrimSpace(cfg.JWTSecret)
		if secret == "" {
			// No keys configured at all. Return nil rather than a
			// manager with HMACSecret=[]byte(""), which would sign
			// tokens that any other unconfigured Nucleus instance
			// could forge.
			return nil, nil
		}
		// HS256 signs with the raw secret bytes; a short secret yields a
		// weak, brute-forceable signing key. Require at least 32 bytes
		// (256 bits) so the key matches the HMAC-SHA256 output width.
		// Fail fast at construction rather than shipping a forgeable token.
		if len(secret) < minJWTSecretBytes {
			return nil, fmt.Errorf("app: jwt_secret is too short (%d bytes); HS256 requires at least %d bytes — use a longer secret or configure jwt_keys[]", len(secret), minJWTSecretBytes)
		}
		return auth.NewJWTManager(secret, expiry, issuer), nil
	}

	resolver, err := buildKeyMaterialResolver(ctx, cfg.JWTKeys)
	if err != nil {
		return nil, err
	}

	keys := make([]auth.SigningKey, 0, len(cfg.JWTKeys))
	for i, spec := range cfg.JWTKeys {
		key, err := loadJWTKey(ctx, resolver, spec)
		if err != nil {
			return nil, fmt.Errorf("app: jwt_keys[%d] (%q): %w", i, spec.KID, err)
		}
		keys = append(keys, key)
	}

	current := strings.TrimSpace(cfg.JWTCurrentKID)
	if current == "" {
		return nil, errors.New("app: jwt_current_kid is required when jwt_keys is non-empty")
	}

	mgr, err := auth.NewJWTManagerFromKeys(keys, current, expiry, issuer)
	if err != nil {
		return nil, fmt.Errorf("app: build jwt manager: %w", err)
	}
	return mgr, nil
}

// buildKeyMaterialResolver constructs the secrets.Resolver chain used to
// resolve `secret_env` / `pem_env` references. The AWS Secrets Manager
// resolver is only constructed when at least one key reference uses the
// `aws-sm:` scheme — otherwise the chain is env-var-only and no AWS SDK
// client (and no AWS credential resolution) is touched.
func buildKeyMaterialResolver(ctx context.Context, specs []JWTKeySpec) (secrets.Resolver, error) {
	needsAWS := false
	for _, spec := range specs {
		if secrets.HasManagedScheme(spec.SecretEnv) || secrets.HasManagedScheme(spec.PemEnv) {
			needsAWS = true
			break
		}
	}

	var awsResolver secrets.Resolver
	if needsAWS {
		r, err := secrets.NewAWSSecretsManagerResolver(ctx)
		if err != nil {
			return nil, fmt.Errorf("app: build AWS Secrets Manager resolver: %w", err)
		}
		awsResolver = r
	}
	return secrets.NewChain(awsResolver), nil
}

// defaultJWTExpiry matches the value `defaults()` in config.go uses to
// stamp on Config.JWTExpiry, kept in sync with this builder so a Config
// value of zero produces a token expiry of 24h rather than the zero-
// value (immediate expiry).
const defaultJWTExpiry = 24 * time.Hour

// minJWTSecretBytes is the minimum accepted length for a legacy single-secret
// HS256 `jwt_secret`. HS256 keys shorter than the 256-bit hash width weaken
// the signature; buildJWTManager rejects them at construction.
const minJWTSecretBytes = 32

func loadJWTKey(ctx context.Context, resolver secrets.Resolver, spec JWTKeySpec) (auth.SigningKey, error) {
	kid := strings.TrimSpace(spec.KID)
	if kid == "" {
		return auth.SigningKey{}, errors.New("kid is required")
	}
	switch strings.ToUpper(strings.TrimSpace(spec.Algorithm)) {
	case "HS256":
		if strings.TrimSpace(spec.SecretEnv) == "" {
			return auth.SigningKey{}, errors.New("HS256 requires secret_env")
		}
		raw, err := resolver.Resolve(ctx, spec.SecretEnv)
		if err != nil {
			return auth.SigningKey{}, fmt.Errorf("HS256 secret_env: %w", err)
		}
		return auth.SigningKey{
			KID:        kid,
			Algorithm:  auth.HS256,
			HMACSecret: raw,
		}, nil

	case "RS256":
		pemBytes, err := loadPEMBytes(ctx, resolver, spec, "RS256")
		if err != nil {
			return auth.SigningKey{}, err
		}
		priv, err := parseRSAPrivateKey(pemBytes)
		if err != nil {
			return auth.SigningKey{}, err
		}
		return auth.SigningKey{
			KID:        kid,
			Algorithm:  auth.RS256,
			RSAPrivate: priv,
		}, nil

	case "ES256":
		pemBytes, err := loadPEMBytes(ctx, resolver, spec, "ES256")
		if err != nil {
			return auth.SigningKey{}, err
		}
		priv, err := parseECDSAPrivateKey(pemBytes)
		if err != nil {
			return auth.SigningKey{}, err
		}
		return auth.SigningKey{
			KID:          kid,
			Algorithm:    auth.ES256,
			ECDSAPrivate: priv,
		}, nil

	default:
		return auth.SigningKey{}, fmt.Errorf("unsupported algorithm %q (want HS256, RS256 or ES256)", spec.Algorithm)
	}
}

// loadPEMBytes resolves the PEM key material for an asymmetric JWT key
// from exactly one of pem_path / pem_env. pem_path always reads a file
// from disk; pem_env is resolved through the secrets chain (a bare name
// or `env:NAME` reads the environment, `aws-sm:<id>` reads AWS Secrets
// Manager). algLabel ("RS256" / "ES256") is woven into error messages so
// operators see which key failed.
func loadPEMBytes(ctx context.Context, resolver secrets.Resolver, spec JWTKeySpec, algLabel string) ([]byte, error) {
	switch {
	case spec.PemEnv != "" && spec.PemPath != "":
		return nil, fmt.Errorf("%s keys must set exactly one of pem_path or pem_env, not both", algLabel)
	case spec.PemEnv != "":
		raw, err := resolver.Resolve(ctx, spec.PemEnv)
		if err != nil {
			return nil, fmt.Errorf("%s pem_env: %w", algLabel, err)
		}
		return raw, nil
	case spec.PemPath != "":
		data, err := os.ReadFile(spec.PemPath)
		if err != nil {
			return nil, fmt.Errorf("read %s pem_path %q: %w", algLabel, spec.PemPath, err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("%s requires pem_path or pem_env", algLabel)
	}
}

// decodeSinglePEMBlock decodes exactly one PEM block and rejects any
// trailing content — a key file accidentally concatenated with a
// certificate is a common operator mistake worth surfacing loudly.
func decodeSinglePEMBlock(pemBytes []byte, algLabel string) (*pem.Block, error) {
	block, rest := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%s key material is not a valid PEM block", algLabel)
	}
	if trimmed := strings.TrimSpace(string(rest)); trimmed != "" {
		return nil, fmt.Errorf("%s PEM contains trailing content after the first block (combined key+cert file?)", algLabel)
	}
	return block, nil
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, err := decodeSinglePEMBlock(pemBytes, "RS256")
	if err != nil {
		return nil, err
	}

	// Accept both PKCS#1 and PKCS#8 — PKCS#8 is what most modern tools
	// (openssl 3.x, kubectl, cert-manager) emit by default.
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("RS256 PEM is neither PKCS#1 nor PKCS#8: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("RS256 PEM is PKCS#8 but holds %T, expected *rsa.PrivateKey", parsed)
	}
	return rsaKey, nil
}

func parseECDSAPrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, err := decodeSinglePEMBlock(pemBytes, "ES256")
	if err != nil {
		return nil, err
	}

	// Accept both SEC1 (`EC PRIVATE KEY`) and PKCS#8 (`PRIVATE KEY`).
	// SEC1 is what `openssl ecparam -genkey` emits; PKCS#8 is the
	// modern default from `openssl genpkey`.
	var key *ecdsa.PrivateKey
	if sec1, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		key = sec1
	} else {
		parsed, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("ES256 PEM is neither SEC1 nor PKCS#8: %w", perr)
		}
		ecKey, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("ES256 PEM is PKCS#8 but holds %T, expected *ecdsa.PrivateKey", parsed)
		}
		key = ecKey
	}

	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("ES256 requires the P-256 curve, got %s", key.Curve.Params().Name)
	}
	return key, nil
}

// hasAsymmetricKey reports whether the given manager owns at least one
// RS256 (or future ECDSA) key — that is the gate App.New uses to
// decide whether to auto-mount the JWKS handler. An HS256-only manager
// would publish an empty `{"keys": []}` document, which is technically
// correct but offers no value to relying parties, so we skip the
// route in that case.
//
// O(n) over the manager's key map; called once at App.New time, never
// on the request path, so the cost is irrelevant in practice.
func hasAsymmetricKey(mgr *auth.JWTManager) bool {
	if mgr == nil {
		return false
	}
	set := mgr.JWKS()
	return len(set.Keys) > 0
}
