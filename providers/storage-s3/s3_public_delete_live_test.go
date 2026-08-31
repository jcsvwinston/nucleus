// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"
	"fmt"
	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// QCD-FW-16: with a public_bucket configured, S3Store.Delete never reached
// it, and reported success anyway.
//
// The loop walks []string{bucket, publicBucket} and only advances to the
// second when isS3NotFound(err). But RemoveObject is IDEMPOTENT: removing a
// key that is not there returns nil, not NotFound. The private bucket
// therefore answered nil every time, Delete returned there, and the public
// bucket was never visited — isS3NotFound is unreachable in that function.
// Exists and Get share the loop and work, because StatObject/GetObject DO
// return NotFound; the contrast inside one file is what gives the bug away.
//
// The class is "exit 0 without the effect": retention, user-requested
// deletion and attachment cleanup all silently keep the object.
func TestS3Live_DeleteReachesThePublicBucket(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_STORAGE_MINIO_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_STORAGE_MINIO_URL not set; real-MinIO storage lane only")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse NUCLEUS_STORAGE_MINIO_URL: %v", err)
	}
	if u.User == nil {
		t.Fatal("NUCLEUS_STORAGE_MINIO_URL must carry credentials")
	}
	accessKey := u.User.Username()
	secretKey, _ := u.User.Password()

	ctx := context.Background()
	admin, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: u.Scheme == "https",
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}

	stamp := time.Now().UnixNano()
	private := fmt.Sprintf("nucleus-priv-%d", stamp)
	public := fmt.Sprintf("nucleus-pub-%d", stamp)
	for _, b := range []string{private, public} {
		if err := admin.MakeBucket(ctx, b, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("make bucket %q: %v", b, err)
		}
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, b := range []string{private, public} {
			for obj := range admin.ListObjects(c, b, minio.ListObjectsOptions{Recursive: true}) {
				if obj.Err == nil {
					_ = admin.RemoveObject(c, b, obj.Key, minio.RemoveObjectOptions{})
				}
			}
			_ = admin.RemoveBucket(c, b)
		}
	})

	store, err := NewS3Store(provider.S3Config{
		Endpoint:        u.Scheme + "://" + u.Host,
		Bucket:          private,
		PublicBucket:    public,
		AccessKeyID:     provider.CredentialSource{Value: accessKey},
		SecretAccessKey: provider.CredentialSource{Value: secretKey},
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	defer store.Close()

	const key = "ns5-borrame"

	if _, err := store.Put(ctx, key, strings.NewReader("contenido publico"), provider.PutOptions{Visibility: provider.Public}); err != nil {
		t.Fatalf("Put(public): %v", err)
	}
	// It really landed in the public bucket, or the test proves nothing.
	if _, err := admin.StatObject(ctx, public, key, minio.StatObjectOptions{}); err != nil {
		t.Fatalf("the object must be in the public bucket for this test to mean anything: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, err := store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after Delete: %v", err)
	}
	if exists {
		t.Fatal("Delete returned nil and the object is still there — a public object is undeletable through the provider.Store's own API")
	}
	if _, err := admin.StatObject(ctx, public, key, minio.StatObjectOptions{}); err == nil {
		t.Fatal("the object is still in the public bucket after Delete")
	}
}
