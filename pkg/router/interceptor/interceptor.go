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

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// Interceptor wraps a handler. It is the standard Go middleware shape on
// purpose: an existing func(http.Handler) http.Handler needs no adapter,
// and an author already knows the contract.
type Interceptor func(http.Handler) http.Handler

// Config carries the `http_interceptors.<name>.*` subtree that belongs to
// one registered interceptor.
//
// It is backend.Config — the same type, not a parallel one — because an
// extension author has already met it if they have written any other
// provider for this framework, and Bind works the same way.
type Config = backend.Config

// Factory builds a configured interceptor.
//
// Returning an error fails BOOT rather than the first request. An
// interceptor that could not configure itself is a protection that is not
// there, and a protection that is not there must not be discovered by an
// audit six months later.
type Factory func(cfg Config) (Interceptor, error)
