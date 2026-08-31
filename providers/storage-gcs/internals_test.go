// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package gcs

// Tests over the store's internals, moved here with the implementation: they
// name unexported types and the SDK's own errors, which is exactly the
// dependency this module exists to keep out of the framework.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gcstorage "cloud.google.com/go/storage"

	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
)

func TestIsGCSNotFound_WrappedSentinel(t *testing.T) {
	if !isGCSNotFound(gcstorage.ErrObjectNotExist) {
		t.Fatal("bare sentinel not classified as not found")
	}
	if !isGCSNotFound(fmt.Errorf("attrs: %w", gcstorage.ErrObjectNotExist)) {
		t.Fatal("wrapped sentinel not classified as not found")
	}
	if isGCSNotFound(errors.New("object not found")) {
		t.Fatal("text-only error must not be classified as not found")
	}
	if isGCSNotFound(nil) {
		t.Fatal("nil should not be treated as not found")
	}
}

func TestCloudURLHelpersValidateAndEscape(t *testing.T) {
	ctx := context.Background()

	gcs := &gcsStore{}
	if url, err := gcs.PublicURL(ctx, "safe/file.txt", provider.URLConfig{}); err != nil || url != "" {
		t.Fatalf("GCS PublicURL without public bucket = %q, %v", url, err)
	}
	if _, err := gcs.PublicURL(ctx, "../escape.txt", provider.URLConfig{}); err == nil {
		t.Fatal("GCS PublicURL should validate keys")
	}
	if got := provider.EscapeURLPath("folder/a b.txt"); got != "folder/a%20b.txt" {
		t.Fatalf("escapeURLPath = %q", got)
	}
}

// Every method validates the key BEFORE it reaches the client: a traversal
// attempt must be refused by us, not forwarded to the backend and refused
// there — or not refused at all. Split per module when the cloud providers
// moved out; the property is the same for each.
func TestProviderMethodsValidateKeysBeforeClients(t *testing.T) {

	ctx := context.Background()
	gcs := &gcsStore{}
	if _, _, err := gcs.Get(ctx, "../escape.txt"); err == nil {
		t.Fatal("GCS Get should reject invalid keys before using client")
	}
	if err := gcs.Delete(ctx, "../escape.txt"); err == nil {
		t.Fatal("GCS Delete should reject invalid keys before using client")
	}
	if _, err := gcs.Exists(ctx, "../escape.txt"); err == nil {
		t.Fatal("GCS Exists should reject invalid keys before using client")
	}
	if _, err := gcs.Copy(ctx, "source.txt", "../escape.txt"); err == nil {
		t.Fatal("GCS Copy should reject invalid keys before using client")
	}
}
