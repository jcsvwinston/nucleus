// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package knownproviders is the catalog: every name `nucleus add` installs,
// `nucleus new --with` resolves and the runtime's refusals point at (ADR-034).
//
// A registry can only report that a name is not registered. That is true
// and nearly useless when the name is one WE publish: the operator wrote
// `ldap` because the documentation told them to, and the answer they need
// is not "unknown" but "you have not imported it yet, here is the line".
//
// The table changes nothing about registration. A name here is registered
// exactly when its package is imported, like any other — this only makes
// the error say what to do about it. Keeping it as data means the core
// carries the NAME of a satellite module and none of its code.
//
// The functions in this file are the views each subsystem has always used,
// keyed the way that subsystem's registry is keyed; the table itself is in
// catalog.go.
package knownproviders

import "strings"

// DBDriver looks a database/sql driver NAME up ("pgx", "sqlserver") — the
// name sql.Open fails on, which is where the guidance has to appear.
func DBDriver(name string) (Provider, bool) {
	return byKey(GroupDriver, name)
}

// DBDriverNames returns the driver names this project publishes, sorted so
// that an error message lists them the same way every time.
func DBDriverNames() []string { return keysOf(GroupDriver) }

// TelemetryExporter looks an exporter name up.
func TelemetryExporter(name string) (Provider, bool) {
	return byKey(GroupExporter, name)
}

// TelemetryExporterNames returns the exporter names this project publishes,
// sorted.
func TelemetryExporterNames() []string { return keysOf(GroupExporter) }

// StorageProvider returns the description of a first-party storage backend
// published as a separate module.
func StorageProvider(name string) (Provider, bool) {
	return byKey(GroupStorage, strings.ToLower(strings.TrimSpace(name)))
}

// StorageProviderNames returns every first-party storage provider name,
// sorted.
func StorageProviderNames() []string { return keysOf(GroupStorage) }

// AuthBackend returns the description of a first-party authentication
// backend published as a separate module.
func AuthBackend(name string) (Provider, bool) {
	return byKey(GroupAuth, strings.ToLower(strings.TrimSpace(name)))
}

// AuthBackendNames returns every first-party authentication backend name,
// sorted.
func AuthBackendNames() []string { return keysOf(GroupAuth) }

// SecretsResolver returns the description of a first-party secrets resolver
// published as a separate module, keyed by its reference scheme ("aws-sm:").
func SecretsResolver(scheme string) (Provider, bool) {
	return byKey(GroupSecrets, strings.ToLower(strings.TrimSpace(scheme)))
}

// FederatedProvider returns the description of a first-party federated
// sign-in provider, keyed by the provider name auth_federated declares.
func FederatedProvider(name string) (Provider, bool) {
	return byKey(GroupFederated, strings.ToLower(strings.TrimSpace(name)))
}

// Interceptor returns the description of a request interceptor this project
// publishes as a separate module, keyed by the name it registers with the
// interceptor registry — the name http_interceptors lists.
func Interceptor(name string) (Provider, bool) {
	return byKey(GroupInterceptor, strings.ToLower(strings.TrimSpace(name)))
}

// InterceptorNames returns every first-party interceptor name, sorted.
func InterceptorNames() []string { return keysOf(GroupInterceptor) }
