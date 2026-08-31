// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package azure is the Azure Blob Storage backend for Nucleus, published as
// its own module so the framework does not link the Azure SDK for
// applications that never touch a container.
//
// Import it for its side effect and the name "azure" becomes available to
// `storage.provider` in nucleus.yml:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/storage-azure"
package azure

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

func init() {
	if err := provider.Register("azure", func(cfg provider.Config) (provider.Store, error) {
		return NewAzureStore(cfg.Azure)
	}); err != nil {
		panic("storage-azure: registering provider: " + err.Error())
	}
}
