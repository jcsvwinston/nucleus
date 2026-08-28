// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package storage

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

// The contract a third-party storage backend implements now lives in
// pkg/storage/provider, a leaf package.
//
// The reason is a measurement: this package links 301 third-party packages
// — the AWS, Azure and Google Cloud SDKs, because the S3, Blob and GCS
// implementations live here — and until now that was the price of
// implementing the Store interface. Someone writing a backend for Ceph, for
// an internal object store, or for a filesystem they control inherited
// three cloud SDKs they will never call. The leaf links two.
//
// The names below are ALIASES, not copies: storage.Store and
// provider.Store are the same interface, storage.Config and provider.Config
// the same struct, so everything that compiled before still compiles. A new
// backend should import pkg/storage/provider and pay for what it uses.
type (
	// Store is the durable object storage interface. See provider.Store.
	Store = provider.Store
	// Config holds the complete storage configuration.
	Config = provider.Config
	// ProviderFactory builds a Store from the configuration.
	ProviderFactory = provider.Factory

	ObjectInfo   = provider.ObjectInfo
	PutOptions   = provider.PutOptions
	ListOptions  = provider.ListOptions
	ListResult   = provider.ListResult
	URLConfig    = provider.URLConfig
	Visibility   = provider.Visibility
	ProviderType = provider.ProviderType

	ErrNotFound   = provider.ErrNotFound
	ErrInvalidKey = provider.ErrInvalidKey

	CredentialSource     = provider.CredentialSource
	S3Config             = provider.S3Config
	GCSConfig            = provider.GCSConfig
	AzureConfig          = provider.AzureConfig
	LocalConfig          = provider.LocalConfig
	CleanupConfig        = provider.CleanupConfig
	CircuitBreakerConfig = provider.CircuitBreakerConfig
)

const (
	Private = provider.Private
	Public  = provider.Public

	ProviderS3    = provider.ProviderS3
	ProviderGCS   = provider.ProviderGCS
	ProviderAzure = provider.ProviderAzure
	ProviderLocal = provider.ProviderLocal
)

// DefaultConfig returns the default storage configuration.
func DefaultConfig() Config { return provider.DefaultConfig() }

// RegisterProvider makes a storage backend selectable by name from
// configuration (`storage.provider`). It delegates to provider.Register; a
// new backend should call that one and avoid importing this package at all.
func RegisterProvider(name string, factory ProviderFactory) error {
	return provider.Register(name, factory)
}

// RegisteredProviders returns every selectable provider name, sorted.
func RegisteredProviders() []string { return provider.Registered() }

// unregisterProviderForTest removes a provider. Test-only.
func unregisterProviderForTest(name string) { provider.Unregister(name) }
