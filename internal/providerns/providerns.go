// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package providerns answers one question for every configuration
// validator in the framework: does this key belong to the private subtree
// of a REGISTERED provider, rather than to app.Config's schema?
//
// It exists because that question was answered twice, differently. The
// builder path exempted `storage.<provider>.*` for a registered provider;
// app.LoadConfig — the path every CLI command takes — did not, so a Ceph
// deployment that booted fine had `nucleus check`, `doctor` and `config
// print` all reporting an unknown key against the file the server was
// running on. That is the "same file, two verdicts" class, and the fix is
// not to teach the second validator the same rule but to leave exactly one
// place where the rule lives.
//
// The exemption is per REGISTERED name and never for the namespace: a
// misspelling under `storage.` or `auth.` is still an unknown key. A
// namespace-wide exemption would turn these into the one place in the
// configuration where any typo passes unseen.
//
// "Registered" has one more source than the registries of the running
// binary: the providers the PROJECT links. The `nucleus` binary imports no
// provider module, so the CLI reads the project's imports and declares
// them (Declared, SetLinked); a running application needs nothing of the
// kind, because its registries already hold what main.go imports.
package providerns

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/knadh/koanf/v2"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// Namespaces returns every config namespace that can hold a registered
// provider's own subtree, mapped to the names currently registered in it.
//
// This is the single table both validators read. A registry whose
// providers can declare open-ended configuration belongs here; one whose
// factory takes a typed struct the framework owns (mail, the session
// store) does not, because there is no subtree to exempt.
func Namespaces() map[string][]string { return NamespacesWith(Declared{}) }

// Declared carries the parts of the rule that come from the CONFIGURATION
// rather than from a registry.
//
// A credential backend is exempted by its REGISTERED name, because the
// name an operator writes is the name the package registered. A federated
// identity provider is not: its registry is keyed by protocol ("oidc")
// while the subtree is keyed by the INSTANCE the operator named ("corp"),
// so the only place that knows "corp" is legitimate is the declaration in
// auth_federated. Passing it here rather than teaching each validator
// keeps the rule in one place, which is the entire reason this package
// exists — the exemption already lived in two validators once, and only
// one of them had it.
//
// The other three lists carry what the PROJECT links rather than what this
// binary registers. A provider published as its own module (providers/*)
// registers when its package is imported, and the `nucleus` binary imports
// none of them: its registries do not know `s3`, `ldap` or `sentry`, while
// the application whose main.go imports them does. The CLI reads those
// imports (knownproviders.LinkedIn) and passes the names here, so `doctor`,
// `health` and `config print` accept the block `nucleus add sentry` wrote
// exactly when the running application would (NU-132) — and keep refusing
// it when the import is missing, naming the line to add.
type Declared struct {
	// FederatedAuth are the instance names declared in auth_federated.
	FederatedAuth []string
	// Storage, Auth and Interceptors are the provider names the project
	// links (by import) that this binary's registries may not know.
	Storage      []string
	Auth         []string
	Interceptors []string
}

// FromEntries is the Declared view of catalog entries a project links: each
// entry's registered name under the namespace its group configures. Entries
// of other groups (drivers, exporters, secrets resolvers) own no
// configuration subtree and are left out.
func FromEntries(entries []knownproviders.Entry) Declared {
	var d Declared
	for _, e := range entries {
		name := strings.ToLower(strings.TrimSpace(e.Key))
		if name == "" {
			continue
		}
		switch e.Group {
		case knownproviders.GroupStorage:
			d.Storage = append(d.Storage, name)
		case knownproviders.GroupAuth:
			d.Auth = append(d.Auth, name)
		case knownproviders.GroupInterceptor:
			d.Interceptors = append(d.Interceptors, name)
		}
	}
	return d
}

// Merge returns d with other's names added.
func (d Declared) Merge(other Declared) Declared {
	return Declared{
		FederatedAuth: append(append([]string{}, d.FederatedAuth...), other.FederatedAuth...),
		Storage:       append(append([]string{}, d.Storage...), other.Storage...),
		Auth:          append(append([]string{}, d.Auth...), other.Auth...),
		Interceptors:  append(append([]string{}, d.Interceptors...), other.Interceptors...),
	}
}

// The providers the project links, set once per command by the CLI (from
// the project's imports) and read by every validator through
// NamespacesWith. Process-wide on purpose: a `nucleus` process serves one
// command against one project, and both configuration loaders (pkg/app
// and the builder's) must give that project the same answer. A running
// application never sets it — its registries already hold what it links.
var (
	linkedMu sync.RWMutex
	linked   Declared
)

// SetLinked records the providers the project links; see Declared.
func SetLinked(d Declared) {
	linkedMu.Lock()
	defer linkedMu.Unlock()
	linked = Declared{Storage: d.Storage, Auth: d.Auth, Interceptors: d.Interceptors}
}

// Linked returns what SetLinked recorded.
func Linked() Declared {
	linkedMu.RLock()
	defer linkedMu.RUnlock()
	return linked
}

// NamespacesWith is Namespaces for a caller that has read the file and
// therefore knows which federated instances were declared. The names the
// project links (d's lists and SetLinked's) are added to the registered
// ones, each under its namespace.
func NamespacesWith(d Declared) map[string][]string {
	d = d.Merge(Linked())
	add := func(registered []string, extra ...[]string) []string {
		out := append([]string{}, registered...)
		for _, names := range extra {
			for _, name := range names {
				name = strings.ToLower(strings.TrimSpace(name))
				if name != "" {
					out = append(out, name)
				}
			}
		}
		return out
	}
	return map[string][]string{
		"storage":      add(storage.RegisteredProviders(), d.Storage),
		"auth":         add(auth.RegisteredBackends(), d.Auth, d.FederatedAuth),
		"interceptors": add(interceptor.Registered(), d.Interceptors),
	}
}

// IsProviderKey reports whether key sits under `<namespace>.<registered
// name>.` for one of the namespaces above.
//
// A key with only two segments (`storage.ceph`) is NOT a provider key: the
// subtree is what a provider owns, and the bare name is either a schema
// field or a typo.
func IsProviderKey(key string, d Declared) bool {
	ns, rest, ok := strings.Cut(key, ".")
	if !ok {
		return false
	}
	name, _, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	for _, registered := range NamespacesWith(d)[ns] {
		if registered == name {
			return true
		}
	}
	return false
}

// StripKeys removes every registered provider's subtree from an
// unknown-key set.
func StripKeys(keys []string, d Declared) []string {
	if len(keys) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if IsProviderKey(k, d) {
			continue
		}
		out = append(out, k)
	}
	return out
}

// Capture cuts `<ns>.<name>.*` out of a merged koanf so it can be handed
// to the provider that owns it. The framework never interprets the
// contents; the provider binds them into its own typed struct.
//
// koanf appears here and not in any exported signature on purpose: a
// third-party provider must not inherit a dependency on the framework's
// configuration decoder (ADR-015).
func Capture(k *koanf.Koanf, ns, name string) map[string]any {
	name = strings.ToLower(strings.TrimSpace(name))
	if k == nil || name == "" {
		return nil
	}
	raw := k.Cut(ns).Cut(name).Raw()
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// CaptureAll captures the subtree of every name in names, skipping the
// ones that turn out to be empty.
func CaptureAll(k *koanf.Koanf, ns string, names []string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, name := range names {
		if sub := Capture(k, ns, name); len(sub) > 0 {
			out[strings.ToLower(strings.TrimSpace(name))] = sub
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CaptureStorage captures a storage provider's subtree, skipping the
// built-ins.
//
// The built-ins bind through their own typed fields (storage.s3.*,
// storage.local.*), so there is nothing to capture for them — and
// capturing anyway would hand a provider a subtree the schema already
// owns. The list lives here and not at either call site because both
// configuration paths need the same answer.
func CaptureStorage(k *koanf.Koanf, provider string) map[string]any {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "local", "s3", "minio", "r2", "gcs", "azure":
		return nil
	default:
		return Capture(k, "storage", provider)
	}
}

// OrphanAuthSubtrees returns the `auth.<name>.*` sections that belong to a
// registered backend which the chain does not name.
//
// The unknown-key guard cannot see these: the name IS registered, so the
// section is legitimately exempt — and then nothing reads it, because the
// chain is its only consumer. An operator who configures a directory and
// forgets the `auth_backends` entry gets a clean boot, a green `check`,
// and a login page that never consults the directory. That is the "exit 0
// without the effect" class, and it is worth an error rather than a
// warning: there is no reading of this configuration under which it does
// something.
//
// Storage deliberately gets no equivalent. `storage.s3.*` while
// `storage.provider` is `local` is a stanza kept for another environment,
// and the schema has always allowed it; a third-party section is the same
// thing and must not be treated more harshly than the built-in one.
func OrphanAuthSubtrees(k *koanf.Koanf, chain []string) []string {
	if k == nil {
		return nil
	}
	declared := map[string]struct{}{}
	for _, name := range chain {
		declared[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	registered := map[string]struct{}{}
	for _, name := range Namespaces()["auth"] {
		registered[name] = struct{}{}
	}

	var orphans []string
	for key := range k.Cut("auth").Raw() {
		name := strings.ToLower(strings.TrimSpace(key))
		if _, isRegistered := registered[name]; !isRegistered {
			continue
		}
		if _, inChain := declared[name]; inChain {
			continue
		}
		orphans = append(orphans, name)
	}
	sort.Strings(orphans)
	return orphans
}

// OrphanAuthSubtreeError renders the orphans as the error both
// configuration paths return, so the same file cannot get two verdicts.
func OrphanAuthSubtreeError(orphans []string) error {
	if len(orphans) == 0 {
		return nil
	}
	sections := make([]string, 0, len(orphans))
	for _, name := range orphans {
		sections = append(sections, "auth."+name)
	}
	return fmt.Errorf("%s is configured but %s not listed in auth_backends, so nothing reads it — a login would never consult it. Add it to the chain (order matters: [%s, local] tries the directory first and still lets a local account in when it is unreachable), or remove the section",
		strings.Join(sections, ", "),
		map[bool]string{true: "is", false: "are"}[len(orphans) == 1],
		orphans[0])
}

// WritesStorage reports whether a set of loaded configuration keys — one
// file's, flattened the way koanf flattens them — writes storage: any
// storage.* key carrying a value. A key set to null unsets rather than
// writes, so it does not count. Both loaders call it, so a file declares
// storage (app.Config.StorageDeclared) on the same terms on either path —
// the reason this lives here and not in each of them.
func WritesStorage(keys map[string]any) bool {
	for key, val := range keys {
		if val != nil && (key == "storage" || strings.HasPrefix(key, "storage.")) {
			return true
		}
	}
	return false
}

// MailDriverKey is the key that selects the mail sender.
const MailDriverKey = "mail_driver"

// WritesMail is WritesStorage for mail: whether a set of loaded
// configuration keys writes mail_driver with a value. Only the driver
// counts — smtp_* without it select nothing, the default driver being noop
// on every application — and a null unsets rather than writes. Both loaders
// call it (app.Config.MailDeclared, NU-114).
func WritesMail(keys map[string]any) bool {
	val, ok := keys[MailDriverKey]
	return ok && val != nil
}

// NotInstalled reports whether key belongs to the configuration subtree of a
// backend this project publishes as its own module — `auth.ldap.url`,
// `storage.<provider>.*`, `interceptors.sentry.*` — and returns that
// backend. Such a key is only ever unknown because the module is not linked:
// once it is, the backend registers and IsProviderKey exempts its subtree.
// So the honest answer for it is not "unknown key, did you mean
// databases.<alias>.url?" but "not installed", with the command that
// installs it (NU-100).
func NotInstalled(key string) (knownproviders.Provider, bool) {
	ns, rest, ok := strings.Cut(key, ".")
	if !ok {
		return knownproviders.Provider{}, false
	}
	name, _, ok := strings.Cut(rest, ".")
	if !ok {
		return knownproviders.Provider{}, false
	}
	switch ns {
	case "auth":
		return knownproviders.AuthBackend(name)
	case "storage":
		return knownproviders.StorageProvider(name)
	case "interceptors":
		return knownproviders.Interceptor(name)
	}
	return knownproviders.Provider{}, false
}

// NotInstalledTag is the short note an unknown-key line carries for a key
// NotInstalled recognises, in place of a did-you-mean.
func NotInstalledTag(p knownproviders.Provider) string {
	return "not installed: `nucleus add " + p.Name + "`"
}

// NotInstalledNote explains, once per backend, the keys an unknown-key
// refusal tagged as not installed: what they configure, the module it ships
// as, the command that installs it and the import line it writes — the one
// the project's main.go lacks when the CLI read it. Empty when no key was
// tagged. Both configuration paths append it, so the same file gets the
// same answer from `go run .` and from `nucleus check`.
func NotInstalledNote(unknown []string) string {
	seen := map[string]bool{}
	var b strings.Builder
	for _, key := range unknown {
		p, ok := NotInstalled(key)
		if !ok || seen[p.Module] {
			continue
		}
		seen[p.Module] = true
		ns, _, _ := strings.Cut(key, ".")
		fmt.Fprintf(&b, "\n\n%s.%s.* configures the %s %s, which ships as its own module (%s) and is not linked: the project's main.go does not import it. "+
			"`nucleus add %s` installs it: the go get and the blank import `_ %q`.",
			ns, p.Name, p.Name, p.Kind, p.Module, p.Name, p.ImportPath())
	}
	return b.String()
}
