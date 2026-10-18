// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// The builder's loader records whether the configuration WROTE mail_driver
// on the same terms as app.LoadConfig — including the default written by
// hand, which provenance alone attributes to the default (NU-114).
func TestFromConfigFile_RecordsWhetherMailIsDeclared(t *testing.T) {
	cases := []struct {
		name  string
		block string
		env   map[string]string
		want  bool
	}{
		{name: "no mail_driver", want: false},
		{name: "a driver", block: "mail_driver: memory\n", want: true},
		{name: "the default written by hand", block: "mail_driver: noop\n", want: true},
		{name: "smtp keys without a driver", block: "smtp_host: smtp.example.test\n", want: false},
		{name: "the environment", env: map[string]string{"NUCLEUS_MAIL_DRIVER": "memory"}, want: true},
		{name: "an empty variable", env: map[string]string{"NUCLEUS_MAIL_DRIVER": ""}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			a, err := nucleus.New().FromConfigFile(starterConfig(t, tc.block)).WithoutDefaults().Build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if a.Config.MailDeclared != tc.want {
				t.Fatalf("MailDeclared = %v, want %v", a.Config.MailDeclared, tc.want)
			}
		})
	}
}

// NU-114 on the builder: a WithoutDefaults() application whose nucleus.yml
// writes a mail_driver refuses to start without WithMail() (DEP-2026-015).
// The error's wording is pinned in pkg/app, where it is built.
func TestBuilder_WithoutDefaults_DeclaredMailDriverRefusesToStart(t *testing.T) {
	err := bootError(t, nucleus.New().
		FromConfigFile(starterConfig(t, "mail_driver: memory\n")).
		WithoutDefaults())
	if err == nil {
		t.Fatal("WithoutDefaults() without WithMail() started with a declared mail driver")
	}
	for _, want := range []string{"mail_driver IGNORED", "WithMail()", "DEP-2026-015"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}
