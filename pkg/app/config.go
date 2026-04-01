// Package app provides the application configuration and bootstrap for GoFrame.
// Configuration is loaded from multiple sources with increasing precedence:
// struct defaults < YAML file < environment variables (prefix GOFRAME_).
package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// Config holds all framework configuration. Every field has a sensible default
// for local development so zero configuration is required to get started.
type Config struct {
	// Server
	Host         string        `koanf:"host"`
	Port         int           `koanf:"port"`
	ReadTimeout  time.Duration `koanf:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout"`
	IdleTimeout  time.Duration `koanf:"idle_timeout"`

	// Database
	DatabaseEngine      string        `koanf:"database_engine"`
	DatabaseURL         string        `koanf:"database_url"`
	DatabaseMaxOpen     int           `koanf:"database_max_open"`
	DatabaseMaxIdle     int           `koanf:"database_max_idle"`
	DatabaseMaxLifetime time.Duration `koanf:"database_max_lifetime"`

	// Redis (optional — empty disables Redis-backed features)
	RedisURL string `koanf:"redis_url"`

	// Auth
	JWTSecret       string        `koanf:"jwt_secret"`
	JWTExpiry       time.Duration `koanf:"jwt_expiry"`
	SessionLifetime time.Duration `koanf:"session_lifetime"`

	// Admin
	AdminPrefix string `koanf:"admin_prefix"`
	AdminTitle  string `koanf:"admin_title"`

	// Mail
	SMTPHost string `koanf:"smtp_host"`
	SMTPPort int    `koanf:"smtp_port"`
	SMTPUser string `koanf:"smtp_user"`
	SMTPPass string `koanf:"smtp_pass"`
	MailFrom string `koanf:"mail_from"`

	// Observability
	LogLevel     string `koanf:"log_level"`
	LogFormat    string `koanf:"log_format"`
	OTLPEndpoint string `koanf:"otlp_endpoint"`
	MetricsPath  string `koanf:"metrics_path"`

	// Security
	RateLimitRequests int           `koanf:"rate_limit_requests"`
	RateLimitWindow   time.Duration `koanf:"rate_limit_window"`

	// i18n
	DefaultLocale string `koanf:"default_locale"`
	LocalesPath   string `koanf:"locales_path"`

	// Static files
	StaticPrefix string `koanf:"static_prefix"`
	StaticRoot   string `koanf:"static_root"`

	// File storage
	StorageDriver string `koanf:"storage_driver"`
	StoragePath   string `koanf:"storage_path"`

	// Environment
	Env   string `koanf:"env"`
	Debug bool   `koanf:"debug"`
}

// defaults returns a Config populated with sensible development defaults.
func defaults() Config {
	return Config{
		Host:         "0.0.0.0",
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,

		DatabaseEngine:      "bun",
		DatabaseURL:         "sqlite://goframe.db",
		DatabaseMaxOpen:     25,
		DatabaseMaxIdle:     5,
		DatabaseMaxLifetime: 5 * time.Minute,

		JWTExpiry:       24 * time.Hour,
		SessionLifetime: 72 * time.Hour,

		AdminPrefix: "/admin",
		AdminTitle:  "GoFrame Admin",

		SMTPPort: 587,
		MailFrom: "noreply@localhost",

		LogLevel:    "info",
		LogFormat:   "json",
		MetricsPath: "/metrics",

		RateLimitRequests: 0,
		RateLimitWindow:   time.Minute,

		DefaultLocale: "en",
		LocalesPath:   "locales/",

		StaticPrefix: "/static/",
		StaticRoot:   "static/",

		StorageDriver: "local",
		StoragePath:   "uploads/",

		Env:   "development",
		Debug: false,
	}
}

// LoadConfig loads configuration from multiple sources with increasing precedence:
// 1. Struct defaults
// 2. YAML file (optional — path argument or "goframe.yaml" in current directory)
// 3. Environment variables with prefix GOFRAME_
//
// If no path is provided and "goframe.yaml" does not exist, only defaults and
// env vars are used.
func LoadConfig(path ...string) (*Config, error) {
	k := koanf.New(".")

	// 1. Load struct defaults
	if err := k.Load(structs.Provider(defaults(), "koanf"), nil); err != nil {
		return nil, fmt.Errorf("app.LoadConfig defaults: %w", err)
	}

	// 2. Load YAML file
	cfgPath := "goframe.yaml"
	if len(path) > 0 && path[0] != "" {
		cfgPath = path[0]
	}
	if _, err := os.Stat(cfgPath); err == nil {
		if err := k.Load(file.Provider(cfgPath), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("app.LoadConfig file=%s: %w", cfgPath, err)
		}
	}

	// 3. Load environment variables (GOFRAME_PORT -> port)
	if err := k.Load(env.Provider("GOFRAME_", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "GOFRAME_"))
	}), nil); err != nil {
		return nil, fmt.Errorf("app.LoadConfig env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("app.LoadConfig unmarshal: %w", err)
	}

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
