package storage

import (
	"fmt"
	"os"
	"strings"
)

// ProviderType identifies the storage backend.
type ProviderType string

const (
	ProviderS3    ProviderType = "s3"
	ProviderGCS   ProviderType = "gcs"
	ProviderAzure ProviderType = "azure"
	ProviderLocal ProviderType = "local"
)

// CredentialSource describes where to find a credential value.
// Supports multiple injection methods used in production environments.
type CredentialSource struct {
	// Value is the literal credential value (for testing or non-sensitive configs).
	Value string `koanf:"value"`

	// EnvVar reads the credential from an environment variable.
	// This is the primary method for production: orchestrators inject
	// secrets from Secret Manager into env vars before starting the container.
	// Example: "AWS_SECRET_ACCESS_KEY"
	EnvVar string `koanf:"env_var"`

	// File reads the credential from a file path.
	// Used for:
	// - GCS service account JSON key mounted as volume
	// - Kubernetes secrets mounted as files
	// - Azure managed identity token file
	// Example: "/etc/secrets/gcs-sa.json"
	File string `koanf:"file"`

	// SecretManager currently supports only env: references, for example
	// "env:MY_SECRET_KEY". Cloud Secret Manager SDK lookups are deliberately
	// not implemented yet; inject cloud-managed secrets into env vars or files.
	// Resolution happens at startup. The secret value is read once and cached.
	SecretManager string `koanf:"secret_manager"`
}

// Resolve returns the credential value from the configured source.
// Priority: Value > EnvVar > File > SecretManager.
// Only one source should be configured; the first non-empty wins.
func (cs *CredentialSource) Resolve() (string, error) {
	if cs == nil {
		return "", nil
	}

	// 1. Direct value
	if cs.Value != "" {
		return cs.Value, nil
	}

	// 2. Environment variable
	if cs.EnvVar != "" {
		val := os.Getenv(cs.EnvVar)
		if val == "" {
			return "", fmt.Errorf("storage: environment variable %q is empty or not set", cs.EnvVar)
		}
		return val, nil
	}

	// 3. File path
	if cs.File != "" {
		data, err := os.ReadFile(cs.File)
		if err != nil {
			return "", fmt.Errorf("storage: read credential file %q: %w", cs.File, err)
		}
		val := strings.TrimSpace(string(data))
		if val == "" {
			return "", fmt.Errorf("storage: credential file %q is empty", cs.File)
		}
		return val, nil
	}

	// 4. SecretManager — simple env var prefixed with "env:"
	if cs.SecretManager != "" {
		if strings.HasPrefix(cs.SecretManager, "env:") {
			envVar := strings.TrimPrefix(cs.SecretManager, "env:")
			val := os.Getenv(envVar)
			if val == "" {
				return "", fmt.Errorf("storage: secret manager env var %q is empty or not set", envVar)
			}
			return val, nil
		}
		// Cloud secret manager paths require SDK resolution
		// For now, return a clear error guiding the user to use env: prefix
		// or set the secret value in an environment variable directly.
		return "", fmt.Errorf("storage: SecretManager %q requires cloud SDK integration. Use 'env:VAR_NAME' prefix to read from environment variables injected by Secret Manager", cs.SecretManager)
	}

	return "", nil
}

// Config holds the complete storage configuration.
type Config struct {
	// Default visibility for new objects (private|public).
	DefaultVisibility Visibility `koanf:"default"`

	// Provider selects the storage backend (s3|gcs|azure|local).
	Provider ProviderType `koanf:"provider"`

	// PublicPaths maps public URL paths to storage key prefixes.
	// Example: "/media" -> "storage/public/media/"
	// Requests to /media/* are served from keys with that prefix.
	PublicPaths map[string]string `koanf:"public_paths"`

	// PublicURLBase is the base URL for public objects (CDN or direct provider).
	// Example: "https://cdn.example.com"
	PublicURLBase string `koanf:"public_url_base"`

	// S3 configuration
	S3 S3Config `koanf:"s3"`

	// GCS Configuration
	GCS GCSConfig `koanf:"gcs"`

	// Azure configuration
	Azure AzureConfig `koanf:"azure"`

	// Local configuration (development only)
	Local LocalConfig `koanf:"local"`

	// Cleanup config for temporary objects
	Cleanup CleanupConfig `koanf:"cleanup"`

	// CircuitBreaker, when Enabled, wraps remote provider operations
	// (Put/Get/Delete/Exists/List/SignedURL/Copy) with a pkg/circuit
	// breaker. The local provider is never wrapped — filesystem failures
	// are not the kind of outage circuit breakers are designed to
	// short-circuit. PublicURL is also not wrapped (pure string
	// composition).
	CircuitBreaker CircuitBreakerConfig `koanf:"circuit_breaker"`
}

// S3Config configures Amazon S3 or any S3-compatible provider (MinIO, R2, etc.).
type S3Config struct {
	// Endpoint is the S3 API endpoint. Empty = AWS S3.
	// For MinIO: "http://minio:9000"
	// For Cloudflare R2: "https://<account>.r2.cloudflarestorage.com"
	Endpoint string `koanf:"endpoint"`

	// Bucket is the default (private) bucket name.
	Bucket string `koanf:"bucket"`

	// Region is the AWS region.
	Region string `koanf:"region"`

	// AccessKeyID credential source.
	// Examples:
	//   env_var: AWS_ACCESS_KEY_ID          # From environment variable
	//   file: /etc/secrets/aws-access-key   # From mounted secret file
	//   value: AKIA...                       # Direct (not recommended for production)
	AccessKeyID CredentialSource `koanf:"access_key_id"`

	// SecretAccessKey credential source.
	// Examples:
	//   env_var: AWS_SECRET_ACCESS_KEY
	//   file: /etc/secrets/aws-secret-key
	SecretAccessKey CredentialSource `koanf:"secret_access_key"`

	// SessionToken credential source (for temporary STS credentials).
	//   env_var: AWS_SESSION_TOKEN
	SessionToken CredentialSource `koanf:"session_token"`

	// UsePathStyle forces path-style URLs instead of virtual-hosted style.
	// Required for MinIO and some other S3-compatible providers.
	UsePathStyle bool `koanf:"use_path_style"`

	// PublicBucket is an optional separate bucket for public objects.
	// When set, public objects are stored here instead of Bucket.
	PublicBucket string `koanf:"public_bucket"`
}

// GCSConfig configures Google Cloud Storage.
type GCSConfig struct {
	// Bucket is the default (private) bucket name.
	Bucket string `koanf:"bucket"`

	// CredentialsSource for the GCS service account.
	// When empty, uses Application Default Credentials (ADC).
	//
	// Examples for Cloud Run / GKE:
	//   env_var: GOOGLE_APPLICATION_CREDENTIALS  # Path injected by Secret Manager
	//   file: /etc/secrets/gcs-sa.json           # Volume-mounted secret
	//
	// Examples for workload identity (no credentials needed):
	//   (leave empty — ADC uses the GKE service account)
	CredentialsSource CredentialSource `koanf:"credentials"`

	// PublicBucket is an optional separate bucket for public objects.
	PublicBucket string `koanf:"public_bucket"`
}

// AzureConfig configures Azure Blob Storage.
type AzureConfig struct {
	// AccountName is the Azure storage account name.
	//   env_var: AZURE_ACCOUNT_NAME
	AccountName CredentialSource `koanf:"account_name"`

	// AccountKey credential source.
	//   env_var: AZURE_STORAGE_KEY
	//   secret_manager: env:AZURE_STORAGE_KEY
	AccountKey CredentialSource `koanf:"account_key"`

	// Container is the default (private) container name.
	Container string `koanf:"container"`

	// PublicContainer is the container for public objects.
	PublicContainer string `koanf:"public_container"`
}

// LocalConfig configures local filesystem storage (development only).
type LocalConfig struct {
	// Path is the root directory for all stored files.
	Path string `koanf:"path"`
}

// CleanupConfig configures automatic cleanup of temporary objects.
type CleanupConfig struct {
	// Enabled turns on background cleanup.
	Enabled bool `koanf:"enabled"`

	// Interval is how often to run cleanup (e.g. "1h").
	Interval string `koanf:"interval"`

	// Prefix is the key prefix for temporary objects (default: "_tmp/").
	Prefix string `koanf:"prefix"`

	// MaxAge is the maximum age before an object is deleted (e.g. "24h").
	MaxAge string `koanf:"max_age"`
}

// Validate checks the configuration for required fields.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("storage config is nil")
	}

	switch c.Provider {
	case ProviderS3:
		if c.S3.Bucket == "" {
			return fmt.Errorf("storage: s3.bucket is required")
		}
		// At least one credential source must be configured for non-local S3
		hasCreds := c.S3.AccessKeyID.EnvVar != "" || c.S3.AccessKeyID.File != "" || c.S3.AccessKeyID.Value != "" ||
			c.S3.SecretAccessKey.EnvVar != "" || c.S3.SecretAccessKey.File != "" || c.S3.SecretAccessKey.Value != ""
		if !hasCreds && !strings.HasPrefix(c.S3.Endpoint, "http://") {
			return fmt.Errorf("storage: s3 credentials required (set access_key_id and secret_access_key via env_var, file, or value)")
		}
	case ProviderGCS:
		if c.GCS.Bucket == "" {
			return fmt.Errorf("storage: gcs.bucket is required")
		}
	case ProviderAzure:
		if c.Azure.AccountName.EnvVar == "" && c.Azure.AccountName.Value == "" && c.Azure.AccountName.File == "" {
			return fmt.Errorf("storage: azure.account_name is required (set via env_var, file, or value)")
		}
		if c.Azure.Container == "" {
			return fmt.Errorf("storage: azure.container is required")
		}
	case ProviderLocal:
		if c.Local.Path == "" {
			return fmt.Errorf("storage: local.path is required")
		}
	default:
		return fmt.Errorf("storage: unknown provider %q (supported: s3, gcs, azure, local)", c.Provider)
	}

	return nil
}

// DefaultConfig returns a sensible default configuration for local development.
func DefaultConfig() Config {
	return Config{
		DefaultVisibility: Private,
		Provider:          ProviderLocal,
		PublicPaths:       map[string]string{},
		Local: LocalConfig{
			Path: "storage/",
		},
		Cleanup: CleanupConfig{
			Enabled:  false,
			Interval: "1h",
			Prefix:   "_tmp/",
			MaxAge:   "24h",
		},
	}
}
