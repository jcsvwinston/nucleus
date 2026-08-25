// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco A del plan de extensibilidad, §1: los subsistemas que eligen backend
// dejan de hacerlo con un switch cerrado.
//
// A framework people can extend is one where the piece that does not fit
// their environment can be replaced without forking it. Until this change
// exactly one subsystem out of six could be extended from outside — mail,
// through RegisterProvider. Storage selected its backend with a switch over
// four hard-coded constants, so an operator running Ceph, Swift or
// Backblaze had no path that did not involve patching the framework.
//
// The registry is the same shape mail already proved, and the same shape
// quark uses for dialects: register by name, look up by name, built-ins
// registered like anyone else.
package storage

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeStore is the minimum a third-party provider must implement.
type fakeStore struct{ name string }

func (f *fakeStore) Put(context.Context, string, io.Reader, PutOptions) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (f *fakeStore) Get(context.Context, string) (io.ReadCloser, ObjectInfo, error) {
	return nil, ObjectInfo{}, nil
}
func (f *fakeStore) Delete(context.Context, string) error         { return nil }
func (f *fakeStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (f *fakeStore) List(context.Context, ListOptions) (ListResult, error) {
	return ListResult{}, nil
}
func (f *fakeStore) PublicURL(context.Context, string, URLConfig) (string, error) {
	return "", nil
}
func (f *fakeStore) SignedURL(context.Context, string, time.Duration, URLConfig) (string, error) {
	return "", nil
}
func (f *fakeStore) Copy(context.Context, string, string) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (f *fakeStore) Close() error { return nil }

func TestRegisterProvider_ThirdPartyBackend(t *testing.T) {
	const name = "testfs"
	t.Cleanup(func() { unregisterProviderForTest(name) })

	if err := RegisterProvider(name, func(Config) (Store, error) {
		return &fakeStore{name: name}, nil
	}); err != nil {
		t.Fatalf("a third-party provider must be registrable: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Provider = ProviderType(name)
	store, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New must build the registered provider: %v", err)
	}
	if _, ok := store.(*fakeStore); !ok {
		t.Fatalf("New returned %T, want the registered provider", store)
	}
}

// Built-ins go through the same door, or the registry is decoration.
func TestRegisteredProviders_IncludesBuiltIns(t *testing.T) {
	got := strings.Join(RegisteredProviders(), " ")
	for _, want := range []string{"local", "s3", "gcs", "azure"} {
		if !strings.Contains(got, want) {
			t.Errorf("built-in %q must be registered like any other provider, got: %s", want, got)
		}
	}
}

func TestRegisterProvider_Rejects(t *testing.T) {
	if err := RegisterProvider("", func(Config) (Store, error) { return nil, nil }); err == nil {
		t.Error("an empty name must be rejected")
	}
	if err := RegisterProvider("nilfactory", nil); err == nil {
		t.Error("a nil factory must be rejected")
	}
	if err := RegisterProvider("local", func(Config) (Store, error) { return nil, nil }); err == nil {
		t.Error("silently replacing a registered provider would make the effective backend depend on init order")
	}
}

// An unknown provider must say what IS available: the error is the only
// place a plugin author learns the registry exists.
func TestNew_UnknownProviderListsTheKnownOnes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider = "cephfs"
	_, err := New(cfg, nil)
	if err == nil {
		t.Fatal("an unknown provider must fail")
	}
	for _, want := range []string{"cephfs", "local", "s3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error must name the unknown provider and the registered ones (missing %q): %v", want, err)
		}
	}
}
