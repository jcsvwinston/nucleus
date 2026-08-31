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

	"github.com/jcsvwinston/nucleus/internal/configbind"

	"github.com/jcsvwinston/nucleus/internal/providerns"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
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
	// AuthBackends is the ORDERED list of authentication backends the
	// login path consults, by registered name (auth.RegisterBackend).
	//
	// Order is the feature, not a detail: `[ldap, local]` means the
	// directory answers first and the application's own user table is what
	// still works the morning the directory does not. A backend that
	// cannot reach its source is skipped rather than treated as a
	// rejection, which is what makes that break-glass account usable.
	//
	// Empty means the chain is not built. An application with no user
	// provider and no directory has nothing to authenticate against, and
	// saying so with an empty list is clearer than inventing a default.
	AuthBackends []string `koanf:"auth_backends"`

	// AuthFederated declares the browser-redirect identity providers —
	// OIDC, SAML — an operator wants sign-in buttons for.
	//
	// Each entry names an INSTANCE and the registered provider type that
	// implements it, and reads its settings from `auth.<name>.*`, the same
	// subtree a credential backend uses:
	//
	//	public_base_url: https://app.example.com
	//	auth_federated:
	//	  - name: corp
	//	    provider: oidc
	//	    display_name: Corp SSO
	//	  - name: partners
	//	    provider: oidc
	//	auth:
	//	  corp:
	//	    issuer: https://login.corp.example/
	//	  partners:
	//	    issuer: https://idp.partners.example/
	//
	// Instance and type are separate names because two identity providers
	// of the same protocol is the ordinary case, and a registry keyed by
	// type alone would have made the second one impossible to express.
	//
	// This list is independent of AuthBackends: a federated flow has no
	// credentials to hand to a chain, so it is not a link in one. An
	// application usually wants both — a directory or local table for
	// break-glass, and an identity provider for everyone else.
	AuthFederated []auth.FederatedInstance `koanf:"auth_federated"`

	// PublicBaseURL is the address the BROWSER reaches this application
	// at, without a trailing slash. Federated sign-in needs it because the
	// callback URL an operator registers with their identity provider has
	// to be the one this application will be listening on, and the address
	// the process binds is frequently not it (a reverse proxy, a
	// container port). Required only when AuthFederated is non-empty.
	PublicBaseURL string `koanf:"public_base_url"`

	// HTTPInterceptors is the ORDERED list of registered request
	// interceptors to wrap the router in, outermost first.
	//
	// Order is the behaviour rather than a detail — authentication before
	// rate limiting and rate limiting before authentication are different
	// systems — so an interceptor is not merely enabled here, it is
	// placed, the same way auth_backends places a backend.
	//
	// Settings live under `interceptors.<name>.*`, mirroring how
	// auth_backends pairs with auth.<name>.*: the list orders, the
	// subtree configures.
	//
	//	http_interceptors: [audit, tenant-guard]
	//	interceptors:
	//	  audit:
	//	    sink: stdout
	//
	// A name nobody registered fails at BOOT, naming what is registered: a
	// typo in a list of request interceptors must not resolve to one fewer
	// protection, quietly.
	HTTPInterceptors []string `koanf:"http_interceptors"`

	// InterceptorConfig maps a registered interceptor name to its
	// `interceptors.<name>.*` subtree. Populated by the loader; not a
	// key an operator writes.
	InterceptorConfig map[string]map[string]any `koanf:"-"`

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
	//
	// SessionCookieName supports the "__Host-" / "__Secure-" cookie
	// prefixes (recommended over HTTPS): App.New validates the prefix
	// preconditions at startup — __Host- requires session_cookie_secure,
	// path "/" and no domain; __Secure- requires session_cookie_secure —
	// and fails loud instead of issuing a cookie every browser would
	// silently drop.
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
	// RBACPolicyFile is the path to the Casbin RBAC CSV policy file. The
	// deprecated admin_rbac_policy_file alias was removed in v0.12.0
	// (DEP-2026-004; see MA-2026-004 for the one-line rename).
	RBACPolicyFile string `koanf:"rbac_policy_file"`

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

	// SQLDriverInstrumentation wraps the database/sql driver so that direct
	// db.QueryContext/ExecContext statements — the ones that bypass
	// model.CRUD (outbox dispatch, SQL session stores, migrations, schema
	// drift, and any raw SQL an app runs) — also reach the observability
	// bus's live SQL feed. Statements issued through model.CRUD are already
	// on the feed and are not double-recorded. Default false: without it the
	// feed shows only CRUD traffic (the historical behaviour) and the driver
	// is not wrapped, so there is zero hot-path cost. Enabling it adds a
	// small per-direct-statement cost; the expensive work (sanitize + emit)
	// still runs only when a subscriber is attached.
	SQLDriverInstrumentation bool `koanf:"sql_driver_instrumentation"`

	// MetricsPublic controls whether the Prometheus endpoint at
	// MetricsPath is seeded into the bootstrap allow-list (ADR-004) and so
	// answers without authorization. Default true — the historical
	// behaviour, matching the common "scraper on a private network" setup.
	// Set false to put /metrics behind the default-deny RBAC enforcer:
	// grant your scraper access with a policy on the metrics path (e.g.
	// `p, metrics-scraper, /metrics, *` plus JWT auth), or keep the
	// endpoint private at the network layer instead. Note that metric
	// VALUES are operational data; if your metrics carry anything
	// sensitive, flip this off or firewall the path.
	MetricsPublic bool `koanf:"metrics_public"`

	// LogRedactExtraKeys are additional log attribute keys whose values
	// the structured logger redacts, on top of the built-in denylist
	// (observe.DefaultRedactedKeys). Use it for app-specific sensitive
	// fields. There is intentionally no config key to *disable*
	// redaction — that requires an explicit code-level opt-out via
	// observe.NewLoggerWithRedaction. See ADR-007.
	LogRedactExtraKeys []string `koanf:"log_redact_extra_keys"`

	// Security — CSRF
	//
	// CSRFEnabled mounts the router's CSRF middleware (router.WithCSRF) on
	// the default stack: origin verification via Sec-Fetch-Site with the
	// double-submit token as fallback. Default false — CSRF protection is
	// opt-in because it only makes sense for cookie/session-authenticated
	// HTML apps; a pure Bearer-token API does not need it. The mvc scaffold
	// enables it; enable it in any app that authenticates browsers with the
	// session cookie.
	CSRFEnabled bool `koanf:"csrf_enabled"`
	// CSRFExemptPaths are URL path prefixes excluded from CSRF validation
	// (e.g. "/api/" for Bearer-only subtrees, or webhook receivers that
	// authenticate by signature).
	CSRFExemptPaths []string `koanf:"csrf_exempt_paths"`

	// CSRFInsecureCookie disables the Secure attribute on the CSRF cookies —
	// a development-only opt-out mirroring session_cookie_secure: false. The
	// default (Secure) makes the double-submit flow unreachable for plain
	// HTTP non-browser clients (Go's cookiejar over http://127.0.0.1), while
	// browsers special-case localhost. Never enable it in production.
	CSRFInsecureCookie bool `koanf:"csrf_insecure_cookie"`

	// Security
	RateLimitRequests int           `koanf:"rate_limit_requests"`
	RateLimitWindow   time.Duration `koanf:"rate_limit_window"`
	RateLimitBurst    int           `koanf:"rate_limit_burst"`
	RateLimitByRoute  bool          `koanf:"rate_limit_by_route"`
	RateLimitByRole   bool          `koanf:"rate_limit_by_role"`

	// TrustedProxies is the allow-list of upstream proxy addresses (IPs or
	// CIDR ranges) whose X-Forwarded-For / X-Real-IP headers are honored. An
	// empty list (the default) DENIES all forwarding headers: r.RemoteAddr —
	// the immediate peer — is used as the client IP for logging and rate
	// limiting. This prevents header-spoofed rate-limit evasion and audit-log
	// poisoning. Set it to your load balancer / reverse-proxy addresses (e.g.
	// ["10.0.0.0/8"]) when Nucleus runs behind one.
	TrustedProxies []string `koanf:"trusted_proxies"`

	// StorageProviderConfig carries the `storage.<provider>.*` subtree of a
	// REGISTERED third-party provider. It is not part of the schema — the
	// framework cannot know the shape of a backend it has never seen — so
	// it is captured raw at load time and handed to the provider, which
	// binds it into its own typed struct with Config.BindProvider.
	//
	// Without this a registry that let you plug a backend in did not let
	// you configure it, which is one step short of useful.
	StorageProviderConfig map[string]any `koanf:"-" json:"-" yaml:"-"`

	// AuthBackendConfig carries the `auth.<backend>.*` subtree of each
	// REGISTERED authentication backend named in AuthBackends, keyed by
	// backend name. Same reason and same shape as StorageProviderConfig:
	// the framework cannot know what a directory backend needs to reach
	// its directory, so the subtree is captured raw at load time and the
	// backend binds it into its own typed struct with
	// auth.BackendConfig.Bind.
	//
	// It is a map and not one subtree because the chain is ORDERED and can
	// hold several backends at once — `[ldap, local]` configures two.
	AuthBackendConfig map[string]map[string]any `koanf:"-" json:"-" yaml:"-"`

	// CORSOrigins is the allow-list of origins permitted by the CORS
	// middleware. An empty list (the default) DENIES cross-origin requests —
	// no CORS headers are emitted (security-by-default, completed at v1.0.0
	// per ADR-013 R4 / DEP-2026-007). A non-empty list restricts CORS to
	// exactly these origins; the historical allow-all behavior is the
	// explicit opt-in `["*"]` (`Access-Control-Allow-Origin: *` for
	// credential-less requests). Key reference:
	// `docs/reference/CONFIG_KEY_REGISTRY.md`.
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

	// Storage (unified config; the legacy flat storage_driver/storage_path
	// keys were removed in v0.12.0 — DEP-2026-005, MA-2026-005)
	Storage StorageConfig `koanf:"storage"`

	// Outbox (transactional outbox pattern)
	// When enabled, the outbox provides reliable message delivery through
	// a SQL-backed table with support for external bridges (Kafka, webhooks, etc.)
	Outbox OutboxConfig `koanf:"outbox"`

	// Jobs — module background jobs (pkg/nucleus ModuleSpec.Jobs).
	//
	// JobsProvider selects the pkg/tasks provider that executes them:
	// "memory" (default; in-process scheduler and workers, jobs are lost on
	// restart) or "asynq" (Redis-backed, durable, requires JobsRedisURL).
	JobsProvider string `koanf:"jobs_provider"`
	// JobsRedisURL is the Redis connection URL for the asynq jobs provider
	// (e.g. "redis://localhost:6379/0"). Required when jobs_provider is
	// "asynq"; ignored by the memory provider.
	JobsRedisURL string `koanf:"jobs_redis_url"`
	// JobsConcurrency is the number of concurrent job workers. 0 uses the
	// provider default.
	JobsConcurrency int `koanf:"jobs_concurrency"`

	// WebhooksPrefix is the URL prefix under which module webhook routes
	// (pkg/nucleus ModuleSpec.Webhooks) are mounted:
	// <prefix>/<module-name><path>. Default "/webhooks". When CSRF
	// protection is enabled the framework exempts this prefix
	// automatically — webhooks authenticate by signature, not CSRF token.
	WebhooksPrefix string `koanf:"webhooks_prefix"`

	// Templates
	TemplatesDir string `koanf:"templates_dir"`

	// Environment
	Env   string `koanf:"env"`
	Debug bool   `koanf:"debug"`

	// Profile applies a named preset over the loaded configuration (DX-23).
	// Supported: "dev" — swap every backing-service selection for its
	// no-dependency counterpart (SQLite database, in-memory sessions and
	// jobs, local filesystem storage, no-op mailer) so the SAME config file
	// boots with zero external services. Empty means no preset. Unknown
	// values fail config load.
	Profile string `koanf:"profile"`

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
	S3 S3StorageSpec `koanf:"s3"`

	// GCS configuration
	GCS GCSStorageSpec `koanf:"gcs"`

	// Azure configuration
	Azure AzureStorageSpec `koanf:"azure"`

	// Local configuration (development only)
	Local LocalStorageSpec `koanf:"local"`

	// Cleanup config
	Cleanup CleanupStorageSpec `koanf:"cleanup"`

	// CircuitBreaker, when Enabled, wraps remote storage operations
	// (Put/Get/Delete/Exists/List/SignedURL/Copy) with a pkg/circuit
	// breaker. The local provider is never wrapped. PublicURL is
	// pass-through (pure string composition).
	CircuitBreaker CircuitBreakerSpec `koanf:"circuit_breaker"`
}

// S3StorageSpec mirrors pkg/storage.S3Config key-for-key and TYPE-for-type
// (TestStorageConfigMirrorParity walks both recursively). Credentials are
// storage.CredentialSource — `access_key_id: "literal"` still binds (the
// config decoder promotes a plain string to {value: …}), and the
// `env_var`/`file`/`secret_manager` shapes the README promises are now
// actually loadable. QCD-FW-4's lesson: a mirror field missing or
// mis-typed here makes a documented key silently unreachable.
type S3StorageSpec struct {
	Endpoint        string                   `koanf:"endpoint"`
	Bucket          string                   `koanf:"bucket"`
	Region          string                   `koanf:"region"`
	AccessKeyID     storage.CredentialSource `koanf:"access_key_id"`
	SecretAccessKey storage.CredentialSource `koanf:"secret_access_key"`
	SessionToken    storage.CredentialSource `koanf:"session_token"`
	UsePathStyle    bool                     `koanf:"use_path_style"`
	PublicBucket    string                   `koanf:"public_bucket"`
	// CreateBucketIfMissing provisions the bucket(s) at startup when they
	// do not exist yet (QCD-FW-2). Opt-in; without it a missing bucket
	// still fails app.New loudly.
	CreateBucketIfMissing bool `koanf:"create_bucket_if_missing"`
}

// GCSStorageSpec mirrors pkg/storage.GCSConfig.
type GCSStorageSpec struct {
	Bucket            string                   `koanf:"bucket"`
	CredentialsSource storage.CredentialSource `koanf:"credentials"`
	PublicBucket      string                   `koanf:"public_bucket"`
}

// AzureStorageSpec mirrors pkg/storage.AzureConfig.
type AzureStorageSpec struct {
	AccountName     storage.CredentialSource `koanf:"account_name"`
	AccountKey      storage.CredentialSource `koanf:"account_key"`
	Container       string                   `koanf:"container"`
	PublicContainer string                   `koanf:"public_container"`
}

// LocalStorageSpec mirrors pkg/storage.LocalConfig.
type LocalStorageSpec struct {
	Path string `koanf:"path"`
}

// CleanupStorageSpec mirrors pkg/storage.CleanupConfig.
type CleanupStorageSpec struct {
	Enabled  bool   `koanf:"enabled"`
	Interval string `koanf:"interval"`
	Prefix   string `koanf:"prefix"`
	MaxAge   string `koanf:"max_age"`
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

	// LeaseOwner identifies THIS instance in the outbox lease rows
	// (QCD-FW-5). Empty (the default) derives a per-instance identifier
	// from the hostname and pid — every process used to share the literal
	// "nucleus-app", which made lease rows untraceable and let co-tenant
	// processes lease messages interchangeably. Set it explicitly for a
	// stable identity (e.g. a k8s pod name).
	LeaseOwner string `koanf:"lease_owner"`

	// MissingRoutePolicy controls what a dispatcher does with a leased
	// message whose topic has no registered bridge (QCD-FW-5):
	// "error" (default) fails the message; "ignore" releases it for the
	// instance that can deliver it — required in a deliberately
	// heterogeneous fleet where not every process registers every bridge.
	MissingRoutePolicy string `koanf:"missing_route_policy"`
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

	// RequireTenantStorage makes storage operations FAIL when the context
	// carries no tenant, instead of silently degrading to the shared
	// (unprefixed) key space — the trap a background job without request
	// scope falls into (NF-12). Off by default for compatibility: without
	// it, a multi-tenant application still gets a one-shot WARN the first
	// time an operation degrades.
	RequireTenantStorage bool `koanf:"require_tenant_storage"`
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

		LogLevel:      "info",
		LogFormat:     "json",
		MetricsPath:   "/metrics",
		MetricsPublic: true,

		RateLimitRequests: 0,
		RateLimitWindow:   time.Minute,
		RateLimitBurst:    0,
		RateLimitByRoute:  false,
		RateLimitByRole:   false,

		DefaultLocale: "en",
		LocalesPath:   "locales/",

		StaticPrefix: "/static/",
		StaticRoot:   "static/",

		Storage: StorageConfig{
			DefaultVisibility: "private",
			Provider:          "local",
			PublicPaths:       map[string]string{},
			PublicURLBase:     "",
			S3:                S3StorageSpec{},
			GCS:               GCSStorageSpec{},
			Azure:             AzureStorageSpec{},
			Local:             LocalStorageSpec{Path: "storage/"},
			Cleanup: CleanupStorageSpec{
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
		JobsProvider:    "memory",
		JobsConcurrency: 4,
		WebhooksPrefix:  "/webhooks",

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

	// 2. Load YAML file. The DEFAULT path (nucleus.yml) is optional — zero
	// mandatory config is a feature. An EXPLICITLY supplied path is a
	// promise: if it does not exist, running on defaults in silence turns a
	// typo into "overall ok" against the wrong database (DX-3), so it fails
	// loudly naming the path instead.
	cfgPath := "nucleus.yml"
	explicit := false
	if len(path) > 0 && path[0] != "" {
		cfgPath = path[0]
		explicit = true
	}
	if _, err := os.Stat(cfgPath); err == nil {
		fileK := koanf.New(".")
		if err := fileK.Load(file.Provider(cfgPath), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("app.LoadConfig file=%s: %w", cfgPath, err)
		}
		// DX-13: validate the FILE's keys against the schema before
		// merging, with the same did-you-mean the builder path gives —
		// `prot: 9999` used to run on defaults with `overall ok`.
		if err := validateConfigFileKeys(fileK.All(), declaredFrom(fileK)); err != nil {
			return nil, fmt.Errorf("app.LoadConfig file=%s: %w", cfgPath, err)
		}
		if err := k.Merge(fileK); err != nil {
			return nil, fmt.Errorf("app.LoadConfig file=%s: %w", cfgPath, err)
		}
	} else if explicit {
		return nil, fmt.Errorf("app.LoadConfig: config file %s does not exist (an explicit path must resolve; only the default nucleus.yml is optional): %w", cfgPath, err)
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
	if err := configbind.Unmarshal(k, &cfg); err != nil {
		return nil, fmt.Errorf("app.LoadConfig unmarshal: %w", err)
	}
	// A registered provider's subtree is not part of this schema, so the
	// unmarshal above skips it. Capture it here for the same reason the
	// builder path does: a backend that cannot read its own settings is a
	// backend nobody can deploy — and capturing on only one of the two
	// paths is how "the same file, two verdicts" comes back.
	cfg.StorageProviderConfig = providerns.CaptureStorage(k, string(cfg.Storage.Provider))
	cfg.AuthBackendConfig = providerns.CaptureAll(k, "auth", append(append([]string{}, cfg.AuthBackends...), federatedNames(cfg.AuthFederated)...))
	cfg.InterceptorConfig = providerns.CaptureAll(k, "interceptors", cfg.HTTPInterceptors)
	// Same rule, same implementation, both paths — see the builder loader.
	if err := providerns.OrphanAuthSubtreeError(providerns.OrphanAuthSubtrees(k, cfg.AuthBackends)); err != nil {
		return nil, fmt.Errorf("app.LoadConfig: %w", err)
	}
	if err := ApplyProfile(&cfg); err != nil {
		return nil, err
	}
	normalizeRuntimeConfig(&cfg)

	// ADR-010 §2 layers 3–4 on the EFFECTIVE config (post-profile, post-
	// normalisation — the same order the builder uses). Without this the
	// CLI accepted configs `go run .` rejects: `log_level: verbose` failed
	// the app and sailed through every `nucleus <cmd>` — the "same file,
	// two verdicts" class DX-13 closed for unknown keys, still alive here.
	if err := ValidateSemantics(&cfg); err != nil {
		return nil, err
	}
	if err := ValidateReferential(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ApplyProfile applies the named configuration preset (DX-23). The "dev"
// profile swaps every backing-service selection for its no-dependency
// counterpart so a realistic production config boots with zero external
// services: in-memory sessions and jobs, local filesystem storage, the
// no-op mailer, and a SQLite database (an already-SQLite URL is kept so
// the profile never moves an existing dev database). Extra database
// aliases — replicas, analytics — are dropped with the same rationale.
// Exported because the fluent loader (pkg/nucleus) produces its Config by
// its own merge and must apply the same preset semantics.
func ApplyProfile(cfg *Config) error {
	profile := strings.TrimSpace(cfg.Profile)
	switch profile {
	case "":
		return nil
	case "dev":
		cfg.SessionStore = "memory"
		cfg.SessionRedisURL = ""
		cfg.RedisURL = ""
		cfg.JobsProvider = "memory"
		cfg.JobsRedisURL = ""
		cfg.MailDriver = "noop"
		cfg.Storage.Provider = "local"
		if strings.TrimSpace(cfg.Storage.Local.Path) == "" {
			cfg.Storage.Local.Path = "storage/"
		}

		alias := cfg.DefaultDatabaseAlias()
		devDB := DatabaseConfig{URL: "sqlite://nucleus_dev.db"}
		if current, ok := cfg.Databases[alias]; ok && db.SystemFromURL(current.URL) == "sqlite" {
			devDB = current
		}
		cfg.Databases = map[string]DatabaseConfig{alias: devDB}
		return nil
	default:
		return fmt.Errorf("app.LoadConfig: unknown profile %q (supported: dev)", cfg.Profile)
	}
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
// ToStorageConfig renders the storage section as the storage package's own
// Config, including the raw subtree of a registered third-party provider.
// Exported so a caller assembling storage outside app.New — a test, an
// embedder — gets exactly what the framework would build.
func (c *Config) ToStorageConfig() storage.Config { return c.toStorageConfig() }

func (c *Config) toStorageConfig() storage.Config {
	cfg := storage.Config{
		DefaultVisibility: storage.Visibility(c.Storage.DefaultVisibility),
		PublicPaths:       make(map[string]string),
		PublicURLBase:     c.Storage.PublicURLBase,
	}

	// Provider name. The aliases below are the ones this framework has
	// always accepted for the S3 protocol; everything else passes through
	// verbatim so a backend registered with storage.RegisterProvider is
	// selectable by its own name. It used to fall through to "local",
	// which meant a typo — or a third-party provider whose package was not
	// imported — silently wrote to the local filesystem instead of failing.
	// storage.New now rejects an unregistered name, naming the registered
	// ones.
	switch name := strings.ToLower(strings.TrimSpace(c.Storage.Provider)); name {
	case "", "local":
		cfg.Provider = storage.ProviderLocal
	case "s3", "minio", "r2":
		cfg.Provider = storage.ProviderS3
	default:
		cfg.Provider = storage.ProviderType(name)
	}

	cfg.ProviderConfig = c.StorageProviderConfig

	// Copy public paths
	for k, v := range c.Storage.PublicPaths {
		cfg.PublicPaths[k] = v
	}

	// Provider-specific config
	cfg.S3 = storage.S3Config{
		Endpoint:              c.Storage.S3.Endpoint,
		Bucket:                c.Storage.S3.Bucket,
		Region:                c.Storage.S3.Region,
		AccessKeyID:           c.Storage.S3.AccessKeyID,
		SecretAccessKey:       c.Storage.S3.SecretAccessKey,
		SessionToken:          c.Storage.S3.SessionToken,
		UsePathStyle:          c.Storage.S3.UsePathStyle,
		PublicBucket:          c.Storage.S3.PublicBucket,
		CreateBucketIfMissing: c.Storage.S3.CreateBucketIfMissing,
	}

	cfg.GCS = storage.GCSConfig{
		Bucket:            c.Storage.GCS.Bucket,
		CredentialsSource: c.Storage.GCS.CredentialsSource,
		PublicBucket:      c.Storage.GCS.PublicBucket,
	}

	cfg.Azure = storage.AzureConfig{
		AccountName:     c.Storage.Azure.AccountName,
		AccountKey:      c.Storage.Azure.AccountKey,
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

	// Terminal default for direct-struct configs that never pass through
	// DefaultConfig (the legacy storage_path middle step was removed in
	// v0.12.0, DEP-2026-005).
	if cfg.Local.Path == "" {
		cfg.Local.Path = "storage/"
	}

	return cfg
}

// federatedNames is the instance names declared in auth_federated. It is
// the bridge between the configuration and the one place the exemption
// rule lives (internal/providerns): a federated instance's subtree is
// legitimate because the operator DECLARED it, not because anything
// registered that name.
func federatedNames(instances []auth.FederatedInstance) []string {
	out := make([]string, 0, len(instances))
	for _, inst := range instances {
		if name := strings.ToLower(strings.TrimSpace(inst.Name)); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// DeclaredProviders is what the validators need from this file to apply
// the exemption rule. Both configuration paths build it the same way,
// which is what stops "the same file, two verdicts" from happening a
// fourth time.
func (c *Config) DeclaredProviders() providerns.Declared {
	return providerns.Declared{FederatedAuth: federatedNames(c.AuthFederated)}
}

// declaredFrom reads the federated instance names straight out of the
// file being validated.
//
// The keys are checked BEFORE the file is merged and unmarshalled, so the
// Config does not exist yet — and the exemption for `auth.<instance>.*`
// depends on what this very file declares. Reading it here keeps the
// order honest: a subtree is exempt because the file that carries it also
// declares the instance, not because some other layer did.
func declaredFrom(k *koanf.Koanf) providerns.Declared {
	var out []string
	for _, raw := range k.Slices("auth_federated") {
		if name := strings.ToLower(strings.TrimSpace(raw.String("name"))); name != "" {
			out = append(out, name)
		}
	}
	return providerns.Declared{FederatedAuth: out}
}
