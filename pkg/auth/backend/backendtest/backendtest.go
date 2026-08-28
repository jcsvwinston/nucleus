// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package backendtest is a conformance suite for authentication backends.
//
// The contract in pkg/auth/backend is three methods and two sentinel
// errors, and every one of its subtle parts is stated in prose: that a
// rejection and an unreachable source are DIFFERENT answers, that an
// unknown user and a wrong password must be indistinguishable, that an
// empty password is not a credential. Prose is where those properties go
// to be misread — the framework's own first backend needed each of them
// pinned by a test that fails when the guard is removed.
//
// This package is that set of tests, packaged so a third party gets them
// by writing four lines instead of by reading carefully:
//
//	func TestConformance(t *testing.T) {
//	    backendtest.Run(t, backendtest.Suite{
//	        New:           func() (backend.Backend, error) { return New(cfg) },
//	        ValidUser:     "ana",
//	        ValidPassword: "correcta",
//	        UnknownUser:   "nadie",
//	    })
//	}
//
// It asserts CONTRACT properties, not quality: passing means a backend
// answers the way the chain expects, so an operator's break-glass account
// still works when the directory does not. It cannot tell you the backend
// talks to the right directory.
package backendtest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// Suite describes a backend and the credentials to exercise it with.
type Suite struct {
	// New builds a fresh backend. Called once per check, so a backend
	// holding a connection is built and discarded repeatedly — return a
	// cheap constructor, not a shared instance, or the checks will observe
	// each other's state.
	New func() (backend.Backend, error)

	// ValidUser and ValidPassword authenticate successfully. Required:
	// without a credential that works, "rejects everything" would pass
	// every other check.
	ValidUser, ValidPassword string

	// UnknownUser does not exist in the source.
	UnknownUser string

	// Unavailable builds the SAME backend pointed at a source it cannot
	// reach — a dead port, a wrong host. Optional, and the only check that
	// needs cooperation from the author, because only they know how to
	// break their own connection.
	//
	// Skipping it skips the single most consequential property in the
	// contract: a backend that reports an outage as "wrong password" stops
	// the chain and locks out the local account kept for exactly that
	// morning.
	Unavailable func() (backend.Backend, error)
}

// check is one conformance property. Returning an error rather than
// failing a *testing.T is what lets the kit be tested against backends
// that are broken on purpose — a conformance suite nobody has watched
// fail is a suite nobody knows bites.
type check struct {
	name string
	// run returns nil when the property holds, a reason when it does not,
	// and errSkipped when the suite did not supply what the check needs.
	run func(Suite) error
}

var errSkipped = errors.New("skipped")

// Run executes every conformance check against the backend the suite
// describes.
func Run(t *testing.T, s Suite) {
	t.Helper()

	if err := s.validate(); err != nil {
		t.Fatalf("backendtest: %v", err)
	}

	for _, c := range checks() {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(s)
			switch {
			case err == nil:
			case errors.Is(err, errSkipped):
				t.Skip(err.Error())
			default:
				t.Error(err.Error())
			}
		})
	}
}

func (s Suite) validate() error {
	switch {
	case s.New == nil:
		return errors.New("Suite.New is required")
	case s.ValidUser == "" || s.ValidPassword == "":
		return errors.New("Suite.ValidUser and ValidPassword are required — without a credential that works, a backend that rejects everything would pass every other check")
	case s.UnknownUser == "":
		return errors.New("Suite.UnknownUser is required")
	}
	return nil
}

func checks() []check {
	return []check{
		{"name is stable and not empty", checkName},
		{"valid credentials authenticate", checkValidCredentials},
		{"wrong password is a certain rejection", checkWrongPassword},
		{"unknown user is a certain rejection", checkUnknownUser},
		{"unknown user and wrong password are indistinguishable", checkIndistinguishable},
		{"empty password is rejected", checkEmptyPassword},
		{"unreachable source is unavailable, not a rejection", checkUnavailable},
	}
}

func checkName(s Suite) error {
	b, err := s.build()
	if err != nil {
		return err
	}
	if b.Name() == "" {
		return errors.New("Name() must not be empty: it is how the backend appears in auth_backends, in logs and in the chain's errors")
	}
	if first, second := b.Name(), b.Name(); first != second {
		return fmt.Errorf("Name() must be stable, got %q then %q", first, second)
	}
	return nil
}

func checkValidCredentials(s Suite) error {
	b, err := s.build()
	if err != nil {
		return err
	}
	user, err := b.Authenticate(context.Background(), s.ValidUser, s.ValidPassword)
	if err != nil {
		return fmt.Errorf("the credential the suite declares valid must authenticate: %v", err)
	}
	if user == nil {
		return errors.New("Authenticate returned no error and no user — the chain would treat a nil user as an acceptance and hand it downstream")
	}
	if user.Username == "" && user.ID == "" {
		return errors.New("the returned user carries neither ID nor Username: nothing downstream can identify who logged in")
	}
	return nil
}

func checkWrongPassword(s Suite) error {
	b, err := s.build()
	if err != nil {
		return err
	}
	_, authErr := b.Authenticate(context.Background(), s.ValidUser, s.ValidPassword+"-wrong")
	return rejection(authErr, "a wrong password")
}

func checkUnknownUser(s Suite) error {
	b, err := s.build()
	if err != nil {
		return err
	}
	_, authErr := b.Authenticate(context.Background(), s.UnknownUser, s.ValidPassword)
	return rejection(authErr, "an unknown user")
}

// checkIndistinguishable is the property a user enumerator is built out
// of. The two failures must be the SAME error: a caller that can tell "no
// such user" from "wrong password" can walk a list of names and learn
// which ones exist — and because the chain stops on rejection, it
// publishes that for every backend behind this one too.
func checkIndistinguishable(s Suite) error {
	b, err := s.build()
	if err != nil {
		return err
	}
	_, unknownErr := b.Authenticate(context.Background(), s.UnknownUser, s.ValidPassword)
	_, wrongErr := b.Authenticate(context.Background(), s.ValidUser, s.ValidPassword+"-wrong")
	if unknownErr == nil || wrongErr == nil {
		return errors.New("both an unknown user and a wrong password must fail")
	}
	if unknownErr.Error() != wrongErr.Error() {
		return fmt.Errorf("the two rejections must be indistinguishable to the caller, and these differ:\n  unknown user:   %v\n  wrong password: %v\n\nA caller that can tell them apart can enumerate your users.", unknownErr, wrongErr)
	}
	return nil
}

// checkEmptyPassword is called out separately because several protocols
// answer an empty credential with success — an LDAP simple bind with a DN
// and no password is UNAUTHENTICATED under RFC 4513, and a directory may
// accept it.
func checkEmptyPassword(s Suite) error {
	b, err := s.build()
	if err != nil {
		return err
	}
	user, authErr := b.Authenticate(context.Background(), s.ValidUser, "")
	if authErr == nil {
		return fmt.Errorf("an empty password authenticated as %v — several protocols answer an empty credential with success, so a backend has to refuse it itself", user)
	}
	return rejection(authErr, "an empty password")
}

func checkUnavailable(s Suite) error {
	if s.Unavailable == nil {
		return fmt.Errorf("%w: Suite.Unavailable not provided — the single most consequential property in the contract is NOT being checked: a backend that reports an outage as a rejection stops the chain and locks out the break-glass account", errSkipped)
	}
	b, err := s.Unavailable()
	if err != nil {
		return fmt.Errorf("Suite.Unavailable: %v", err)
	}
	if b == nil {
		return errors.New("Suite.Unavailable returned a nil backend")
	}
	_, authErr := b.Authenticate(context.Background(), s.ValidUser, s.ValidPassword)
	if authErr == nil {
		return errors.New("a backend that cannot reach its source must not authenticate")
	}
	if errors.Is(authErr, backend.ErrInvalidCredentials) {
		return fmt.Errorf("an unreachable source was reported as a REJECTION (%v).\n\nThat is the failure this contract exists to prevent: the chain stops instead of falling through, so the local account an operator keeps for exactly this morning cannot get in either.", authErr)
	}
	if !errors.Is(authErr, backend.ErrBackendUnavailable) {
		return fmt.Errorf("an unreachable source should report ErrBackendUnavailable so the caller can say WHY nobody can log in, got %v", authErr)
	}
	return nil
}

func rejection(err error, what string) error {
	if err == nil {
		return fmt.Errorf("%s must not authenticate", what)
	}
	if !errors.Is(err, backend.ErrInvalidCredentials) {
		return fmt.Errorf("%s must be a CERTAIN rejection (ErrInvalidCredentials), got %v — anything else is read by the chain as 'could not reach the source', so it moves on to the next backend instead of stopping", what, err)
	}
	if errors.Is(err, backend.ErrBackendUnavailable) {
		return fmt.Errorf("%s reported BOTH rejection and unavailable: the chain cannot tell which happened", what)
	}
	return nil
}

func (s Suite) build() (backend.Backend, error) {
	b, err := s.New()
	if err != nil {
		return nil, fmt.Errorf("Suite.New: %v", err)
	}
	if b == nil {
		return nil, errors.New("Suite.New returned a nil backend")
	}
	return b, nil
}
