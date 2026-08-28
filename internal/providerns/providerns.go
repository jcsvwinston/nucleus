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
package providerns

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knadh/koanf/v2"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// Namespaces returns every config namespace that can hold a registered
// provider's own subtree, mapped to the names currently registered in it.
//
// This is the single table both validators read. A registry whose
// providers can declare open-ended configuration belongs here; one whose
// factory takes a typed struct the framework owns (mail, the session
// store) does not, because there is no subtree to exempt.
func Namespaces() map[string][]string {
	return map[string][]string{
		"storage": storage.RegisteredProviders(),
		"auth":    auth.RegisteredBackends(),
	}
}

// IsProviderKey reports whether key sits under `<namespace>.<registered
// name>.` for one of the namespaces above.
//
// A key with only two segments (`storage.ceph`) is NOT a provider key: the
// subtree is what a provider owns, and the bare name is either a schema
// field or a typo.
func IsProviderKey(key string) bool {
	ns, rest, ok := strings.Cut(key, ".")
	if !ok {
		return false
	}
	name, _, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	for _, registered := range Namespaces()[ns] {
		if registered == name {
			return true
		}
	}
	return false
}

// StripKeys removes every registered provider's subtree from an
// unknown-key set.
func StripKeys(keys []string) []string {
	if len(keys) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if IsProviderKey(k) {
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
