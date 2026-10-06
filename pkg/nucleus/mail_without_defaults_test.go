// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
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

// NU-114 on the builder: the api starter's shape with a mail_driver and no
// WithMail still starts — what booted before keeps booting until the major
// (QADR-0010) — and builds no sender for the driver it ignores. The ERROR
// line it logs is pinned in pkg/app, where the logger is.
func TestBuilder_WithoutDefaults_DeclaredMailDriverStillStarts(t *testing.T) {
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(starterConfig(t, "mail_driver: memory\n")).
		WithOpenAuthz().
		WithoutDefaults())
	t.Cleanup(srv.Stop)
	if m := srv.Runtime().Mailer(); m != nil {
		t.Fatalf("WithoutDefaults() without WithMail() built a mail sender (%T)", m)
	}
}
