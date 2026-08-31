// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package s3

// Tests over the store's internals, moved here with the implementation: they
// name unexported types and the SDK's own errors, which is exactly the
// dependency this module exists to keep out of the framework.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
	"github.com/minio/minio-go/v7"
)

func TestS3Helpers(t *testing.T) {
	store := &S3Store{bucket: "private-bucket", publicBucket: "public-bucket"}

	if got := store.resolveBucket(provider.PutOptions{}); got != "private-bucket" {
		t.Fatalf("private bucket = %q", got)
	}
	if got := store.resolveBucket(provider.PutOptions{Visibility: provider.Public}); got != "public-bucket" {
		t.Fatalf("public bucket = %q", got)
	}
	if got := store.detectContentType("image.png", provider.PutOptions{}); got != "image/png" {
		t.Fatalf("detected content type = %q", got)
	}
	if got := store.detectContentType("file.unknown", provider.PutOptions{}); got != "application/octet-stream" {
		t.Fatalf("fallback content type = %q", got)
	}
	if got := store.detectContentType("file.bin", provider.PutOptions{ContentType: "application/custom"}); got != "application/custom" {
		t.Fatalf("explicit content type = %q", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.PublicURLBase(context.Background(), provider.URLConfig{ContentType: "https://cdn.example.com"}); got != "https://cdn.example.com" {
		t.Fatalf("PublicURLBase = %q", got)
	}
	if isS3NotFound(nil) {
		t.Fatal("nil should not be treated as not found")
	}
}

func TestIsS3NotFound_TypedSDKErrors(t *testing.T) {
	// The exact error a real S3/MinIO endpoint produces for a missing key.
	// Its Error() text contains neither "NoSuchKey" nor "not found".
	realKeyErr := minio.ErrorResponse{
		Code:       minio.NoSuchKey,
		Message:    "The specified key does not exist.",
		StatusCode: http.StatusNotFound,
	}
	if !strings.Contains(realKeyErr.Error(), "The specified key does not exist.") {
		t.Fatalf("test premise broken: Error() = %q", realKeyErr.Error())
	}
	if !isS3NotFound(realKeyErr) {
		t.Fatalf("real NoSuchKey response not classified as not found: %v", realKeyErr)
	}

	// Wrapped by a caller with %w — errors.As must still find it.
	if !isS3NotFound(fmt.Errorf("storage: S3 Get %q: %w", "missing.txt", realKeyErr)) {
		t.Fatal("wrapped NoSuchKey response not classified as not found")
	}

	// Missing bucket.
	if !isS3NotFound(minio.ErrorResponse{
		Code:       minio.NoSuchBucket,
		Message:    "The specified bucket does not exist.",
		StatusCode: http.StatusNotFound,
	}) {
		t.Fatal("NoSuchBucket response not classified as not found")
	}

	// A 404 whose code we do not recognize still means the object is gone.
	if !isS3NotFound(minio.ErrorResponse{StatusCode: http.StatusNotFound}) {
		t.Fatal("plain 404 response not classified as not found")
	}

	// Negative: other typed API errors must not map to not-found.
	if isS3NotFound(minio.ErrorResponse{
		Code:       minio.AccessDenied,
		Message:    "Access Denied.",
		StatusCode: http.StatusForbidden,
	}) {
		t.Fatal("AccessDenied must not be classified as not found")
	}

	// Negative: untyped errors carry no S3 semantics, whatever their text.
	if isS3NotFound(errors.New("NoSuchKey: fabricated text error")) {
		t.Fatal("text-only error must not be classified as not found")
	}
}

// Every method validates the key BEFORE it reaches the client: a traversal
// attempt must be refused by us, not forwarded to the backend and refused
// there — or not refused at all. Split per module when the cloud providers
// moved out; the property is the same for each.
func TestProviderMethodsValidateKeysBeforeClients(t *testing.T) {

	ctx := context.Background()
	s3 := &S3Store{}
	if _, err := s3.Put(ctx, "../escape.txt", strings.NewReader("x"), provider.PutOptions{}); err == nil {
		t.Fatal("S3 Put should reject invalid keys before using client")
	}
	if _, _, err := s3.Get(ctx, "../escape.txt"); err == nil {
		t.Fatal("S3 Get should reject invalid keys before using client")
	}
	if err := s3.Delete(ctx, "../escape.txt"); err == nil {
		t.Fatal("S3 Delete should reject invalid keys before using client")
	}
	if _, err := s3.Exists(ctx, "../escape.txt"); err == nil {
		t.Fatal("S3 Exists should reject invalid keys before using client")
	}
	if _, err := s3.List(ctx, provider.ListOptions{Prefix: "../"}); err == nil {
		t.Fatal("S3 List should reject invalid prefixes before using client")
	}
	if _, err := s3.PublicURL(ctx, "../escape.txt", provider.URLConfig{}); err == nil {
		t.Fatal("S3 PublicURL should reject invalid keys before using client")
	}
	if _, err := s3.SignedURL(ctx, "../escape.txt", time.Hour, provider.URLConfig{}); err == nil {
		t.Fatal("S3 SignedURL should reject invalid keys before using client")
	}
	if _, err := s3.Copy(ctx, "source.txt", "../escape.txt"); err == nil {
		t.Fatal("S3 Copy should reject invalid keys before using client")
	}
}
