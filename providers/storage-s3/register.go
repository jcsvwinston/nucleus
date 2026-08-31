// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Package s3 is the S3-compatible storage backend for Nucleus, published as
// its own module so the framework does not carry an object-storage client
// that most applications never open.
//
// Import it for its side effect and the name "s3" becomes available to
// `storage.provider` in nucleus.yml:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/storage-s3"
//
// It works against AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces and
// anything else that speaks the same protocol; the endpoint decides.
package s3

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

func init() {
	if err := provider.Register("s3", func(cfg provider.Config) (provider.Store, error) {
		return NewS3Store(cfg.S3)
	}); err != nil {
		panic("storage-s3: registering provider: " + err.Error())
	}
}
