// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package storage

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

func init() {
	// The built-ins register through the same door as anyone else, and now
	// they do it from OUTSIDE the package that owns the registry — which is
	// the strongest version of that rule: the path a third party takes is
	// literally the only path there is.
	mustRegister(string(ProviderLocal), func(cfg Config) (Store, error) { return NewLocalStore(cfg.Local) })
	mustRegister(string(ProviderS3), func(cfg Config) (Store, error) { return NewS3Store(cfg.S3) })
	mustRegister(string(ProviderGCS), func(cfg Config) (Store, error) { return NewGCSStore(cfg.GCS) })
	mustRegister(string(ProviderAzure), func(cfg Config) (Store, error) { return NewAzureStore(cfg.Azure) })
}

func mustRegister(name string, factory ProviderFactory) {
	if err := provider.Register(name, factory); err != nil {
		panic("storage: registering built-in provider: " + err.Error())
	}
}
