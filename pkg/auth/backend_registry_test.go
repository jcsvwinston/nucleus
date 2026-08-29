// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco A, §3: la costura de autenticación. Es la que desbloquea LDAP como
// módulo externo — y SÓLO LDAP: SAML y OIDC no son un par usuario/contraseña
// sino un flujo de redirección con callback, y su costura es otra
// (pkg/auth/federated, ADR-028). La afirmación original decía «LDAP, SAML y
// OIDC» y era falsa para dos de los tres.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type stubBackend struct {
	name   string
	accept string // username it accepts
	fail   error  // returned when it does not accept
	calls  *[]string
}

func (s *stubBackend) Name() string { return s.name }
func (s *stubBackend) Authenticate(_ context.Context, username, _ string) (*User, error) {
	if s.calls != nil {
		*s.calls = append(*s.calls, s.name)
	}
	if username == s.accept {
		return &User{ID: username, Username: username}, nil
	}
	if s.fail != nil {
		return nil, s.fail
	}
	return nil, ErrInvalidCredentials
}

func register(t *testing.T, b *stubBackend) {
	t.Helper()
	if err := RegisterBackend(b.name, func(BackendConfig) (Backend, error) { return b, nil }); err != nil {
		t.Fatalf("register %s: %v", b.name, err)
	}
	t.Cleanup(func() { unregisterBackendForTest(b.name) })
}

// The deployment everyone actually wants: the directory first, a local
// account second.
//
// Note how the second backend gets its turn: because the first was
// UNAVAILABLE. This test used to have the directory REJECT and still expect
// local to accept, which is the fail-open TestChain_RejectionEndsTheAttempt
// now forbids — the assertion about ordering was true, but the scenario it
// used to prove it was the defect itself.
func TestChain_OrderIsHonoured(t *testing.T) {
	var calls []string
	register(t, &stubBackend{name: "dir", accept: "alice", fail: ErrBackendUnavailable, calls: &calls})
	register(t, &stubBackend{name: "local", accept: "root", calls: &calls})

	chain, err := NewChain("dir", "local")
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	if got := strings.Join(chain.Names(), ","); got != "dir,local" {
		t.Fatalf("the chain must preserve declaration order, got %q", got)
	}

	user, err := chain.Authenticate(context.Background(), "root", "pw")
	if err != nil || user == nil || user.Username != "root" {
		t.Fatalf("the second backend must get its turn: user=%v err=%v", user, err)
	}
	if got := strings.Join(calls, ","); got != "dir,local" {
		t.Errorf("backends must be consulted in order, got %q", got)
	}

	// And the first backend still short-circuits when it ACCEPTS.
	calls = nil
	if u, err := chain.Authenticate(context.Background(), "alice", "pw"); err != nil || u == nil {
		t.Fatalf("dir must accept alice: user=%v err=%v", u, err)
	}
	if got := strings.Join(calls, ","); got != "dir" {
		t.Errorf("an acceptance must not consult the rest of the chain, consulted %q", got)
	}
}

// The break-glass case, and the whole reason the chain is ordered: the
// directory is down and somebody still has to get in.
func TestChain_LocalAccountSurvivesAnUnreachableDirectory(t *testing.T) {
	register(t, &stubBackend{name: "dir", accept: "\x00none", fail: ErrBackendUnavailable})
	register(t, &stubBackend{name: "local", accept: "root"})

	chain, err := NewChain("dir", "local")
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	user, err := chain.Authenticate(context.Background(), "root", "pw")
	if err != nil || user == nil {
		t.Fatalf("a break-glass account must work while the directory is unreachable: %v", err)
	}
}

// "Wrong password" and "the directory is down" are different operational
// situations; reporting the first when the truth is the second sends the
// operator hunting in the wrong place.
func TestChain_UnavailableIsNotRejection(t *testing.T) {
	register(t, &stubBackend{name: "dir", accept: "\x00none", fail: ErrBackendUnavailable})
	register(t, &stubBackend{name: "local", accept: "\x00none"})

	chain, _ := NewChain("dir", "local")
	_, err := chain.Authenticate(context.Background(), "nobody", "pw")
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("want ErrBackendUnavailable when a backend could not be reached, got %v", err)
	}
	if !strings.Contains(err.Error(), "dir") {
		t.Errorf("the error must name the unreachable backend, got %v", err)
	}

	// Every backend rejecting IS a rejection.
	chain2, _ := NewChain("local")
	if _, err := chain2.Authenticate(context.Background(), "nobody", "pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("want ErrInvalidCredentials when every backend rejected, got %v", err)
	}
}

// An unexpected error must not be able to lock everyone out.
func TestChain_UnexpectedErrorCountsAsUnavailable(t *testing.T) {
	register(t, &stubBackend{name: "flaky", accept: "\x00none", fail: fmt.Errorf("boom")})
	register(t, &stubBackend{name: "local", accept: "root"})

	chain, _ := NewChain("flaky", "local")
	if _, err := chain.Authenticate(context.Background(), "root", "pw"); err != nil {
		t.Fatalf("a backend failing unexpectedly must not block the rest: %v", err)
	}
}

func TestNewChain_Rejects(t *testing.T) {
	register(t, &stubBackend{name: "solo", accept: "x"})

	if _, err := NewChain(); err == nil {
		t.Error("an empty chain must be rejected")
	}
	if _, err := NewChain("nosuch"); err == nil {
		t.Error("an unknown backend must be rejected")
	} else if !strings.Contains(err.Error(), "solo") {
		t.Errorf("the error must name the registered backends, got %v", err)
	}
	if _, err := NewChain("solo", "solo"); err == nil {
		t.Error("a repeated backend must be rejected: order is meaningful, so a repeat is a mistake")
	}
}

// TestChain_RejectionEndsTheAttempt is the security regression guard for a
// fail-open in the chain.
//
// The scenario: an employee's directory account is revoked, but her local
// row is still alive with the old password. The directory REJECTED her —
// a certain answer, not a failure to reach it — and the chain continued to
// the local backend anyway, which let her in.
//
// Rejection and unavailability were both `continue`, so the loop could only
// end by acceptance or by running out of backends. The two situations have
// opposite first causes and must not have the same effect: an unreachable
// backend proves nothing, so the chain keeps going (break-glass); a
// rejection is the identity source's verdict, so it ends the attempt.
//
// This is what pkg/auth/backend documents ("the backend is sure the answer
// is no"), what README.md and the configuration reference publish, what the
// backendtest conformance kit argues its anti-enumeration check on, and
// what orbit promises when it says a local admin row is not a bypass.
func TestChain_RejectionEndsTheAttempt(t *testing.T) {
	var calls []string
	// dir knows ana and says no. local would say yes.
	register(t, &stubBackend{name: "dir", accept: "\x00none", calls: &calls})
	register(t, &stubBackend{name: "local", accept: "ana", calls: &calls})

	chain, err := NewChain("dir", "local")
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	user, err := chain.Authenticate(context.Background(), "ana", "vieja")
	if user != nil {
		t.Errorf("FAIL-OPEN: ana entró pese al rechazo cierto del directorio: %+v", user)
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("a rejection must surface as ErrInvalidCredentials, got %v", err)
	}
	if got := strings.Join(calls, ","); got != "dir" {
		t.Errorf("the chain must not consult a backend behind a rejection, consulted %q", got)
	}
}

// TestChain_UnavailableStillFallsThrough is the control that keeps the fix
// honest: without it, "rejection ends the attempt" could be implemented by
// ending the attempt on ANY error, which would kill break-glass access.
func TestChain_UnavailableStillFallsThrough(t *testing.T) {
	register(t, &stubBackend{name: "dircaido", accept: "\x00none", fail: ErrBackendUnavailable})
	register(t, &stubBackend{name: "local2", accept: "ana"})

	chain, err := NewChain("dircaido", "local2")
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	user, err := chain.Authenticate(context.Background(), "ana", "pw")
	if err != nil || user == nil {
		t.Fatalf("an UNREACHABLE backend must not end the attempt: user=%v err=%v", user, err)
	}
}
