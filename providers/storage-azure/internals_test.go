// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
)

// Every method validates the key BEFORE it reaches the client: a traversal
// attempt must be refused by us, not forwarded to the backend and refused
// there — or not refused at all. Split per module when the cloud providers
// moved out; the property is the same for each.
func TestProviderMethodsValidateKeysBeforeClients(t *testing.T) {

	ctx := context.Background()
	azure := &azureStore{}
	if _, _, err := azure.Get(ctx, "../escape.txt"); err == nil {
		t.Fatal("Azure Get should reject invalid keys before using client")
	}
	if err := azure.Delete(ctx, "../escape.txt"); err == nil {
		t.Fatal("Azure Delete should reject invalid keys before using client")
	}
	if _, err := azure.Exists(ctx, "../escape.txt"); err == nil {
		t.Fatal("Azure Exists should reject invalid keys before using client")
	}
	if _, err := azure.Copy(ctx, "source.txt", "../escape.txt"); err == nil {
		t.Fatal("Azure Copy should reject invalid keys before using client")
	}
}

// The public-URL contract for this backend: no public container configured
// means no public URL — an empty string and no error — and a traversal key
// is refused before the client sees it. Split from the cross-provider test
// when the cloud backends moved to their own modules.
func TestAzurePublicURLContract(t *testing.T) {
	ctx := context.Background()
	azure := &azureStore{}
	if url, err := azure.PublicURL(ctx, "safe/file.txt", provider.URLConfig{}); err != nil || url != "" {
		t.Fatalf("Azure PublicURL without public container = %q, %v", url, err)
	}
	if _, err := azure.PublicURL(ctx, "../escape.txt", provider.URLConfig{}); err == nil {
		t.Fatal("Azure PublicURL should validate keys")
	}
}
