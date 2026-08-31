// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// A managed secret store registers the scheme it owns, the same way a
// storage backend registers its name. Before this, the chain named the AWS
// resolver in its own struct, which meant the framework linked the AWS SDK
// to offer a scheme most deployments never write — the whole credential
// chain, for a feature behind an "aws-sm:" prefix nobody had typed.
//
// The scheme is the contract: a reference "aws-sm:prod/jwt" routes to
// whoever registered "aws-sm:", and the resolver is built lazily, only if
// some key actually uses it.
type ResolverFactory func(ctx context.Context) (Resolver, error)

var (
	resolversMu sync.RWMutex
	resolvers   = map[string]ResolverFactory{}
)

// RegisterResolver makes scheme resolvable by the returned resolver.
// The scheme must end in ":" — it is matched as a literal prefix of the
// reference, so "aws-sm:" owns "aws-sm:prod/jwt".
func RegisterResolver(scheme string, factory ResolverFactory) error {
	scheme = strings.TrimSpace(scheme)
	switch {
	case scheme == "":
		return fmt.Errorf("secrets: empty scheme")
	case !strings.HasSuffix(scheme, ":"):
		return fmt.Errorf("secrets: scheme %q must end in \":\"", scheme)
	case scheme == schemeEnv:
		return fmt.Errorf("secrets: %q is the built-in environment scheme", scheme)
	case factory == nil:
		return fmt.Errorf("secrets: nil factory for scheme %q", scheme)
	}
	resolversMu.Lock()
	defer resolversMu.Unlock()
	if _, dup := resolvers[scheme]; dup {
		// Replacing silently would make the effective resolver depend on
		// init order, which is not something a deployment can reason about.
		return fmt.Errorf("secrets: scheme %q is already registered", scheme)
	}
	resolvers[scheme] = factory
	return nil
}

// RegisteredSchemes returns every managed scheme currently registered.
func RegisteredSchemes() []string {
	resolversMu.RLock()
	defer resolversMu.RUnlock()
	out := make([]string, 0, len(resolvers))
	for s := range resolvers {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// lookupScheme returns the factory that owns ref's scheme, if any.
func lookupScheme(ref string) (string, ResolverFactory, bool) {
	trimmed := strings.TrimSpace(ref)
	resolversMu.RLock()
	defer resolversMu.RUnlock()
	for scheme, f := range resolvers {
		if strings.HasPrefix(trimmed, scheme) {
			return scheme, f, true
		}
	}
	return "", nil, false
}
