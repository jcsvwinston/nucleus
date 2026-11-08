// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package providerns

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

func entry(t *testing.T, name string) knownproviders.Entry {
	t.Helper()
	e, ok := knownproviders.Lookup(name)
	if !ok {
		t.Fatalf("%s is not in the catalog", name)
	}
	return e
}

// NU-132: a provider the project links is exempt under its namespace, the
// same way a registered one is — and only that one. The names come from
// the catalog groups, so the mapping cannot drift from what `nucleus add`
// installs.
func TestFromEntriesMapsGroupsToNamespaces(t *testing.T) {
	d := FromEntries([]knownproviders.Entry{
		entry(t, "s3"), entry(t, "ldap"), entry(t, "sentry"),
		entry(t, "postgres"), entry(t, "aws-sm"), // own no configuration subtree
	})
	if strings.Join(d.Storage, ",") != "s3" || strings.Join(d.Auth, ",") != "ldap" || strings.Join(d.Interceptors, ",") != "sentry" {
		t.Fatalf("FromEntries = %+v", d)
	}

	for key, want := range map[string]bool{
		"storage.s3.bucket":        true,
		"auth.ldap.url":            true,
		"interceptors.sentry.dsn":  true,
		"storage.gcs.bucket":       false, // published, not linked
		"interceptors.sentry":      false, // the bare name is not a subtree
		"storage.s4.bucket":        false, // a typo stays unknown
		"interceptors.datadog.key": false,
	} {
		if got := IsProviderKey(key, d); got != want {
			t.Errorf("IsProviderKey(%q) with s3/ldap/sentry linked = %v, want %v", key, got, want)
		}
	}
	// Without the declaration nothing changed: the registries alone decide.
	if IsProviderKey("interceptors.sentry.dsn", Declared{}) {
		t.Fatal("sentry is exempt with nothing linked and nothing registered")
	}
}

// SetLinked is what the CLI calls once per command; every validator then
// sees the names through NamespacesWith, whatever Declared it was handed.
func TestSetLinkedReachesEveryValidator(t *testing.T) {
	SetLinked(FromEntries([]knownproviders.Entry{entry(t, "sentry"), entry(t, "ldap")}))
	t.Cleanup(func() { SetLinked(Declared{}) })

	if !IsProviderKey("interceptors.sentry.dsn", Declared{}) {
		t.Fatal("a linked interceptor's subtree is still an unknown key")
	}
	if got := StripKeys([]string{"interceptors.sentry.dsn", "auth.ldap.url", "prot"}, Declared{}); len(got) != 1 || got[0] != "prot" {
		t.Fatalf("StripKeys left %v, want [prot]", got)
	}
	if !contains(Namespaces()["auth"], "ldap") {
		t.Fatalf("Namespaces()[auth] = %v, want ldap among them", Namespaces()["auth"])
	}
	// FederatedAuth from the file and the linked names add up.
	if !IsProviderKey("auth.corp.issuer", Declared{FederatedAuth: []string{"corp"}}) {
		t.Fatal("a declared federated instance lost its exemption when something was linked")
	}
	SetLinked(Declared{})
	if IsProviderKey("interceptors.sentry.dsn", Declared{}) {
		t.Fatal("clearing the linked set did not take")
	}
}

// The refusal for a provider the project does not link names the import
// that would link it, not only the command: the person who already ran
// `nucleus add` and sees the key refused needs to know what to look for in
// main.go.
func TestNotInstalledNoteNamesTheImport(t *testing.T) {
	note := NotInstalledNote([]string{"interceptors.sentry.dsn", "interceptors.sentry.release", "prot"})
	for _, want := range []string{
		"interceptors.sentry.* configures the sentry error reporter",
		"the project's main.go does not import it",
		"`nucleus add sentry`",
		"`_ \"github.com/jcsvwinston/nucleus/providers/errors-sentry\"`",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if strings.Count(note, "nucleus add sentry") != 1 {
		t.Errorf("one note per module, got:\n%s", note)
	}
	if NotInstalledNote([]string{"prot"}) != "" {
		t.Error("a key no provider owns must add no note")
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
