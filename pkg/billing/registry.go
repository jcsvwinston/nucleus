// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/internal/providerconfig"
)

// Config carries what the framework hands a provider's factory: the name
// the operator selected it by (`billing.provider`) and its own
// configuration subtree (`billing.<name>.*`).
type Config struct {
	// Name is the registered name `billing.provider` selected.
	Name string

	// ProviderConfig is the raw `billing.<name>.*` subtree. Read it with
	// Bind rather than reaching into the map.
	ProviderConfig map[string]any

	// Logger is the application's logger. Never nil when the framework
	// calls the factory.
	Logger *slog.Logger
}

// Bind decodes the provider's configuration subtree into dst — a pointer to
// a struct with koanf tags — and fills still-zero fields from their
// `default:` tags.
//
// A key dst does not declare is an error, not a line ignored: a misspelled
// webhook secret is a webhook route that refuses every delivery, or one
// that accepts what it should not.
func (c Config) Bind(dst any) error {
	name := c.Name
	if name == "" {
		name = "billing"
	}
	return providerconfig.Bind("billing."+name, c.ProviderConfig, dst)
}

// Factory builds a configured provider. An error fails the boot: an
// application that bills and cannot reach its provider, or cannot verify
// its webhooks, must not start as if it could.
type Factory func(cfg Config) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

func normalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Register makes a provider selectable by name from `billing.provider`.
// Call it from an init function of the provider's package (MustRegister is
// the form for that), and import the package for its side effect.
//
// A name already taken is an error rather than a replacement: two packages
// claiming "stripe" would make the provider that charges customers depend
// on the order of an import block.
func Register(name string, factory Factory) error {
	name = normalize(name)
	if name == "" {
		return errors.New("billing: a provider needs a name")
	}
	if strings.ContainsAny(name, "/ \t\r\n?#.") {
		// The name is a configuration key (billing.<name>) and a path
		// segment of the webhook route (/webhooks/billing/<name>).
		return fmt.Errorf("billing: provider name %q must be a single word: it names a configuration key and a URL path segment", name)
	}
	if factory == nil {
		return fmt.Errorf("billing: provider %q: factory cannot be nil", name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		return fmt.Errorf("billing: provider %q is already registered", name)
	}
	registry[name] = factory
	return nil
}

// MustRegister is Register for an init function: it panics with Register's
// error.
func MustRegister(name string, factory Factory) {
	if err := Register(name, factory); err != nil {
		panic(err)
	}
}

// RegisteredProviders returns every name `billing.provider` accepts in this
// binary, sorted.
func RegisteredProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Unregister removes a registered provider. It exists for tests that
// register a fake and must not leak it into the next one.
func Unregister(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, normalize(name))
}

// Open builds the provider cfg.Name selects and returns it ready to use.
//
// A name this project publishes and the binary does not link — "stripe"
// without the module's import — is refused with the command that installs
// it; any other unregistered name is refused naming what is registered.
func Open(cfg Config) (*Billing, error) {
	name := normalize(cfg.Name)
	if name == "" {
		return nil, errors.New("billing: no provider selected (billing.provider is empty)")
	}
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		registered := strings.Join(RegisteredProviders(), ", ")
		if registered == "" {
			registered = "none"
		}
		if p, ours := knownproviders.BillingProvider(name); ours {
			return nil, fmt.Errorf("billing: provider %q ships as its own module and is not imported yet (registered: %s).\n\n"+
				"\tAdd it to your build:\n\n%s",
				name, registered, p.InstallHint())
		}
		return nil, fmt.Errorf("billing: provider %q is not registered (registered: %s) — a provider registers itself with billing.Register when its package is imported",
			cfg.Name, registered)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	cfg.Name = name
	p, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("billing: provider %q: %w", name, err)
	}
	if p == nil {
		return nil, fmt.Errorf("billing: provider %q: the factory returned no provider and no error", name)
	}
	return New(name, p), nil
}
