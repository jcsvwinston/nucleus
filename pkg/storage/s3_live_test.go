package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// TestS3Live_MinIO exercises the S3 store against a real MinIO endpoint: it
// runs only when NUCLEUS_STORAGE_MINIO_URL is set (the storage-minio CI
// lane). The URL carries the credentials, e.g.
// http://minioadmin:minioadmin@127.0.0.1:9000.
//
// This is the lane issue #227 was missing: the unit tests fabricated errors
// with "NoSuchKey" in the text, so the text-matching classifier passed while
// a real endpoint — whose message is "The specified key does not exist.",
// with the code only in the typed response — never mapped a missing key to
// storage.ErrNotFound.
func TestS3Live_MinIO(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_STORAGE_MINIO_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_STORAGE_MINIO_URL not set; real-MinIO storage lane only")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse NUCLEUS_STORAGE_MINIO_URL: %v", err)
	}
	if u.User == nil {
		t.Fatal("NUCLEUS_STORAGE_MINIO_URL must carry credentials, e.g. http://user:pass@host:port")
	}
	accessKey := u.User.Username()
	secretKey, _ := u.User.Password()

	ctx := context.Background()

	// Provision a dedicated bucket with a raw client — NewS3Store refuses to
	// start against a bucket that does not exist.
	admin, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: u.Scheme == "https",
	})
	if err != nil {
		t.Fatalf("create admin client: %v", err)
	}
	bucket := fmt.Sprintf("nucleus-live-%d", time.Now().UnixNano())
	if err := admin.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("make bucket %q: %v", bucket, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for obj := range admin.ListObjects(cleanupCtx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err == nil {
				_ = admin.RemoveObject(cleanupCtx, bucket, obj.Key, minio.RemoveObjectOptions{})
			}
		}
		_ = admin.RemoveBucket(cleanupCtx, bucket)
	})

	store, err := NewS3Store(S3Config{
		Endpoint:        u.Scheme + "://" + u.Host,
		Bucket:          bucket,
		AccessKeyID:     CredentialSource{Value: accessKey},
		SecretAccessKey: CredentialSource{Value: secretKey},
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	defer store.Close()

	const key = "e2e/hello.txt"
	const content = "hello from the storage-minio lane"

	// Put.
	info, err := store.Put(ctx, key, strings.NewReader(content), PutOptions{})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.Key != key {
		t.Fatalf("Put info.Key = %q, want %q", info.Key, key)
	}

	// Get returns the stored bytes.
	reader, getInfo, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("read Get body: %v", err)
	}
	if string(got) != content {
		t.Fatalf("Get body = %q, want %q", got, content)
	}
	if getInfo.Size != int64(len(content)) {
		t.Fatalf("Get info.Size = %d, want %d", getInfo.Size, len(content))
	}

	// Exists on a stored key.
	exists, err := store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists = false for a stored key")
	}

	// The issue #227 pin: Get of a missing key against a real endpoint must
	// map to storage.ErrNotFound, not surface a generic SDK error.
	_, _, err = store.Get(ctx, "does/not-exist.txt")
	if err == nil {
		t.Fatal("Get of a missing key returned nil error")
	}
	var notFound ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("Get of a missing key = %T %q, want storage.ErrNotFound", err, err)
	}

	// Exists of a missing key is false with no error.
	exists, err = store.Exists(ctx, "does/not-exist.txt")
	if err != nil {
		t.Fatalf("Exists of a missing key: %v", err)
	}
	if exists {
		t.Fatal("Exists = true for a missing key")
	}

	// Delete, then the key is gone.
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after Delete: %v", err)
	}
	if exists {
		t.Fatal("Exists = true after Delete")
	}

	// Delete of a missing key is idempotent.
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a missing key: %v", err)
	}
}
