package app

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
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
func buildJWTManager(cfg *Config) (*auth.JWTManager, error) {
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
		return auth.NewJWTManager(secret, expiry, issuer), nil
	}

	keys := make([]auth.SigningKey, 0, len(cfg.JWTKeys))
	for i, spec := range cfg.JWTKeys {
		key, err := loadJWTKey(spec)
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

// defaultJWTExpiry matches the value `defaults()` in config.go uses to
// stamp on Config.JWTExpiry, kept in sync with this builder so a Config
// value of zero produces a token expiry of 24h rather than the zero-
// value (immediate expiry).
const defaultJWTExpiry = 24 * time.Hour

func loadJWTKey(spec JWTKeySpec) (auth.SigningKey, error) {
	kid := strings.TrimSpace(spec.KID)
	if kid == "" {
		return auth.SigningKey{}, errors.New("kid is required")
	}
	switch strings.ToUpper(strings.TrimSpace(spec.Algorithm)) {
	case "HS256":
		if spec.SecretEnv == "" {
			return auth.SigningKey{}, errors.New("HS256 requires secret_env")
		}
		raw := os.Getenv(spec.SecretEnv)
		if raw == "" {
			return auth.SigningKey{}, fmt.Errorf("HS256 secret_env %q resolved to an empty value", spec.SecretEnv)
		}
		return auth.SigningKey{
			KID:        kid,
			Algorithm:  auth.HS256,
			HMACSecret: []byte(raw),
		}, nil

	case "RS256":
		pemBytes, err := loadRSAPEMBytes(spec)
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

	default:
		return auth.SigningKey{}, fmt.Errorf("unsupported algorithm %q (want HS256 or RS256)", spec.Algorithm)
	}
}

func loadRSAPEMBytes(spec JWTKeySpec) ([]byte, error) {
	switch {
	case spec.PemEnv != "" && spec.PemPath != "":
		return nil, errors.New("RS256 keys must set exactly one of pem_path or pem_env, not both")
	case spec.PemEnv != "":
		raw := os.Getenv(spec.PemEnv)
		if raw == "" {
			return nil, fmt.Errorf("RS256 pem_env %q resolved to an empty value", spec.PemEnv)
		}
		return []byte(raw), nil
	case spec.PemPath != "":
		data, err := os.ReadFile(spec.PemPath)
		if err != nil {
			return nil, fmt.Errorf("read RS256 pem_path %q: %w", spec.PemPath, err)
		}
		return data, nil
	default:
		return nil, errors.New("RS256 requires pem_path or pem_env")
	}
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("RS256 key material is not a valid PEM block")
	}
	// Trailing PEM blocks (e.g. a key file accidentally concatenated
	// with a certificate) are a common operator mistake. Surface it
	// explicitly rather than silently ignoring everything past the
	// first block.
	if trimmed := strings.TrimSpace(string(rest)); trimmed != "" {
		return nil, errors.New("RS256 PEM contains trailing content after the first block (combined key+cert file?)")
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
