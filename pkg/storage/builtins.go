// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package storage

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

func init() {
	// Local is the one backend the framework carries: it needs no client
	// library and it is what a fresh project writes to. The cloud backends
	// register the same way from their own modules — the door a third party
	// uses is literally the only door there is.
	mustRegister(string(ProviderLocal), func(cfg Config) (Store, error) { return NewLocalStore(cfg.Local) })
	// Memory is the other backend that needs nothing: objects live in the
	// process and go with it. It is what a test wants (pkg/nucleustest
	// reads it back) and what a demo that must not touch the disk wants.
	mustRegister("memory", func(Config) (Store, error) { return NewMemoryStore(), nil })
}

func mustRegister(name string, factory ProviderFactory) {
	if err := provider.Register(name, factory); err != nil {
		panic("storage: registering built-in provider: " + err.Error())
	}
}
