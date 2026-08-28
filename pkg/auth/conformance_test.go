// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend/backendtest"
)

// tableProvider is a minimal UserProvider: an application's own user
// table, reduced to the part the contract cares about.
type tableProvider struct{ user, pass string }

func (p *tableProvider) FindByID(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p *tableProvider) FindByUsername(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p *tableProvider) FindByEmail(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p *tableProvider) ValidateCredentials(_ context.Context, user, pass string) (*auth.User, error) {
	if user == p.user && pass == p.pass {
		return &auth.User{ID: "1", Username: user}, nil
	}
	return nil, auth.ErrUserNotFound
}

// The framework's own backend runs the conformance suite it ships. A kit
// whose author does not point it at their own implementation is a kit that
// has never had to be right.
//
// Suite.Unavailable is deliberately absent here, and the skip says so: this
// backend wraps the application's OWN database, so a failure there is a bug
// in the application rather than the "the directory is down" case the
// unavailable answer exists for — reporting it as unavailable would let the
// chain fall through to a backend the operator did not intend. That
// reasoning is in the backend's godoc; the skip is not an oversight.
func TestUserProviderBackend_Conformance(t *testing.T) {
	backendtest.Run(t, backendtest.Suite{
		New: func() (backend.Backend, error) {
			return auth.NewUserProviderBackend("local", &tableProvider{user: "ana", pass: "correcta"})
		},
		ValidUser:     "ana",
		ValidPassword: "correcta",
		UnknownUser:   "nadie",
	})
}

// The unsafe condition, fabricated: a UserProvider that returns a user
// whatever password it is given — a legacy row with an empty stored hash,
// or simply a bug. The framework sits in that path, so it stops it here
// instead of trusting every application's provider to be careful.
//
// Found by writing the conformance suite: the framework's own adapter
// passed the empty-password check only because the test's provider
// happened to reject it.
type acceptAnythingProvider struct{}

func (acceptAnythingProvider) FindByID(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (acceptAnythingProvider) FindByUsername(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (acceptAnythingProvider) FindByEmail(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (acceptAnythingProvider) ValidateCredentials(_ context.Context, user, _ string) (*auth.User, error) {
	return &auth.User{ID: "1", Username: user}, nil
}

func TestUserProviderBackend_RejectsEmptyPasswordEvenIfTheProviderWouldNot(t *testing.T) {
	b, err := auth.NewUserProviderBackend("local", acceptAnythingProvider{})
	if err != nil {
		t.Fatalf("NewUserProviderBackend: %v", err)
	}

	user, err := b.Authenticate(context.Background(), "ana", "")
	if err == nil {
		t.Fatalf("an empty password authenticated as %v — with a provider that accepts anything, this is a full authentication bypass and the framework is in the path", user)
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("must be a certain rejection, got %v", err)
	}

	// And the guard must not swallow the normal path.
	if _, err := b.Authenticate(context.Background(), "ana", "cualquiera"); err != nil {
		t.Errorf("a non-empty password must still reach the provider: %v", err)
	}
}
