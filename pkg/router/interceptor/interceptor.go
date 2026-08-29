// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package interceptor is the contract a third-party request interceptor
// implements — and nothing else.
//
// Every other extension point in this framework is reached the way
// database/sql drivers are: a package registers itself in an init
// function, an application imports it for the side effect, and
// configuration names it. Storage providers, session stores,
// authentication backends and federated identity providers all work that
// way. The request lifecycle did not.
//
// Middleware could be attached — but only by whoever CONSTRUCTS the
// application, or by a module that is mounted. A package that an
// application merely imports had no way in, which meant an interceptor
// could not be distributed as a plugin: it had to be code the application
// author pasted into their own bootstrap in the right order. That is the
// gap ADR-023 recorded as "observing is possible today, intercepting is
// not".
//
// # Ordering is the behaviour, so an operator declares it
//
// Middleware order is not a detail: authentication before rate limiting
// and rate limiting before authentication are different systems. So an
// interceptor is not merely enabled, it is placed — `http_interceptors`
// is an ORDERED list, the way `auth_backends` is, and the order in the
// file is the order requests pass through.
package interceptor

import (
	"net/http"

	"github.com/jcsvwinston/nucleus/internal/providerconfig"
)

// Interceptor wraps a handler. It is the standard Go middleware shape on
// purpose: an existing func(http.Handler) http.Handler needs no adapter,
// and an author already knows the contract.
type Interceptor func(http.Handler) http.Handler

// Config carries the `interceptors.<name>.*` subtree that belongs to one
// registered interceptor.
//
// It is defined here rather than aliased from the authentication
// contract, even though the shape is identical. An interceptor has
// nothing to do with authentication, and borrowing that package's type
// would have made every third-party interceptor compile the auth contract
// to read a config map — a dependency inherited for a resemblance rather
// than for a reason. Bind behaves exactly as it does for a backend.
type Config struct {
	// Name is the registered name the operator selected this interceptor
	// by.
	Name string

	// ProviderConfig is the raw `interceptors.<name>.*` subtree. Read it
	// with Bind rather than reaching into the map.
	ProviderConfig map[string]any
}

// Bind decodes the interceptor's own configuration subtree into dst,
// applying `default:` tags to fields the file left unset.
//
// A key the destination struct does not declare is an ERROR, not a
// silently ignored line: an interceptor is usually a protection, and a
// misspelled setting on a protection is the kind of thing an audit finds
// months later.
func (c Config) Bind(dst any) error {
	name := c.Name
	if name == "" {
		name = "interceptor"
	}
	return providerconfig.Bind(name, c.ProviderConfig, dst)
}

// Factory builds a configured interceptor.
//
// Returning an error fails BOOT rather than the first request. An
// interceptor that could not configure itself is a protection that is not
// there, and a protection that is not there must not be discovered by an
// audit six months later.
type Factory func(cfg Config) (Interceptor, error)
