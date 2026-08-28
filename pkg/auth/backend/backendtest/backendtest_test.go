// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// A conformance suite nobody has watched fail is a suite nobody knows
// bites. Every check here is fed a backend broken in EXACTLY the way that
// check exists to catch, and the test requires the check to complain.
package backendtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// fake is a backend whose every contract-relevant behaviour can be broken
// on purpose, one at a time.
type fake struct {
	name string

	// the deliberate defects
	acceptEmptyPassword bool
	distinctErrors      bool // different error for unknown user vs wrong password
	rejectionUsesWrong  error
	alwaysErr           error // fails even the valid credential (an unreachable source)
	returnNilUser       bool
	unstableName        bool
	nameCalls           int
}

func (f *fake) Name() string {
	f.nameCalls++
	if f.unstableName {
		return f.name + string(rune('a'+f.nameCalls))
	}
	return f.name
}

func (f *fake) Authenticate(_ context.Context, user, pass string) (*backend.User, error) {
	if f.alwaysErr != nil {
		return nil, f.alwaysErr
	}
	if pass == "" && f.acceptEmptyPassword {
		return &backend.User{ID: "1", Username: user}, nil
	}
	if user == "ana" && pass == "correcta" {
		if f.returnNilUser {
			return nil, nil
		}
		return &backend.User{ID: "1", Username: "ana"}, nil
	}
	if f.rejectionUsesWrong != nil {
		return nil, f.rejectionUsesWrong
	}
	if f.distinctErrors && user != "ana" {
		// Still a certain rejection — only the TEXT differs. Modelling it
		// any other way would trip the earlier "certain rejection" check
		// instead, and the kit would pass this case for the wrong reason.
		return nil, fmt.Errorf("%w: no such user", backend.ErrInvalidCredentials)
	}
	return nil, backend.ErrInvalidCredentials
}

func goodSuite(f *fake) Suite {
	return Suite{
		New:           func() (backend.Backend, error) { return f, nil },
		ValidUser:     "ana",
		ValidPassword: "correcta",
		UnknownUser:   "nadie",
	}
}

// A correct backend passes everything (with the unavailable check skipped
// because the suite does not supply the hook).
func TestKit_CorrectBackendPasses(t *testing.T) {
	s := goodSuite(&fake{name: "fake"})
	for _, c := range checks() {
		if err := c.run(s); err != nil && !errors.Is(err, errSkipped) {
			t.Errorf("%s: a correct backend must pass, got %v", c.name, err)
		}
	}
}

// The heart of it: each defect must be caught by its check, BY NAME.
// Asserting which check complains is what stops a kit from passing for
// the wrong reason.
func TestKit_CatchesEachDefect(t *testing.T) {
	for _, tc := range []struct {
		defect    string
		suite     Suite
		wantCheck string
		wantIn    string
	}{
		{
			defect:    "accepts an empty password",
			suite:     goodSuite(&fake{name: "fake", acceptEmptyPassword: true}),
			wantCheck: "empty password is rejected",
			wantIn:    "empty password authenticated",
		},
		{
			defect:    "tells unknown user apart from wrong password",
			suite:     goodSuite(&fake{name: "fake", distinctErrors: true}),
			wantCheck: "unknown user and wrong password are indistinguishable",
			wantIn:    "enumerate your users",
		},
		{
			defect:    "reports a rejection as unavailable",
			suite:     goodSuite(&fake{name: "fake", rejectionUsesWrong: backend.ErrBackendUnavailable}),
			wantCheck: "wrong password is a certain rejection",
			wantIn:    "CERTAIN rejection",
		},
		{
			defect:    "returns no error and no user",
			suite:     goodSuite(&fake{name: "fake", returnNilUser: true}),
			wantCheck: "valid credentials authenticate",
			wantIn:    "nil user as an acceptance",
		},
		{
			defect:    "has an unstable name",
			suite:     goodSuite(&fake{name: "fake", unstableName: true}),
			wantCheck: "name is stable and not empty",
			wantIn:    "must be stable",
		},
		{
			defect:    "has an empty name",
			suite:     goodSuite(&fake{name: ""}),
			wantCheck: "name is stable and not empty",
			wantIn:    "must not be empty",
		},
	} {
		t.Run(tc.defect, func(t *testing.T) {
			var got error
			var gotName string
			for _, c := range checks() {
				if err := c.run(tc.suite); err != nil && !errors.Is(err, errSkipped) {
					got, gotName = err, c.name
					break
				}
			}
			if got == nil {
				t.Fatalf("the kit did not catch a backend that %s", tc.defect)
			}
			if gotName != tc.wantCheck {
				t.Errorf("caught by %q, expected %q — a kit that fails for the wrong reason teaches the wrong lesson\n  %v", gotName, tc.wantCheck, got)
			}
			if !strings.Contains(got.Error(), tc.wantIn) {
				t.Errorf("the message must explain the defect; wanted it to mention %q, got:\n  %v", tc.wantIn, got)
			}
		})
	}
}

// The most consequential check, and the one an author can skip: an
// unreachable source reported as a rejection stops the chain and locks out
// the break-glass account.
func TestKit_CatchesOutageReportedAsRejection(t *testing.T) {
	s := goodSuite(&fake{name: "fake"})
	s.Unavailable = func() (backend.Backend, error) {
		return &fake{name: "fake", alwaysErr: backend.ErrInvalidCredentials}, nil
	}
	err := checkUnavailable(s)
	if err == nil {
		t.Fatal("an outage reported as a rejection must be caught")
	}
	if !strings.Contains(err.Error(), "REJECTION") || !strings.Contains(err.Error(), "cannot get in either") {
		t.Errorf("the message must name the defect AND what it costs, got: %v", err)
	}
}

// Skipping it is allowed, but it must SAY what is going unchecked rather
// than passing quietly.
func TestKit_SkippingUnavailableIsLoud(t *testing.T) {
	err := checkUnavailable(goodSuite(&fake{name: "fake"}))
	if !errors.Is(err, errSkipped) {
		t.Fatalf("a missing Unavailable hook must skip, got %v", err)
	}
	if !strings.Contains(err.Error(), "locks out the break-glass account") {
		t.Errorf("the skip must name what is not being checked, got: %v", err)
	}
}

// A suite that forgets a required field must fail loudly rather than
// checking a subset in silence.
func TestKit_IncompleteSuiteIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    Suite
	}{
		{"no New", Suite{ValidUser: "a", ValidPassword: "b", UnknownUser: "c"}},
		{"no valid credential", Suite{New: func() (backend.Backend, error) { return &fake{}, nil }, UnknownUser: "c"}},
		{"no unknown user", Suite{New: func() (backend.Backend, error) { return &fake{}, nil }, ValidUser: "a", ValidPassword: "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.validate(); err == nil {
				t.Error("an incomplete suite must be rejected")
			}
		})
	}
}
