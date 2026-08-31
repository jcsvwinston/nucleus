// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package knownproviders names the backends this project ships as separate
// modules.
//
// A registry can only report that a name is not registered. That is true
// and nearly useless when the name is one WE publish: the operator wrote
// `ldap` because the documentation told them to, and the answer they need
// is not "unknown" but "you have not imported it yet, here is the line".
//
// The table changes nothing about registration. A name here is registered
// exactly when its module is imported, like any other — this only makes
// the error say what to do about it. Keeping it as data means the core
// carries the NAME of a satellite module and none of its code.
package knownproviders

import (
	"sort"
	"strings"
)

// Provider describes a backend published as its own module.
type Provider struct {
	// Kind is how the subsystem is named in an error sentence, e.g.
	// "authentication backend".
	Kind string
	// Name is the registered name it takes once imported.
	Name string
	// Module is the `go get` target.
	Module string
	// RequiresConfig reports whether the backend is unusable without a
	// configuration subtree of its own — a directory client cannot invent
	// the address of the directory.
	RequiresConfig bool
	// Remote reports whether the backend depends on a system outside the
	// application, which is what makes a chain without a local fallback a
	// single point of failure.
	Remote bool
}

// authBackends is the whole table today. It is deliberately small: a name
// belongs here only if this project publishes it, because the promise the
// error makes — `go get` this and it works — is one we have to keep.
var authBackends = map[string]Provider{
	"ldap": {
		Kind:           "authentication backend",
		Name:           "ldap",
		Module:         "github.com/jcsvwinston/nucleus/providers/ldap",
		RequiresConfig: true,
		Remote:         true,
	},
}

// dbDrivers are the database drivers this project publishes as their own
// modules. They are keyed by the database/sql driver NAME that pkg/db
// resolves a URL scheme to — "pgx" for postgres://, "sqlserver" for both
// sqlserver:// and mssql:// — because that is the name sql.Open fails on,
// and the failure is where the guidance has to appear.
//
// Two of them, sqlserver and oracle, used to sit behind the build tags
// `mssql` and `oracle`. A build tag is invisible: nothing in the source says
// it exists, so a build that forgot it failed at RUN time with "unknown
// driver". As modules they fail while compiling, and the error below names
// the fix.
var dbDrivers = map[string]Provider{
	"pgx": {
		Kind:   "database driver",
		Name:   "postgres",
		Module: "github.com/jcsvwinston/nucleus/drivers/postgres",
	},
	"mysql": {
		Kind:   "database driver",
		Name:   "mysql",
		Module: "github.com/jcsvwinston/nucleus/drivers/mysql",
	},
	"sqlite": {
		Kind:   "database driver",
		Name:   "sqlite",
		Module: "github.com/jcsvwinston/nucleus/drivers/sqlite",
	},
	"sqlserver": {
		Kind:   "database driver",
		Name:   "sqlserver",
		Module: "github.com/jcsvwinston/nucleus/drivers/mssql",
	},
	"oracle": {
		Kind:   "database driver",
		Name:   "oracle",
		Module: "github.com/jcsvwinston/nucleus/drivers/oracle",
	},
}

// DBDriver looks a database/sql driver name up in the table above.
func DBDriver(name string) (Provider, bool) {
	p, ok := dbDrivers[name]
	return p, ok
}

// DBDriverNames returns the driver names this project publishes, sorted so
// that an error message lists them the same way every time.
func DBDriverNames() []string {
	names := make([]string, 0, len(dbDrivers))
	for name := range dbDrivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// telemetryExporters are the OpenTelemetry exporters this project publishes
// as their own modules. The framework keeps the SDK — it is small, and
// instrumentation all over the code calls into it — and only the exporters
// left, because they are where the weight is: OTLP pulls gRPC even in its
// HTTP flavour, and Prometheus pulls protobuf through client_golang.
var telemetryExporters = map[string]Provider{
	"otlp": {
		Kind:   "telemetry exporter",
		Name:   "otlp",
		Module: "github.com/jcsvwinston/nucleus/exporters/otlp",
	},
	"prometheus": {
		Kind:   "telemetry exporter",
		Name:   "prometheus",
		Module: "github.com/jcsvwinston/nucleus/exporters/prometheus",
	},
}

// TelemetryExporter looks an exporter name up in the table above.
func TelemetryExporter(name string) (Provider, bool) {
	p, ok := telemetryExporters[name]
	return p, ok
}

// TelemetryExporterNames returns the exporter names this project publishes,
// sorted.
func TelemetryExporterNames() []string {
	names := make([]string, 0, len(telemetryExporters))
	for name := range telemetryExporters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// storageProviders are the object-storage backends this project publishes as
// their own modules. They left the core in the graph-slimming arc: the four
// cloud SDKs weighed 42.6 MB of a 75.6 MB hello-world, which every
// application paid whether or not it ever opened a bucket.
var storageProviders = map[string]Provider{
	"s3": {
		Kind:           "storage provider",
		Name:           "s3",
		Module:         "github.com/jcsvwinston/nucleus/providers/storage-s3",
		RequiresConfig: true,
		Remote:         true,
	},
	"gcs": {
		Kind:           "storage provider",
		Name:           "gcs",
		Module:         "github.com/jcsvwinston/nucleus/providers/storage-gcs",
		RequiresConfig: true,
		Remote:         true,
	},
	"azure": {
		Kind:           "storage provider",
		Name:           "azure",
		Module:         "github.com/jcsvwinston/nucleus/providers/storage-azure",
		RequiresConfig: true,
		Remote:         true,
	},
}

// secretsResolvers are the managed secret stores this project publishes as
// their own modules, keyed by the reference scheme they own.
var secretsResolvers = map[string]Provider{
	"aws-sm:": {
		Kind:           "secrets resolver",
		Name:           "aws-sm:",
		Module:         "github.com/jcsvwinston/nucleus/providers/secrets-aws",
		RequiresConfig: false,
		Remote:         true,
	},
}

// SecretsResolver returns the description of a first-party secrets resolver
// published as a separate module, keyed by its reference scheme.
func SecretsResolver(scheme string) (Provider, bool) {
	p, ok := secretsResolvers[strings.ToLower(strings.TrimSpace(scheme))]
	return p, ok
}

// StorageProvider returns the description of a first-party storage backend
// published as a separate module.
func StorageProvider(name string) (Provider, bool) {
	p, ok := storageProviders[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// StorageProviderNames returns every first-party storage provider name.
func StorageProviderNames() []string {
	names := make([]string, 0, len(storageProviders))
	for name := range storageProviders {
		names = append(names, name)
	}
	return names
}

// AuthBackend returns the description of a first-party authentication
// backend published as a separate module.
func AuthBackend(name string) (Provider, bool) {
	p, ok := authBackends[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// AuthBackendNames returns every first-party authentication backend name.
func AuthBackendNames() []string {
	names := make([]string, 0, len(authBackends))
	for name := range authBackends {
		names = append(names, name)
	}
	return names
}

// InstallHint is the two-line recipe that turns "unknown backend" into
// something an operator can act on without leaving the terminal.
func (p Provider) InstallHint() string {
	return "\t\tgo get " + p.Module + "\n\n" +
		"\tand import it for its side effect, the way database/sql drivers are wired:\n\n" +
		"\t\timport _ \"" + p.Module + "\""
}
