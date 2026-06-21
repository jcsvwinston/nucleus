// Package app provides the application configuration and bootstrap for Nucleus.
// Configuration is loaded from multiple sources with increasing precedence:
// struct defaults < YAML file < environment variables (prefix NUCLEUS_).
package app

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// JWTKeySpec describes one key in the JWT keyset constructed by App.New.
// Operators populate this slice via `auth.jwt_keys` in nucleus.yml and
// nominate the current signing key via `auth.jwt_current_kid`. The key
// material itself follows the `CredentialSource` pattern already used
// by pkg/storage: PEM files and secrets stay out of tracked YAML and
// load from `*_env` / `*_path` references.
//
// Supported algorithms and their material fields:
//
//   - HS256 — set `SecretEnv` (the named environment variable holds the
//     shared HMAC secret).
//   - RS256 — set exactly one of `PemPath` / `PemEnv` (RSA private key,
//     PKCS#1 or PKCS#8 PEM).
//   - ES256 — set exactly one of `PemPath` / `PemEnv` (ECDSA P-256
//     private key, SEC1 or PKCS#8 PEM). Only the P-256 curve is
//     accepted; see ADR-005.
//
// `SecretEnv` reads the named environment variable; `PemPath` reads a
// file from disk; `PemEnv` reads PEM bytes from an environment variable
// (suitable for Kubernetes secrets mounted as env vars).
type JWTKeySpec struct {
	KID       string `koanf:"kid"`
	Algorithm string `koanf:"algorithm"`
	SecretEnv string `koanf:"secret_env"`
	PemPath   string `koanf:"pem_path"`
	PemEnv    string `koanf:"pem_env"`
}

// Config holds all framework configuration. Every field has a sensible default
// for local development so zero configuration is required to get started.
type Config struct {
	// Server
	Host         string        `koanf:"host"`
	Port         int           `koanf:"port"`
	ReadTimeout  time.Duration `koanf:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout"`
	IdleTimeout  time.Duration `koanf:"idle_timeout"`

	// TLS configuration (optional — empty disables HTTPS)
	TLSCertFile string `koanf:"tls_cert_file"`
	TLSKeyFile  string `koanf:"tls_key_file"`

	// Database
	DatabaseDefault string                    `koanf:"database_default"`
	Databases       map[string]DatabaseConfig `koanf:"databases"`

	// Multi-site and multi-tenant routing.
	MultiSite   MultiSiteConfig   `koanf:"multisite"`
	MultiTenant MultiTenantConfig `koanf:"multitenant"`

	// Redis (optional — empty disables Redis-backed features)
	RedisURL string `koanf:"redis_url"`

	// Auth
	JWTSecret       string        `koanf:"jwt_secret"`
	JWTExpiry       time.Duration `koanf:"jwt_expiry"`
	JWTIssuer       string        `koanf:"jwt_issuer"`
	JWTKeys         []JWTKeySpec  `koanf:"jwt_keys"`
	JWTCurrentKID   string        `koanf:"jwt_current_kid"`
	SessionLifetime time.Duration `koanf:"session_lifetime"`
	SessionStore    string        `koanf:"session_store"`
	SessionRedisURL string        `koanf:"session_redis_url"`
	SessionTable    string        `koanf:"session_table"`

	// Session cookies
	SessionCookieName   string `koanf:"session_cookie_name"`
	SessionCookieDomain string `koanf:"session_cookie_domain"`
	SessionCookiePath   string `koanf:"session_cookie_path"`
	// SessionCookieSecure sets the session cookie's Secure attribute.
	// Default true (secure-by-default, SPEC §2.4): the session cookie
	// refuses to ride over plain HTTP. Local development over http:// must
	// opt out with `session_cookie_secure: false`. Mirrors the CSRF cookie
	// posture (ADR-008: Secure by default, explicit opt-out).
	SessionCookieSecure   bool          `koanf:"session_cookie_secure"`
	SessionCookieSameSite string        `koanf:"session_cookie_samesite"`
	SessionIdleTimeout    time.Duration `koanf:"session_idle_timeout"`
	SessionRedisPrefix    string        `koanf:"session_redis_prefix"`

	// RBAC
	//
	// RBACPolicyFile is the path to the Casbin RBAC CSV policy file.
	RBACPolicyFile string `koanf:"rbac_policy_file"`

	// AdminRBACPolicyFile is the DEPRECATED alias for RBACPolicyFile. It is
	// retained for backward compatibility: when RBACPolicyFile is empty and
	// this is set, the framework uses it and emits a one-time startup WARN.
	// Prefer rbac_policy_file; this key will be removed in a future release.
	AdminRBACPolicyFile string `koanf:"admin_rbac_policy_file"`

	// Mail
	MailDriver string `koanf:"mail_driver"`
	SMTPHost   string `koanf:"smtp_host"`
	SMTPPort   int    `koanf:"smtp_port"`
	SMTPUser   string `koanf:"smtp_user"`
	SMTPPass   string `koanf:"smtp_pass"`
	MailFrom   string `koanf:"mail_from"`

	// MailCircuitBreaker, when Enabled, wraps mail.Sender.Send calls
	// with a pkg/circuit breaker. Healthy (the SMTP HELO probe used by
	// /healthz) bypasses the breaker so a recovering dependency can
	// still be observed while Send is short-circuited.
	MailCircuitBreaker CircuitBreakerSpec `koanf:"mail_circuit_breaker"`

	// Observability
	LogLevel     string `koanf:"log_level"`
	LogFormat    string `koanf:"log_format"`
	OTLPEndpoint string `koanf:"otlp_endpoint"`
	MetricsPath  string `koanf:"metrics_path"`

	// LogRedactExtraKeys are additional log attribute keys whose values
	// the structured logger redacts, on top of the built-in denylist
	// (observe.DefaultRedactedKeys). Use it for app-specific sensitive
	// fields. There is intentionally no config key to *disable*
	// redaction — that requires an explicit code-level opt-out via
	// observe.NewLoggerWithRedaction. See ADR-007.
	LogRedactExtraKeys []string `koanf:"log_redact_extra_keys"`

	// Security
	RateLimitRequests int           `koanf:"rate_limit_requests"`
	RateLimitWindow   time.Duration `koanf:"rate_limit_window"`
	RateLimitBurst    int           `koanf:"rate_limit_burst"`
	RateLimitByRoute  bool          `koanf:"rate_limit_by_route"`
	RateLimitByRole   bool          `koanf:"rate_limit_by_role"`

	// CORSOrigins is the allow-list of origins permitted by the CORS
	// middleware. An empty list (the default) preserves the historical
	// allow-all behavior (`Access-Control-Allow-Origin: *` for
	// credential-less requests); a non-empty list restricts CORS to exactly
	// these origins and rejects all others. See `docs/guides/security.md`.
	CORSOrigins []string `koanf:"cors_origins"`
	// CORSAllowCredentials controls whether the CORS middleware emits
	// `Access-Control-Allow-Credentials: true`. It is only honored when
	// CORSOrigins is non-empty: per the Fetch standard, credentials cannot be
	// combined with the `*` wildcard, so the allow-all default never sets it.
	CORSAllowCredentials bool `koanf:"cors_allow_credentials"`

	// i18n
	DefaultLocale string `koanf:"default_locale"`
	LocalesPath   string `koanf:"locales_path"`

	// Static files
	StaticPrefix string `koanf:"static_prefix"`
	StaticRoot   string `koanf:"static_root"`

	// File storage (legacy — deprecated, use StorageConfig below)
	StorageDriver string `koanf:"storage_driver"`
	StoragePath   string `koanf:"storage_path"`

	// Storage (new unified config)
	Storage StorageConfig `koanf:"storage"`

	// Outbox (transactional outbox pattern)
	// When enabled, the outbox provides reliable message delivery through
	// a SQL-backed table with support for external bridges (Kafka, webhooks, etc.)
	Outbox OutboxConfig `koanf:"outbox"`

	// Templates
	TemplatesDir string `koanf:"templates_dir"`

	// Environment
	Env   string `koanf:"env"`
	Debug bool   `koanf:"debug"`

	// StateDir is the local directory under which the framework persists
	// machine-local artefacts. Default: "./.nucleus-state". Override with the
	// NUCLEUS_STATE_DIR environment variable.
	StateDir string `koanf:"state_dir"`
}

// DatabaseConfig describes one named database connection under databases.<alias>.
type DatabaseConfig struct {
	URL         string        `koanf:"url"`
	MaxOpen     int           `koanf:"max_open"`
	MaxIdle     int           `koanf:"max_idle"`
	MaxLifetime time.Duration `koanf:"max_lifetime"`
}

// MultiSiteConfig describes host-based site resolution.
type MultiSiteConfig struct {
	Enabled     bool                  `koanf:"enabled"`
	DefaultSite string                `koanf:"default_site"`
	Sites       map[string]SiteConfig `koanf:"sites"`
}

// SiteConfig maps host patterns to a logical site and default DB alias.
// Host patterns support exact hosts and wildcard prefix patterns (*.example.com).
type SiteConfig struct {
	Hosts                       []string `koanf:"hosts"`
	Database                    string   `koanf:"database"`
	TenantDatabaseAliasTemplate string   `koanf:"tenant_database_alias_template"`
}

// StorageConfig is the unified storage configuration.
type StorageConfig struct {
	// Default visibility for new objects (private|public).
	DefaultVisibility string `koanf:"default"`

	// Provider selects the storage backend (s3|gcs|azure|local).
	Provider string `koanf:"provider"`

	// PublicPaths maps public URL paths to storage key prefixes.
	PublicPaths map[string]string `koanf:"public_paths"`

	// PublicURLBase is the base URL for public objects (CDN or direct provider).
	PublicURLBase string `koanf:"public_url_base"`

	// S3 configuration
	S3 struct {
		Endpoint        string `koanf:"endpoint"`
		Bucket          string `koanf:"bucket"`
		Region          string `koanf:"region"`
		AccessKeyID     string `koanf:"access_key_id"`     // Direct value (use env vars at OS level)
		SecretAccessKey string `koanf:"secret_access_key"` // Direct value (use env vars at OS level)
		UsePathStyle    bool   `koanf:"use_path_style"`
		PublicBucket    string `koanf:"public_bucket"`
	} `koanf:"s3"`

	// GCS configuration
	GCS struct {
		Bucket       string `koanf:"bucket"`
		PublicBucket string `koanf:"public_bucket"`
	} `koanf:"gcs"`

	// Azure configuration
	Azure struct {
		AccountName     string `koanf:"account_name"` // Direct value (use env vars at OS level)
		AccountKey      string `koanf:"account_key"`  // Direct value (use env vars at OS level)
		Container       string `koanf:"container"`
		PublicContainer string `koanf:"public_container"`
	} `koanf:"azure"`

	// Local configuration (development only)
	Local struct {
		Path string `koanf:"path"`
	} `koanf:"local"`

	// Cleanup config
	Cleanup struct {
		Enabled  bool   `koanf:"enabled"`
		Interval string `koanf:"interval"`
		Prefix   string `koanf:"prefix"`
		MaxAge   string `koanf:"max_age"`
	} `koanf:"cleanup"`

	// CircuitBreaker, when Enabled, wraps remote storage operations
	// (Put/Get/Delete/Exists/List/SignedURL/Copy) with a pkg/circuit
	// breaker. The local provider is never wrapped. PublicURL is
	// pass-through (pure string composition).
	CircuitBreaker CircuitBreakerSpec `koanf:"circuit_breaker"`
}

// CircuitBreakerSpec is the koanf-bindable shape for the optional
// circuit breaker wrapping mail and storage. The same struct backs
// `mail_circuit_breaker.*` and `storage.circuit_breaker.*` config
// keys.
//
// Defaults applied by DefaultConfig are Enabled=true,
// FailureThreshold=5, Cooldown=30s, HalfOpenMaxConcurrent=1.
type CircuitBreakerSpec struct {
	// Enabled turns on circuit-breaker wrapping for the package.
	Enabled bool `koanf:"enabled"`

	// FailureThreshold is the number of consecutive failures required
	// to trip the breaker open.
	FailureThreshold int `koanf:"failure_threshold"`

	// Cooldown is the duration the breaker stays open before admitting
	// half-open probes.
	Cooldown time.Duration `koanf:"cooldown"`

	// HalfOpenMaxConcurrent caps in-flight probes in the half-open
	// state.
	HalfOpenMaxConcurrent int `koanf:"half_open_max_concurrent"`
}

// OutboxConfig configures the transactional outbox pattern for reliable message delivery.
type OutboxConfig struct {
	Enabled       bool           `koanf:"enabled"`
	TableName     string         `koanf:"table_name"`
	LeaseDuration time.Duration  `koanf:"lease_duration"`
	MaxRetries    int            `koanf:"max_retries"`
	RetryBackoff  time.Duration  `koanf:"retry_backoff"`
	Bridges       []BridgeConfig `koanf:"bridges"`
}

// BridgeConfig configures an external message bridge (Kafka, Webhook, RabbitMQ, etc.).
type BridgeConfig struct {
	Name   string                 `koanf:"name"`
	Type   string                 `koanf:"type"` // kafka, webhook, rabbitmq
	Config map[string]interface{} `koanf:"config"`
}

// MultiTenantConfig describes tenant resolution and tenant->database mapping.
type MultiTenantConfig struct {
	Enabled               bool                    `koanf:"enabled"`
	Resolver              string                  `koanf:"resolver"` // subdomain|header
	Header                string                  `koanf:"header"`
	DefaultTenant         string                  `koanf:"default_tenant"`
	RequireIsolatedDB     bool                    `koanf:"require_isolated_db"`
	DatabaseAliasTemplate string                  `koanf:"database_alias_template"`
	Tenants               map[string]TenantConfig `koanf:"tenants"`
}

// TenantConfig allows explicit site and database alias assignment for one tenant id.
type TenantConfig struct {
	Site     string `koanf:"site"`
	Database string `koanf:"database"`
}

// defaults returns a Config populated with sensible development defaults.
func defaults() Config {
	defaultDB := DatabaseConfig{
		URL:         "sqlite://nucleus.db",
		MaxOpen:     25,
		MaxIdle:     5,
		MaxLifetime: 5 * time.Minute,
	}
	return Config{
		Host:         "0.0.0.0",
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,

		DatabaseDefault: "default",
		Databases: map[string]DatabaseConfig{
			"default": defaultDB,
		},

		MultiSite: MultiSiteConfig{
			Enabled:     false,
			DefaultSite: "default",
			Sites: map[string]SiteConfig{
				"default": {Database: "default"},
			},
		},
		MultiTenant: MultiTenantConfig{
			Enabled:               false,
			Resolver:              "subdomain",
			Header:                "X-Tenant-ID",
			DefaultTenant:         "",
			RequireIsolatedDB:     true,
			DatabaseAliasTemplate: "tenant_%s",
			Tenants:               map[string]TenantConfig{},
		},

		JWTExpiry:       24 * time.Hour,
		SessionLifetime: 72 * time.Hour,
		SessionStore:    "memory",
		SessionTable:    "nucleus_sessions",

		SessionCookieName:     "session",
		SessionCookiePath:     "/",
		SessionCookieSecure:   true,
		SessionCookieSameSite: "lax",
		SessionRedisPrefix:    "nucleus:sessions:",

		MailDriver: "noop",
		SMTPPort:   587,
		MailFrom:   "noreply@localhost",

		MailCircuitBreaker: CircuitBreakerSpec{
			Enabled:               true,
			FailureThreshold:      5,
			Cooldown:              30 * time.Second,
			HalfOpenMaxConcurrent: 1,
		},

		LogLevel:    "info",
		LogFormat:   "json",
		MetricsPath: "/metrics",

		RateLimitRequests: 0,
		RateLimitWindow:   time.Minute,
		RateLimitBurst:    0,
		RateLimitByRoute:  false,
		RateLimitByRole:   false,

		DefaultLocale: "en",
		LocalesPath:   "locales/",

		StaticPrefix: "/static/",
		StaticRoot:   "static/",

		StorageDriver: "local",
		StoragePath:   "uploads/",

		Storage: StorageConfig{
			DefaultVisibility: "private",
			Provider:          "local",
			PublicPaths:       map[string]string{},
			PublicURLBase:     "",
			S3: struct {
				Endpoint        string `koanf:"endpoint"`
				Bucket          string `koanf:"bucket"`
				Region          string `koanf:"region"`
				AccessKeyID     string `koanf:"access_key_id"`
				SecretAccessKey string `koanf:"secret_access_key"`
				UsePathStyle    bool   `koanf:"use_path_style"`
				PublicBucket    string `koanf:"public_bucket"`
			}{},
			GCS: struct {
				Bucket       string `koanf:"bucket"`
				PublicBucket string `koanf:"public_bucket"`
			}{},
			Azure: struct {
				AccountName     string `koanf:"account_name"`
				AccountKey      string `koanf:"account_key"`
				Container       string `koanf:"container"`
				PublicContainer string `koanf:"public_container"`
			}{},
			Local: struct {
				Path string `koanf:"path"`
			}{
				Path: "storage/",
			},
			Cleanup: struct {
				Enabled  bool   `koanf:"enabled"`
				Interval string `koanf:"interval"`
				Prefix   string `koanf:"prefix"`
				MaxAge   string `koanf:"max_age"`
			}{
				Enabled:  false,
				Interval: "1h",
				Prefix:   "_tmp/",
				MaxAge:   "24h",
			},
			CircuitBreaker: CircuitBreakerSpec{
				Enabled:               true,
				FailureThreshold:      5,
				Cooldown:              30 * time.Second,
				HalfOpenMaxConcurrent: 1,
			},
		},

		Outbox: OutboxConfig{
			Enabled:       false,
			TableName:     "nucleus_outbox",
			LeaseDuration: 30 * time.Second,
			MaxRetries:    5,
			RetryBackoff:  time.Second,
			Bridges:       []BridgeConfig{},
		},
		TemplatesDir: "internal/web/templates",

		Env:   "development",
		Debug: false,

		StateDir: "./.nucleus-state",
	}
}

// DefaultConfig returns a copy of the framework default configuration.
func DefaultConfig() Config {
	return defaults()
}

// LoadConfig loads configuration from multiple sources with increasing precedence:
// 1. Struct defaults
// 2. YAML file (optional — path argument or "nucleus.yml" in current directory)
// 3. Environment variables with prefix NUCLEUS_
//
// If no path is provided and "nucleus.yml" does not exist, only defaults and
// env vars are used.
func LoadConfig(path ...string) (*Config, error) {
	k := koanf.New(".")

	// 1. Load struct defaults
	if err := k.Load(structs.Provider(defaults(), "koanf"), nil); err != nil {
		return nil, fmt.Errorf("app.LoadConfig defaults: %w", err)
	}

	// 2. Load YAML file
	cfgPath := "nucleus.yml"
	if len(path) > 0 && path[0] != "" {
		cfgPath = path[0]
	}
	if _, err := os.Stat(cfgPath); err == nil {
		if err := k.Load(file.Provider(cfgPath), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("app.LoadConfig file=%s: %w", cfgPath, err)
		}
	}

	// 3. Load environment variables (NUCLEUS_PORT -> port)
	if err := k.Load(env.Provider("NUCLEUS_", ".", func(s string) string {
		key := strings.TrimPrefix(s, "NUCLEUS_")
		// Use double underscore for nested keys:
		// NUCLEUS_DATABASES__ANALYTICS__URL -> databases.analytics.url
		key = strings.ReplaceAll(key, "__", ".")
		return strings.ToLower(key)
	}), nil); err != nil {
		return nil, fmt.Errorf("app.LoadConfig env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("app.LoadConfig unmarshal: %w", err)
	}
	normalizeRuntimeConfig(&cfg)

	return &cfg, nil
}

// Addr returns the host:port address string for the server.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// IsDev returns true if the environment is "development".
func (c *Config) IsDev() bool {
	return c.Env == "development"
}

// IsProd returns true if the environment is "production".
func (c *Config) IsProd() bool {
	return c.Env == "production"
}

// DefaultDatabaseAlias returns the configured primary database alias.
func (c *Config) DefaultDatabaseAlias() string {
	if c == nil {
		return "default"
	}
	alias := normalizeAlias(c.DatabaseDefault)
	if alias == "" {
		return "default"
	}
	return alias
}

// DatabaseAliases returns configured database aliases with non-empty URLs.
func (c *Config) DatabaseAliases() []string {
	if c == nil || len(c.Databases) == 0 {
		return nil
	}
	aliases := make([]string, 0, len(c.Databases))
	for alias, dbc := range c.Databases {
		if strings.TrimSpace(dbc.URL) == "" {
			continue
		}
		aliases = append(aliases, normalizeAlias(alias))
	}
	sort.Strings(aliases)
	return aliases
}

// DatabaseByAlias returns one resolved database config.
func (c *Config) DatabaseByAlias(alias string) (DatabaseConfig, bool) {
	if c == nil {
		return DatabaseConfig{}, false
	}
	key := normalizeAlias(alias)
	if key == "" {
		key = c.DefaultDatabaseAlias()
	}
	dbCfg, ok := c.Databases[key]
	if !ok {
		return DatabaseConfig{}, false
	}
	if strings.TrimSpace(dbCfg.URL) == "" {
		return DatabaseConfig{}, false
	}
	primary := c.DefaultDatabase()
	if dbCfg.MaxOpen <= 0 {
		dbCfg.MaxOpen = primary.MaxOpen
	}
	if dbCfg.MaxIdle <= 0 {
		dbCfg.MaxIdle = primary.MaxIdle
	}
	if dbCfg.MaxLifetime <= 0 {
		dbCfg.MaxLifetime = primary.MaxLifetime
	}
	return dbCfg, true
}

// DefaultDatabase returns the resolved primary database config.
func (c *Config) DefaultDatabase() DatabaseConfig {
	base := defaults().Databases["default"]
	if c == nil {
		return base
	}
	defaultAlias := c.DefaultDatabaseAlias()
	dbCfg, ok := c.Databases[defaultAlias]
	if !ok || strings.TrimSpace(dbCfg.URL) == "" {
		return base
	}
	if dbCfg.MaxOpen <= 0 {
		dbCfg.MaxOpen = base.MaxOpen
	}
	if dbCfg.MaxIdle <= 0 {
		dbCfg.MaxIdle = base.MaxIdle
	}
	if dbCfg.MaxLifetime <= 0 {
		dbCfg.MaxLifetime = base.MaxLifetime
	}
	return dbCfg
}

func normalizeRuntimeConfig(cfg *Config) {
	NormalizeRuntimeConfig(cfg)
}

// NormalizeRuntimeConfig applies the framework's runtime-config
// normalisations (database alias canonicalisation, multi-site /
// multi-tenant resolver normalisation) to cfg in
// place. `app.LoadConfig` calls this internally before returning;
// callers that bypass `LoadConfig` (most notably the multi-file
// loader in `pkg/nucleus.FromConfigFile`) need to call this so they
// produce a `*Config` indistinguishable from the env-var path.
// Safe to call with cfg == nil (no-op).
func NormalizeRuntimeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	normalizeDatabaseConfig(cfg)
	normalizeMultiSiteConfig(cfg)
	normalizeMultiTenantConfig(cfg)
}

func normalizeDatabaseConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	defaultAlias := normalizeAlias(cfg.DatabaseDefault)
	if defaultAlias == "" {
		defaultAlias = "default"
	}

	base := defaults()
	baseDB := base.Databases["default"]

	normalized := make(map[string]DatabaseConfig, len(cfg.Databases)+1)
	for alias, dbc := range cfg.Databases {
		key := normalizeAlias(alias)
		if key == "" {
			continue
		}
		dbc.URL = strings.TrimSpace(dbc.URL)
		normalized[key] = dbc
	}
	if defaultAlias != "default" {
		if fallback, ok := normalized["default"]; ok && strings.TrimSpace(fallback.URL) == strings.TrimSpace(baseDB.URL) {
			delete(normalized, "default")
		}
	}

	if len(normalized) == 0 {
		normalized[defaultAlias] = baseDB
	}

	defaultDB := normalized[defaultAlias]
	if strings.TrimSpace(defaultDB.URL) == "" && len(normalized) > 0 {
		aliases := make([]string, 0, len(normalized))
		for alias := range normalized {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			candidate := normalized[alias]
			if strings.TrimSpace(candidate.URL) == "" {
				continue
			}
			defaultAlias = alias
			defaultDB = candidate
			break
		}
	}

	if strings.TrimSpace(defaultDB.URL) == "" {
		defaultDB = baseDB
		normalized[defaultAlias] = defaultDB
	}
	if defaultDB.MaxOpen <= 0 {
		defaultDB.MaxOpen = baseDB.MaxOpen
	}
	if defaultDB.MaxIdle <= 0 {
		defaultDB.MaxIdle = baseDB.MaxIdle
	}
	if defaultDB.MaxLifetime <= 0 {
		defaultDB.MaxLifetime = baseDB.MaxLifetime
	}
	normalized[defaultAlias] = defaultDB

	for alias, dbc := range normalized {
		if dbc.MaxOpen <= 0 {
			dbc.MaxOpen = defaultDB.MaxOpen
		}
		if dbc.MaxIdle <= 0 {
			dbc.MaxIdle = defaultDB.MaxIdle
		}
		if dbc.MaxLifetime <= 0 {
			dbc.MaxLifetime = defaultDB.MaxLifetime
		}
		normalized[alias] = dbc
	}

	cfg.DatabaseDefault = defaultAlias
	cfg.Databases = normalized
}

func normalizeMultiSiteConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	ms := cfg.MultiSite
	defaultSite := normalizeAlias(ms.DefaultSite)
	if defaultSite == "" {
		defaultSite = "default"
	}

	sites := make(map[string]SiteConfig, len(ms.Sites)+1)
	for rawName, site := range ms.Sites {
		name := normalizeAlias(rawName)
		if name == "" {
			continue
		}
		site.Database = normalizeAlias(site.Database)
		if site.Database == "" {
			site.Database = cfg.DefaultDatabaseAlias()
		}
		site.TenantDatabaseAliasTemplate = strings.TrimSpace(site.TenantDatabaseAliasTemplate)
		site.Hosts = normalizeHostPatterns(site.Hosts)
		sites[name] = site
	}

	if _, ok := sites[defaultSite]; !ok {
		sites[defaultSite] = SiteConfig{
			Database: cfg.DefaultDatabaseAlias(),
		}
	}

	ms.DefaultSite = defaultSite
	ms.Sites = sites
	cfg.MultiSite = ms
}

func normalizeMultiTenantConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	mt := cfg.MultiTenant
	mt.Resolver = strings.ToLower(strings.TrimSpace(mt.Resolver))
	switch mt.Resolver {
	case "", "subdomain":
		mt.Resolver = "subdomain"
	case "header":
		// ok
	default:
		mt.Resolver = "subdomain"
	}

	mt.Header = strings.TrimSpace(mt.Header)
	if mt.Header == "" {
		mt.Header = "X-Tenant-ID"
	}

	mt.DefaultTenant = normalizeAlias(mt.DefaultTenant)
	if !mt.RequireIsolatedDB {
		// keep explicit opt-out as-is.
	}
	mt.DatabaseAliasTemplate = strings.TrimSpace(mt.DatabaseAliasTemplate)
	if mt.DatabaseAliasTemplate == "" {
		mt.DatabaseAliasTemplate = "tenant_%s"
	}

	tenants := make(map[string]TenantConfig, len(mt.Tenants))
	for rawTenant, tenant := range mt.Tenants {
		tenantID := normalizeAlias(rawTenant)
		if tenantID == "" {
			continue
		}
		tenant.Site = normalizeAlias(tenant.Site)
		tenant.Database = normalizeAlias(tenant.Database)
		tenants[tenantID] = tenant
	}
	mt.Tenants = tenants
	cfg.MultiTenant = mt
}

func validateMultiTenantIsolation(cfg *Config) error {
	if cfg == nil || !cfg.MultiTenant.Enabled || !cfg.MultiTenant.RequireIsolatedDB {
		return nil
	}

	globalTemplate := strings.TrimSpace(cfg.MultiTenant.DatabaseAliasTemplate)
	if globalTemplate != "" && !aliasTemplateHasTenant(globalTemplate) && len(cfg.MultiTenant.Tenants) == 0 {
		return fmt.Errorf("multitenant.database_alias_template must include %%s or {tenant} when tenant isolation is required")
	}

	aliasOwner := map[string]string{}
	for tenantID, tenantCfg := range cfg.MultiTenant.Tenants {
		siteName := normalizeAlias(tenantCfg.Site)
		if siteName == "" {
			siteName = cfg.MultiSite.DefaultSite
		}
		siteCfg := cfg.MultiSite.Sites[siteName]
		siteBaseAlias := normalizeAlias(siteCfg.Database)
		if siteBaseAlias == "" {
			siteBaseAlias = cfg.DefaultDatabaseAlias()
		}

		resolvedAlias := normalizeAlias(tenantCfg.Database)
		if resolvedAlias == "" {
			resolvedAlias = formatAliasTemplate(siteCfg.TenantDatabaseAliasTemplate, tenantID)
		}
		if resolvedAlias == "" {
			resolvedAlias = formatAliasTemplate(globalTemplate, tenantID)
		}
		if resolvedAlias == "" {
			return fmt.Errorf("multitenant.tenants.%s has no database alias and no tenant template is available", tenantID)
		}
		if resolvedAlias == siteBaseAlias {
			return fmt.Errorf("multitenant tenant %q resolves to shared site database alias %q", tenantID, resolvedAlias)
		}

		if prevTenant, ok := aliasOwner[resolvedAlias]; ok && prevTenant != tenantID {
			return fmt.Errorf("multitenant tenants %q and %q share database alias %q", prevTenant, tenantID, resolvedAlias)
		}
		aliasOwner[resolvedAlias] = tenantID
	}

	for siteName, siteCfg := range cfg.MultiSite.Sites {
		tmpl := strings.TrimSpace(siteCfg.TenantDatabaseAliasTemplate)
		if tmpl == "" {
			continue
		}
		if !aliasTemplateHasTenant(tmpl) {
			return fmt.Errorf("multisite.sites.%s.tenant_database_alias_template must include %%s or {tenant}", siteName)
		}
	}

	return nil
}

func aliasTemplateHasTenant(template string) bool {
	tpl := strings.TrimSpace(template)
	if tpl == "" {
		return false
	}
	return strings.Contains(tpl, "%s") || strings.Contains(tpl, "{tenant}")
}

func normalizeHostPatterns(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	out := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, raw := range hosts {
		h := strings.ToLower(strings.TrimSpace(raw))
		if h == "" {
			continue
		}
		if strings.HasSuffix(h, ".") {
			h = strings.TrimSuffix(h, ".")
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func normalizeAlias(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// toStorageConfig converts the app Config to storage.Config.
func (c *Config) toStorageConfig() storage.Config {
	cfg := storage.Config{
		DefaultVisibility: storage.Visibility(c.Storage.DefaultVisibility),
		PublicPaths:       make(map[string]string),
		PublicURLBase:     c.Storage.PublicURLBase,
	}

	// Determine provider
	switch strings.ToLower(c.Storage.Provider) {
	case "s3", "minio", "r2":
		cfg.Provider = storage.ProviderS3
	case "gcs":
		cfg.Provider = storage.ProviderGCS
	case "azure":
		cfg.Provider = storage.ProviderAzure
	default:
		cfg.Provider = storage.ProviderLocal
	}

	// Copy public paths
	for k, v := range c.Storage.PublicPaths {
		cfg.PublicPaths[k] = v
	}

	// Provider-specific config
	cfg.S3 = storage.S3Config{
		Endpoint:        c.Storage.S3.Endpoint,
		Bucket:          c.Storage.S3.Bucket,
		Region:          c.Storage.S3.Region,
		AccessKeyID:     storage.CredentialSource{Value: c.Storage.S3.AccessKeyID},
		SecretAccessKey: storage.CredentialSource{Value: c.Storage.S3.SecretAccessKey},
		UsePathStyle:    c.Storage.S3.UsePathStyle,
		PublicBucket:    c.Storage.S3.PublicBucket,
	}

	cfg.GCS = storage.GCSConfig{
		Bucket:       c.Storage.GCS.Bucket,
		PublicBucket: c.Storage.GCS.PublicBucket,
	}

	cfg.Azure = storage.AzureConfig{
		AccountName:     storage.CredentialSource{Value: c.Storage.Azure.AccountName},
		AccountKey:      storage.CredentialSource{Value: c.Storage.Azure.AccountKey},
		Container:       c.Storage.Azure.Container,
		PublicContainer: c.Storage.Azure.PublicContainer,
	}

	cfg.Local = storage.LocalConfig{
		Path: c.Storage.Local.Path,
	}

	cfg.Cleanup = storage.CleanupConfig{
		Enabled:  c.Storage.Cleanup.Enabled,
		Interval: c.Storage.Cleanup.Interval,
		Prefix:   c.Storage.Cleanup.Prefix,
		MaxAge:   c.Storage.Cleanup.MaxAge,
	}

	cfg.CircuitBreaker = storage.CircuitBreakerConfig{
		Enabled:               c.Storage.CircuitBreaker.Enabled,
		FailureThreshold:      c.Storage.CircuitBreaker.FailureThreshold,
		Cooldown:              c.Storage.CircuitBreaker.Cooldown,
		HalfOpenMaxConcurrent: c.Storage.CircuitBreaker.HalfOpenMaxConcurrent,
	}

	// Fallback to legacy config if new config is empty
	if cfg.Local.Path == "" {
		cfg.Local.Path = c.StoragePath
		if cfg.Local.Path == "" {
			cfg.Local.Path = "storage/"
		}
	}

	return cfg
}
