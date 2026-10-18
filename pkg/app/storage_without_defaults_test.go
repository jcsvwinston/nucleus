// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// NU-99: an application built WithoutDefaults() builds no storage, and a
// storage block its configuration wrote was once ignored without a word — the
// api starter booted with `storage.provider: s3` exactly as it booted without
// it. A declared block is built by WithStorage(), and refused without it
// (DEP-2026-013); an application that declares no storage behaves as it
// always did.

func TestWithoutDefaults_NoStorageDeclared_BuildsNone(t *testing.T) {
	for name, opts := range map[string][]Option{
		"WithoutDefaults":               {WithoutDefaults()},
		"WithoutDefaults + WithStorage": {WithoutDefaults(), WithStorage()},
	} {
		t.Run(name, func(t *testing.T) {
			// The local default would create storage/ in the working
			// directory; nothing may appear there.
			dir := t.TempDir()
			t.Chdir(dir)
			a, err := New(testAppConfig(), opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() { _ = a.Shutdown(context.Background()) }()
			if a.Storage != nil {
				t.Fatalf("a configuration that declares no storage got a store (%T)", a.Storage)
			}
			if entries, _ := os.ReadDir(dir); len(entries) > 0 {
				t.Fatalf("building the application wrote to the working directory: %v", entries)
			}
		})
	}
}

// The combination the v1.x line reported with an ERROR line refuses to start
// (DEP-2026-013): New returns an error that names the provider, the option
// that builds the block, and the notice — and builds nothing on the way out.
func TestWithoutDefaults_DeclaredStorage_RefusesToStart(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cfg := testAppConfig()
	cfg.Storage.Provider = "memory"
	cfg.StorageDeclared = true

	a, err := New(cfg, WithoutDefaults())
	if err == nil {
		_ = a.Shutdown(context.Background())
		t.Fatal("WithoutDefaults() without WithStorage() started with a declared storage block")
	}
	msg := err.Error()
	for _, want := range []string{"storage block IGNORED", "provider=memory", "WithStorage()", "DEP-2026-013"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, msg)
		}
	}
	// memory is linked: there is no module to install, so no command.
	if strings.Contains(msg, "nucleus add") {
		t.Errorf("the refusal names a nucleus add for a provider that is linked:\n%s", msg)
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		t.Fatalf("the refused application wrote to the working directory: %v", entries)
	}
}

func TestWithoutDefaults_DeclaredUnlinkedProvider_RefusalNamesTheModuleToo(t *testing.T) {
	cfg := testAppConfig()
	cfg.Storage.Provider = "s3" // providers/storage-s3 is its own module, not linked here
	cfg.StorageDeclared = true

	a, err := New(cfg, WithoutDefaults())
	if err == nil {
		_ = a.Shutdown(context.Background())
		t.Fatal("WithoutDefaults() without WithStorage() started with a declared storage block")
	}
	for _, want := range []string{"WithStorage()", "nucleus add s3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}

// Nothing declared, nothing refused and nothing said: the refusal is for a
// block that exists.
func TestWithoutDefaults_NoStorageDeclared_NoWarning(t *testing.T) {
	cfg := testAppConfig()
	cfg.LogFormat = "text"
	out := captureStdout(t, func() {
		a, err := New(cfg, WithoutDefaults())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_ = a.Shutdown(context.Background())
	})
	if strings.Contains(out, "storage block IGNORED") {
		t.Fatalf("a configuration without storage got the warning:\n%s", out)
	}
}

func linesContaining(out, needle string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestWithStorage_BuildsTheDeclaredStorage(t *testing.T) {
	cfg := testAppConfig()
	cfg.Storage.Provider = "memory"
	cfg.StorageDeclared = true

	a, err := New(cfg, WithoutDefaults(), WithStorage())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()
	if a.Storage == nil {
		t.Fatal("WithStorage() built no store for a declared storage block")
	}
	if a.Mailer != nil || a.Authorizer != nil {
		t.Fatal("WithStorage() brought back other default subsystems")
	}
	ctx := context.Background()
	if _, err := a.Storage.Put(ctx, "probe.txt", strings.NewReader("stored"), storage.PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	r, _, err := a.Storage.Get(ctx, "probe.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = r.Close() }()
	got, _ := io.ReadAll(r)
	if !bytes.Equal(got, []byte("stored")) {
		t.Fatalf("Get answered %q", got)
	}
}

func TestWithStorage_DeclaredUnlinkedProvider_RefusesNamingNucleusAdd(t *testing.T) {
	cfg := testAppConfig()
	cfg.Storage.Provider = "gcs"
	cfg.Storage.GCS.Bucket = "uploads"
	cfg.StorageDeclared = true

	_, err := New(cfg, WithoutDefaults(), WithStorage())
	if err == nil {
		t.Fatal("expected the storage factory to refuse an unlinked provider")
	}
	if !strings.Contains(err.Error(), "nucleus add gcs") {
		t.Fatalf("the refusal does not name the command that installs the provider:\n%v", err)
	}
}

func TestWithStorage_WithTheDefaults_ChangesNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	a, err := New(testAppConfig(), WithStorage())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()
	if a.Storage == nil || a.Mailer == nil {
		t.Fatal("an application built with the defaults lost a default subsystem")
	}
}

func TestLoadConfig_RecordsWhetherStorageIsDeclared(t *testing.T) {
	cases := []struct {
		name string
		file string
		env  map[string]string
		want bool
	}{
		{name: "no storage block", file: "port: 8080\n", want: false},
		{name: "a provider", file: "storage:\n  provider: memory\n", want: true},
		// The struct cannot tell this from the default; the file can.
		{name: "the default written by hand", file: "storage:\n  provider: local\n", want: true},
		{name: "a setting without a provider", file: "storage:\n  local:\n    path: uploads/\n", want: true},
		{name: "the environment", file: "port: 8080\n", env: map[string]string{"NUCLEUS_STORAGE__PROVIDER": "memory"}, want: true},
		{name: "an empty variable", file: "port: 8080\n", env: map[string]string{"NUCLEUS_STORAGE__PROVIDER": ""}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			path := filepath.Join(t.TempDir(), "nucleus.yml")
			if err := os.WriteFile(path, []byte(tc.file), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.StorageDeclared != tc.want {
				t.Fatalf("StorageDeclared = %v, want %v", cfg.StorageDeclared, tc.want)
			}
		})
	}
}

// With the option, the declared block is built, and nothing is said or
// refused about it.
func TestWithStorage_DeclaredStorage_NoWarning(t *testing.T) {
	cfg := testAppConfig()
	cfg.LogFormat = "text"
	cfg.Storage.Provider = "memory"
	cfg.StorageDeclared = true
	out := captureStdout(t, func() {
		a, err := New(cfg, WithoutDefaults(), WithStorage())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_ = a.Shutdown(context.Background())
	})
	if strings.Contains(out, "storage block IGNORED") {
		t.Fatalf("WithStorage() built the block and still warned it was ignored:\n%s", out)
	}
}
