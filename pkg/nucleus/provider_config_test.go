// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco B: un proveedor registrado puede declarar su propia configuración.
//
// The registry of Arco A let you plug a storage backend in by name. It did
// not let you configure it: `storage.ceph.endpoint` died as an unknown key
// before the provider ever ran, so the seam stopped one step short of
// useful. A backend nobody can configure is a backend nobody can deploy.
package nucleus

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

type cephish struct{ endpoint string }

func (c *cephish) Put(context.Context, string, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}
func (c *cephish) Get(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return nil, storage.ObjectInfo{}, nil
}
func (c *cephish) Delete(context.Context, string) error         { return nil }
func (c *cephish) Exists(context.Context, string) (bool, error) { return false, nil }
func (c *cephish) List(context.Context, storage.ListOptions) (storage.ListResult, error) {
	return storage.ListResult{}, nil
}
func (c *cephish) PublicURL(context.Context, string, storage.URLConfig) (string, error) {
	return "", nil
}
func (c *cephish) SignedURL(context.Context, string, time.Duration, storage.URLConfig) (string, error) {
	return "", nil
}
func (c *cephish) Copy(context.Context, string, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}
func (c *cephish) Close() error { return nil }

// The provider declares its own shape, exactly as a third party would.
type cephConfig struct {
	Endpoint string        `koanf:"endpoint" validate:"required"`
	Pool     int           `koanf:"pool" default:"8"`
	Timeout  time.Duration `koanf:"timeout" default:"5s"`
}

func TestProviderConfig_RegisteredNamespaceLoadsAndBinds(t *testing.T) {
	var bound cephConfig
	if err := storage.RegisterProvider("cephtest", func(cfg storage.Config) (storage.Store, error) {
		if err := cfg.BindProvider(&bound); err != nil {
			return nil, err
		}
		return &cephish{endpoint: bound.Endpoint}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(path, []byte("storage:\n  provider: cephtest\n  cephtest:\n    endpoint: \"http://ceph.local\"\n    pool: 32\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// (1) The namespace of a REGISTERED provider must survive the
	// unknown-key guard.
	app, err := New().FromConfigFile(path).Build()
	if err != nil {
		t.Fatalf("a registered provider's config namespace must load: %v", err)
	}

	// (2) The subtree must reach the provider, typed, with defaults for
	// what the file left out.
	cfg := storageConfigForTest(app.Config)
	if _, err := storage.New(cfg, nil); err != nil {
		t.Fatalf("build the provider: %v", err)
	}
	if bound.Endpoint != "http://ceph.local" {
		t.Errorf("the file's value must reach the provider, got %q", bound.Endpoint)
	}
	if bound.Pool != 32 {
		t.Errorf("the file must win over the default tag, got %d", bound.Pool)
	}
	if bound.Timeout != 5*time.Second {
		t.Errorf("a field the file omitted must come from its default tag, got %v", bound.Timeout)
	}
}

// The exemption is for REGISTERED providers only. A typo under storage.
// must still fail, or the namespace becomes a hole where any misspelling
// passes.
func TestProviderConfig_TypoStillFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	if err := os.WriteFile(path, []byte("storage:\n  provider: local\n  cehp:\n    endpoint: \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New().FromConfigFile(path).Build()
	if err == nil {
		t.Fatal("an unregistered namespace must still be rejected as an unknown key")
	}
	if !strings.Contains(err.Error(), "cehp") {
		t.Errorf("the error must name the offending key, got %v", err)
	}
}

// storageConfigForTest reaches the same conversion Run uses.
func storageConfigForTest(c app.Config) storage.Config {
	return c.ToStorageConfig()
}

// Arco B shipped the exemption on ONE of the two loading paths. The
// builder accepted `storage.<registered>.…`; app.LoadConfig — what every
// CLI command runs on — rejected it as an unknown key, so a deployment on
// a third-party backend booted a healthy server whose own `nucleus check`,
// `doctor` and `config print` all called the file malformed.
//
// The rule now lives in one place (internal/providerns). This test is what
// would have caught its absence: it asks both validators about the same
// file and requires the same verdict.
func TestProviderConfig_OneFileOneVerdict(t *testing.T) {
	if err := storage.RegisterProvider("verdicttest", func(cfg storage.Config) (storage.Store, error) {
		return &cephish{}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	for _, tc := range []struct {
		name    string
		yml     string
		wantErr bool
	}{
		{
			name: "registered provider subtree",
			yml:  "storage:\n  provider: verdicttest\n  verdicttest:\n    endpoint: \"http://x\"\n",
		},
		{
			name:    "typo under the same namespace",
			yml:     "storage:\n  provider: local\n  verdictest:\n    endpoint: \"http://x\"\n",
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nucleus.yml")
			if err := os.WriteFile(path, []byte(tc.yml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, errBuilder := New().FromConfigFile(path).Build()
			_, errLoad := app.LoadConfig(path)
			if (errBuilder != nil) != (errLoad != nil) {
				t.Fatalf("the same file must get the same verdict from both paths:\n  builder:    %v\n  LoadConfig: %v", errBuilder, errLoad)
			}
			if (errBuilder != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v, got %v", tc.wantErr, errBuilder)
			}
		})
	}
}
