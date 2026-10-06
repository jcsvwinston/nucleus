// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package providerns

import (
	"strings"
	"testing"
)

func TestNotInstalled_RecognisesAPublishedBackendsSubtree(t *testing.T) {
	for key, want := range map[string]string{
		"auth.ldap.url":       "ldap",
		"auth.LDAP.base_dn":   "ldap",
		"storage.s3.bucket":   "s3",
		"storage.gcs.bucket":  "gcs",
		"storage.azure.x":     "azure",
		"auth.corp.client_id": "",
		"auth.ldap":           "",
		"databases.ldap.url":  "",
		"ldap":                "",
	} {
		p, ok := NotInstalled(key)
		if ok != (want != "") || p.Name != want {
			t.Errorf("NotInstalled(%q) = %q, %v; want %q", key, p.Name, ok, want)
		}
	}
}

func TestNotInstalledNote_OncePerBackend(t *testing.T) {
	note := NotInstalledNote([]string{"auth.ldap.url", "auth.ldap.base_dn", "prot"})
	if strings.Count(note, "configures the ldap") != 1 {
		t.Fatalf("the note should explain ldap once:\n%s", note)
	}
	for _, want := range []string{"auth.ldap.*", "github.com/jcsvwinston/nucleus/providers/ldap", "`nucleus add ldap`"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q:\n%s", want, note)
		}
	}
	if NotInstalledNote([]string{"prot"}) != "" {
		t.Error("a plain typo got a not-installed note")
	}
}

func TestWritesStorage(t *testing.T) {
	cases := []struct {
		keys map[string]any
		want bool
	}{
		{map[string]any{"port": 8080}, false},
		{map[string]any{"storage.provider": "local"}, true},
		{map[string]any{"storage.local.path": "uploads/"}, true},
		{map[string]any{"storage.provider": nil}, false},
		{map[string]any{"storage_driver": "s3"}, false},
	}
	for _, tc := range cases {
		if got := WritesStorage(tc.keys); got != tc.want {
			t.Errorf("WritesStorage(%v) = %v, want %v", tc.keys, got, tc.want)
		}
	}
}

func TestWritesMail(t *testing.T) {
	cases := []struct {
		keys map[string]any
		want bool
	}{
		{map[string]any{"port": 8080}, false},
		{map[string]any{"mail_driver": "smtp"}, true},
		{map[string]any{"mail_driver": "noop"}, true},
		{map[string]any{"mail_driver": nil}, false},
		{map[string]any{"smtp_host": "smtp.example.test", "smtp_port": 587}, false},
	}
	for _, tc := range cases {
		if got := WritesMail(tc.keys); got != tc.want {
			t.Errorf("WritesMail(%v) = %v, want %v", tc.keys, got, tc.want)
		}
	}
}
