// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	netmail "net/mail"
	"net/url"
	"os"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Module takes a finished *Service, and a Service needs a database handle and
// a mailer BEFORE the application exists — which is when neither is there:
// the framework opens the database and builds the mail sender inside
// nucleus.New().…Start(). So an application that wanted the account flows
// opened a second handle to its own database, built a sender of its own,
// and — on the api starter, which builds no mail sender at all — had
// registration answer 500 at the first request, because the flow refuses to
// pretend it sent a confirmation link (A11 S0, EN-04).
//
// FromRuntime builds the same Service at start-up out of what the
// application already has: its default database, its mail sender and its
// session manager. It is what `nucleus add accounts` mounts.

// RuntimeConfig is the configuration of the account flows FromRuntime
// builds, bound from `modules.accounts.*` in nucleus.yml.
type RuntimeConfig struct {
	// BaseURL is the address the links in account mail point at — the
	// address the browser reaches this application at, e.g.
	// "https://app.example.com". Required: a link built from the request's
	// Host header is a link an attacker chooses.
	BaseURL string `koanf:"base_url"`
	// From is the sender of account mail, e.g. "no-reply@example.com".
	// Required.
	From string `koanf:"from"`
	// AllowUnverifiedLogin lets an account sign in before its address is
	// confirmed. Off by default: the flows built here always have a mail
	// sender, so the confirmation link always goes out.
	AllowUnverifiedLogin bool `koanf:"allow_unverified_login"`
	// MinPasswordLength defaults to 12 (Config.MinPasswordLength).
	MinPasswordLength int `koanf:"min_password_length"`
	// Issuer is the product name an authenticator app shows next to a
	// code. Defaults to "Nucleus".
	Issuer string `koanf:"issuer"`
	// MFAKeyEnv names the environment variable that holds the key second
	// factors are encrypted with: 32 bytes, base64-encoded (openssl rand
	// -base64 32). Empty refuses second factors rather than storing their
	// secrets in the clear (Config.MFAEncryptionKey); a name whose variable
	// is unset or does not decode to 32 bytes fails start-up.
	MFAKeyEnv string `koanf:"mfa_key_env"`
}

// RuntimeModuleName is the name FromRuntime mounts under: its configuration
// is `modules.accounts.*`. It is the name Module mounts under too, so an
// application mounts one or the other.
const RuntimeModuleName = "accounts"

// FromRuntime returns the module that builds the account flows from the
// running application and serves them — the routes Module serves:
//
//	nucleus.New().
//	    FromConfigFile("nucleus.yml").
//	    WithoutDefaults().
//	    WithMail().
//	    Mount(accounts.FromRuntime()).
//	    Start()
//
// At start-up it opens the account tables (SQLStore, created if missing) on
// the application's default database — SQLite, PostgreSQL or MySQL; another
// engine fails start-up with its name — and sends account mail through the
// application's mail sender (Runtime.Mailer). It refuses to start when that
// sender delivers nothing: an application built WithoutDefaults() without
// WithMail() has none, and the default mail_driver, noop, discards every
// message. A registration that answered 202 while the confirmation link went
// nowhere would leave every account unconfirmable, so the refusal names the
// keys instead: mail_driver with smtp_host and smtp_port, a
// nucleus-plugin-<driver>, or mail_driver: log in development, which writes
// each message to the application log.
//
// A signed-in account is recorded in the session under SessionKeyAccountID
// and, for the framework's default-deny layer, under auth.SessionKeySubject:
// a policy row for the account's id applies to its session as it applies to
// a bearer token with that user id and to an API key it owns. On the default
// stack the module grants the anonymous subject its routes — the person
// registering or signing in has no identity yet; the routes that need one
// (a password change, a second factor) check the session themselves.
func FromRuntime() nucleus.ModuleSpec {
	var service *Service
	return nucleus.Module[RuntimeConfig]{
		Name:     RuntimeModuleName,
		Policies: anonymousPolicies(),
		OnStart: func(ctx context.Context, rt nucleus.Runtime, cfg RuntimeConfig) error {
			svc, err := serviceFromRuntime(ctx, rt, cfg)
			if err != nil {
				return err
			}
			service = svc
			return nil
		},
		Routes: func(r nucleus.Router, _ RuntimeConfig) {
			mountRoutes(r, service)
		},
	}.Build()
}

// anonymousPolicies grants the anonymous subject every account route: the
// flows are how someone without an identity gets one.
func anonymousPolicies() []nucleus.PolicyRule {
	routes := []string{
		RouteRegister, RouteVerifyEmail, RouteLogin, RouteLogout,
		RoutePasswordReset, RoutePasswordChange, RouteMagicLink,
		RouteTOTP, RouteMFAVerify, RouteRecoveryCodes,
	}
	rules := make([]nucleus.PolicyRule, 0, len(routes))
	for _, route := range routes {
		rules = append(rules, nucleus.PolicyRule{Subject: authz.BootstrapSubject, Object: route, Action: "*"})
	}
	return rules
}

// serviceFromRuntime is FromRuntime's start-up: the configuration checked,
// the store opened on the default database, the mailer taken from the
// application and refused when it delivers nothing.
func serviceFromRuntime(ctx context.Context, rt nucleus.Runtime, cfg RuntimeConfig) (*Service, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, errors.New("accounts: modules.accounts.base_url is empty — the links in account mail need the address the browser reaches this application at, e.g. https://app.example.com")
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("accounts: modules.accounts.base_url %q is not an http(s) address", base)
	}
	from := strings.TrimSpace(cfg.From)
	if from == "" {
		return nil, errors.New("accounts: modules.accounts.from is empty — account mail needs a sender, e.g. no-reply@example.com")
	}
	if _, err := netmail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("accounts: modules.accounts.from %q is not a mail address", from)
	}

	mailer := rt.Mailer()
	switch {
	case mailer == nil:
		return nil, errors.New("accounts: the application builds no mail sender, and the account flows mail the links that confirm an address and reset a password — " +
			"an application built WithoutDefaults() needs WithMail() in its nucleus.New() chain (nucleus add accounts writes it), and a mail_driver")
	case mail.Discards(mailer):
		return nil, errors.New("accounts: mail_driver is noop, which delivers nothing, and the account flows mail the links that confirm an address and reset a password — " +
			"set mail_driver: smtp with smtp_host and smtp_port, mail_driver: <name> for a nucleus-plugin-<name>, or mail_driver: log in development, which writes each message to the application log")
	}

	db := rt.DB()
	handle := rt.DatabaseHandle()
	if db == nil || handle == nil {
		return nil, errors.New("accounts: the account tables live in the default database, and none is configured")
	}
	flavor, err := flavorOf(handle.System())
	if err != nil {
		return nil, err
	}
	store, err := NewSQLStore(ctx, db, SQLStoreConfig{Flavor: flavor})
	if err != nil {
		return nil, err
	}

	var key []byte
	if name := strings.TrimSpace(cfg.MFAKeyEnv); name != "" {
		key, err = mfaKeyFromEnv(name)
		if err != nil {
			return nil, err
		}
	}

	return New(store, rt.Session(), mailer, nil, Config{
		BaseURL:                  base,
		From:                     from,
		RequireEmailVerification: !cfg.AllowUnverifiedLogin,
		MinPasswordLength:        cfg.MinPasswordLength,
		Issuer:                   strings.TrimSpace(cfg.Issuer),
		MFAEncryptionKey:         key,
	}, rt.Logger())
}

// flavorOf maps the database system the framework resolved from the URL onto
// the dialect SQLStore speaks.
func flavorOf(system string) (Flavor, error) {
	switch strings.ToLower(strings.TrimSpace(system)) {
	case "sqlite":
		return FlavorSQLite, nil
	case "postgresql", "postgres":
		return FlavorPostgres, nil
	case "mysql":
		return FlavorMySQL, nil
	}
	return "", fmt.Errorf("accounts: the account store speaks sqlite, postgres and mysql; the default database is %s — implement accounts.Store and mount accounts.Module(svc)", system)
}

// mfaKeyFromEnv reads the second-factor encryption key from the variable the
// configuration names.
func mfaKeyFromEnv(name string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("accounts: modules.accounts.mfa_key_env names %s, which is not set — it holds 32 bytes, base64-encoded (openssl rand -base64 32)", name)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		key, err = base64.RawURLEncoding.DecodeString(raw)
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("accounts: %s does not hold 32 bytes, base64-encoded (openssl rand -base64 32)", name)
	}
	return key, nil
}
