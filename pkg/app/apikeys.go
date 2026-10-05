// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// WithAPIKeys authenticates the requests that present an API key — the
// X-API-Key header, or `Authorization: Bearer nk_…` — against the keys kept
// in the application's default database, the same table `nucleus apikey
// create` issues them into (nucleus_api_keys, created on first use).
//
// It is what `nucleus add apikeys` writes into main.go. Before it, the
// pieces existed (a SQL store, apikeys.Issue, apikeys.Middleware) and the
// application assembled them by hand: a store needs the database handle,
// which exists only once the application is built, and the middleware has
// to sit at a particular point of a stack the application does not own.
//
// The middleware goes right after the bearer decode and BEFORE the rate
// limiter and the request interceptors, so a key's owner is the identity the
// limiter keys on and an interceptor sees. A request without a key passes
// through untouched; a key that does not authenticate is refused with 401.
// A route that must have a key says so with apikeys.Require. The
// default-deny RBAC layer resolves its subject from bearer claims and does
// not read a key's owner: on the default stack a route a key-holding program
// calls is authorised for its anonymous subject and gated by apikeys.Require.
//
// The store speaks SQLite, PostgreSQL and MySQL; another default engine
// fails boot with its name.
func WithAPIKeys() Option {
	return func(o *appOptions) { o.apiKeys = true }
}

// openAPIKeyStore opens the key table on the default database.
func openAPIKeyStore(ctx context.Context, conn *db.DB) (apikeys.Store, error) {
	if conn == nil {
		return nil, fmt.Errorf("API keys live in the default database, and none is configured")
	}
	flavor, err := apiKeyFlavor(conn.System())
	if err != nil {
		return nil, err
	}
	sqlDB, err := conn.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("open the default database: %w", err)
	}
	return apikeys.NewSQLStore(ctx, sqlDB, apikeys.SQLStoreConfig{Flavor: flavor})
}

// apiKeyFlavor maps the database system pkg/db resolved from the URL onto the
// dialect the key store speaks.
func apiKeyFlavor(system string) (apikeys.Flavor, error) {
	switch strings.ToLower(strings.TrimSpace(system)) {
	case "sqlite":
		return apikeys.FlavorSQLite, nil
	case "postgresql", "postgres":
		return apikeys.FlavorPostgres, nil
	case "mysql":
		return apikeys.FlavorMySQL, nil
	}
	return "", fmt.Errorf("the API key store speaks sqlite, postgres and mysql; the default database is %s", system)
}

// mountAPIKeys installs the key middleware once. The default stack calls it
// right after the bearer decode; New calls it again afterwards so an
// application built WithoutDefaults gets it too.
func (a *App) mountAPIKeys() {
	if a == nil || a.apiKeys == nil || a.apiKeysMounted || a.Router == nil {
		return
	}
	a.apiKeysMounted = true
	a.Router.Use(apikeys.Middleware(a.apiKeys))
	if a.Logger != nil {
		a.Logger.Info("nucleus: API keys authenticate requests (X-API-Key or Authorization: Bearer nk_…; issue one with `nucleus apikey create`)")
	}
}
