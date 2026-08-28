// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
)

// ErrUserNotFound is what a UserProvider returns when the username does
// not exist. A provider may return it, or any other error; the adapter
// below maps every lookup failure to the same outcome on purpose.
var ErrUserNotFound = errors.New("auth: user not found")

// userProviderBackend adapts an application's UserProvider — the interface
// that has described how to reach your user table since v0.x — into a
// Backend the authentication chain can consult.
//
// The interface was declared, frozen in the contract baseline, and used by
// nothing: the framework never called it. This is what finally consumes it.
type userProviderBackend struct {
	name     string
	provider UserProvider
}

// NewUserProviderBackend wraps a UserProvider as an authentication
// backend, so an application's own user table takes its place in the chain
// alongside a directory or any other source.
//
// It is usually the LAST entry: `[ldap, local]` means the directory
// answers first and the local table is what still works the morning the
// directory does not.
//
// TIMING. This adapter cannot make an implementation constant-time; it
// only refrains from making things worse. Every failure — user absent,
// password wrong, lookup error — leaves through one path with one error,
// so the adapter adds no distinguishable branch. Equalising the WORK is
// the provider's job: if ValidateCredentials returns early for an unknown
// user without hashing anything, it answers faster than for a real user
// with a wrong password, and that difference is a user enumerator no
// wrapper can hide. Hash against a dummy value before returning, the way
// the admin login in this suite already does.
func NewUserProviderBackend(name string, provider UserProvider) (Backend, error) {
	if provider == nil {
		return nil, fmt.Errorf("auth: backend %q: user provider cannot be nil", name)
	}
	if name == "" {
		name = "local"
	}
	return &userProviderBackend{name: name, provider: provider}, nil
}

func (b *userProviderBackend) Name() string { return b.name }

func (b *userProviderBackend) Authenticate(ctx context.Context, username, password string) (*User, error) {
	// An empty password is not a credential, and this adapter refuses it
	// rather than trusting every UserProvider to do so.
	//
	// The conformance suite states the rule; writing it found that this
	// adapter did not enforce it. A provider whose ValidateCredentials
	// returns the user without comparing —because the stored hash is empty
	// for a legacy row, because of a bug— becomes a full authentication
	// bypass, and the framework sits in that path and let it through. It
	// costs nothing to stop here, and the guard holds whatever quality the
	// application's provider has.
	if password == "" {
		return nil, ErrInvalidCredentials
	}

	user, err := b.provider.ValidateCredentials(ctx, username, password)
	if err != nil {
		// Every rejection reason collapses into one answer. "No such
		// user" and "wrong password" must be indistinguishable to the
		// caller, and a lookup error is not reported as unavailable
		// either: a UserProvider talks to the application's own database,
		// so a failure there is not the "directory is down" case the
		// chain's unavailable state exists for — it is a bug in the
		// application, and treating it as unavailable would let the chain
		// fall through to a backend the operator did not intend.
		return nil, ErrInvalidCredentials
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}
