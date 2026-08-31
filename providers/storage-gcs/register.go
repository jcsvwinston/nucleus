// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package gcs is the Google Cloud Storage backend for Nucleus, published as
// its own module so the framework does not link the Google Cloud SDK for
// applications that never touch a bucket.
//
// Import it for its side effect and the name "gcs" becomes available to
// `storage.provider` in nucleus.yml:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/storage-gcs"
package gcs

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

func init() {
	if err := provider.Register("gcs", func(cfg provider.Config) (provider.Store, error) {
		return NewGCSStore(cfg.GCS)
	}); err != nil {
		panic("storage-gcs: registering provider: " + err.Error())
	}
}
