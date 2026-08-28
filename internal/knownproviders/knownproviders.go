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

import "strings"

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
