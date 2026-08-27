// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco A, §3: la costura de autenticación. Es la que desbloquea LDAP, SAML
// y OIDC como módulos externos.
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
func TestChain_OrderIsHonoured(t *testing.T) {
	var calls []string
	register(t, &stubBackend{name: "dir", accept: "alice", calls: &calls})
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
