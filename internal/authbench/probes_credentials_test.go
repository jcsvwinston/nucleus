// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// configKeys returns every koanf key the application configuration declares,
// lowercased. A control whose knob does not exist has nowhere to be
// configured — measured off the struct, not off a document.
func configKeys() map[string]bool {
	keys := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if tag := f.Tag.Get("koanf"); tag != "" && tag != "-" {
				keys[strings.ToLower(tag)] = true
			}
			ft := f.Type
			for ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && ft.PkgPath() != "time" {
				walk(ft)
			}
		}
	}
	walk(reflect.TypeOf(app.Config{}))
	return keys
}

// hasConfigKeyContaining reports whether any configuration key mentions the
// term.
func hasConfigKeyContaining(term string) (string, bool) {
	for k := range configKeys() {
		if strings.Contains(k, term) {
			return k, true
		}
	}
	return "", false
}

// CRED-01 — passwords are hashed with a modern KDF and verified.
func probePasswordHash(t *testing.T, _ *env) verdict {
	hash, err := auth.HashPassword("correcta-horse-battery")
	if err != nil {
		t.Logf("HashPassword: %v", err)
		return absent
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Logf("unexpected hash format: %q", hash)
		return partial
	}
	if !auth.CheckPassword("correcta-horse-battery", hash) {
		t.Log("the hash does not verify its own password")
		return absent
	}
	if auth.CheckPassword("otra", hash) {
		t.Log("a wrong password verified")
		return absent
	}
	return present
}

// CRED-02 — a long passphrase. bcrypt takes 72 bytes and no more: the
// question a probe has to settle is whether the framework REFUSES the rest
// or silently ignores it, because silently ignoring it makes two different
// passphrases the same credential.
func probeLongPassphrase(t *testing.T, _ *env) verdict {
	long := strings.Repeat("a", 80) + "-tail-one"
	other := strings.Repeat("a", 80) + "-tail-two"

	h1, err1 := auth.HashPassword(long)
	if err1 != nil {
		t.Logf("a >72-byte passphrase is refused: %v", err1)
		return present
	}
	if auth.CheckPassword(other, h1) {
		t.Log("two different >72-byte passphrases verify against the same hash")
		return absent
	}
	return present
}

// CRED-03 — progressive lockout after repeated failures.
func probeLockout(t *testing.T, _ *env) verdict {
	if k, ok := hasConfigKeyContaining("lockout"); ok {
		t.Logf("config key %q exists", k)
		return partial
	}
	return absent
}

// CRED-04 — password reset by single-use token.
func probePasswordReset(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/password/reset", "/password/reset", "/accounts/password/reset")
}

// CRED-05 — password change with re-authentication.
func probePasswordChange(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/password/change", "/accounts/password")
}

// CRED-06 — registration with email verification.
func probeEmailVerification(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/register", "/accounts/register", "/auth/verify-email")
}

// CRED-07 — magic link sign-in.
func probeMagicLink(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/magic-link", "/accounts/magic-link")
}

// CRED-08 — a login route the framework serves. Today an application writes
// its own handler over the backend chain; nothing is mounted for it.
func probeLoginRoute(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/login", "/auth/login", "/accounts/login")
}
