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
