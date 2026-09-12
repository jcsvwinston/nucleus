// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// putJSON is postJSON's sibling for the one route that takes a PUT.
func putJSON(t *testing.T, srv *nucleustest.Server, path string, body any) int {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL(path), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

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

// CRED-03 — progressive lockout after repeated failures. The probe spends
// wrong passwords against a real endpoint until it is refused, and checks
// the CORRECT password is refused too: a lockout that lets the right
// password through only slows down an attacker who is already wrong.
func probeLockout(t *testing.T, e *env) verdict {
	srv, _ := e.accountsServer(t)
	email := fmt.Sprintf("lockout-%d@example.test", time.Now().UnixNano())
	password := "a-long-enough-passphrase"

	if status, _ := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusAccepted {
		t.Logf("register answered %d", status)
		return absent
	}

	locked := false
	for i := 0; i < 20; i++ {
		status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
			"email": email, "password": "definitely-the-wrong-one",
		})
		if status == http.StatusTooManyRequests {
			locked = true
			break
		}
	}
	if !locked {
		t.Log("twenty wrong passwords cost the caller nothing")
		return absent
	}
	if status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusTooManyRequests {
		t.Logf("the correct password bypassed the lockout (%d)", status)
		return partial
	}
	return present
}

// CRED-04 — password reset by single-use token, followed end to end: ask
// for the link, read it out of the mail, set a new password, sign in with
// it, and check the link cannot be replayed.
func probePasswordReset(t *testing.T, e *env) verdict {
	srv, mailer := e.accountsServer(t)
	email := fmt.Sprintf("reset-%d@example.test", time.Now().UnixNano())

	if status, _ := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": "a-long-enough-passphrase",
	}); status != http.StatusAccepted {
		return absent
	}
	if status, _ := postJSON(t, srv, accounts.RoutePasswordReset, map[string]string{"email": email}); status != http.StatusAccepted {
		return absent
	}
	token := mailer.lastToken()
	if token == "" {
		t.Log("no reset link was sent")
		return partial
	}

	newPassword := "a-brand-new-passphrase"
	if code := putJSON(t, srv, accounts.RoutePasswordReset, map[string]string{
		"token": token, "password": newPassword,
	}); code != http.StatusNoContent {
		t.Logf("reset answered %d", code)
		return partial
	}
	if status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": newPassword,
	}); status != http.StatusOK {
		t.Logf("the new password does not sign in (%d)", status)
		return partial
	}
	if code := putJSON(t, srv, accounts.RoutePasswordReset, map[string]string{
		"token": token, "password": "third-passphrase-here",
	}); code == http.StatusNoContent {
		t.Log("the reset link worked twice")
		return partial
	}
	return present
}

// CRED-05 — password change with re-authentication. Measured on the
// property that matters: an anonymous caller cannot change anybody's
// password.
func probePasswordChange(t *testing.T, e *env) verdict {
	srv, _ := e.accountsServer(t)
	status, _ := postJSON(t, srv, accounts.RoutePasswordChange, map[string]string{
		"current_password": "a-long-enough-passphrase", "new_password": "a-new-long-passphrase",
	})
	switch status {
	case http.StatusNotFound:
		return absent
	case http.StatusUnauthorized:
		return present
	default:
		t.Logf("an anonymous password change answered %d", status)
		return partial
	}
}

// CRED-06 — registration with email verification, end to end, INCLUDING
// the property that makes the endpoint safe: an address already registered
// answers exactly like a new one.
func probeEmailVerification(t *testing.T, e *env) verdict {
	srv, mailer := e.accountsServer(t)
	email := fmt.Sprintf("verify-%d@example.test", time.Now().UnixNano())

	firstStatus, firstBody := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": "a-long-enough-passphrase",
	})
	if firstStatus != http.StatusAccepted {
		t.Logf("register answered %d", firstStatus)
		return absent
	}
	token := mailer.lastToken()
	if token == "" {
		t.Log("no verification link was sent")
		return partial
	}

	secondStatus, secondBody := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": "another-long-passphrase",
	})
	if secondStatus != firstStatus || fmt.Sprint(secondBody) != fmt.Sprint(firstBody) {
		t.Logf("a taken address is distinguishable: %d %v vs %d %v", firstStatus, firstBody, secondStatus, secondBody)
		return partial
	}

	resp, err := srv.Client().Get(srv.URL(accounts.RouteVerifyEmail + "?token=" + token))
	if err != nil {
		t.Logf("verify: %v", err)
		return partial
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Logf("verify answered %d", resp.StatusCode)
		return partial
	}
	return present
}

// CRED-07 — magic-link sign-in, followed from the request to the session.
func probeMagicLink(t *testing.T, e *env) verdict {
	srv, mailer := e.accountsServer(t)
	email := fmt.Sprintf("magic-%d@example.test", time.Now().UnixNano())

	if status, _ := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": "a-long-enough-passphrase",
	}); status != http.StatusAccepted {
		return absent
	}
	if status, _ := postJSON(t, srv, accounts.RouteMagicLink, map[string]string{"email": email}); status != http.StatusAccepted {
		return absent
	}
	token := mailer.lastToken()
	if token == "" {
		return partial
	}

	resp, err := srv.Client().Get(srv.URL(accounts.RouteMagicLink + "?token=" + token))
	if err != nil {
		t.Logf("consume: %v", err)
		return partial
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Logf("the link answered %d", resp.StatusCode)
		return partial
	}

	// Once used, never again.
	again, err := srv.Client().Get(srv.URL(accounts.RouteMagicLink + "?token=" + token))
	if err == nil {
		defer func() { _ = again.Body.Close() }()
		if again.StatusCode == http.StatusOK {
			t.Log("the magic link worked twice")
			return partial
		}
	}
	return present
}

// CRED-08 — a login route the framework serves, with a session to show for
// it.
func probeLoginRoute(t *testing.T, e *env) verdict {
	srv, _ := e.accountsServer(t)
	email := fmt.Sprintf("login-%d@example.test", time.Now().UnixNano())
	password := "a-long-enough-passphrase"

	if status, _ := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusAccepted {
		return absent
	}
	status, body := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	})
	if status == http.StatusNotFound {
		return absent
	}
	if status != http.StatusOK || body["email"] != email {
		t.Logf("login answered %d: %v", status, body)
		return partial
	}
	return present
}
