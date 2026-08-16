// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression test for QCD-FW-2 (quantum-coverage-demo): storage.Store had no
// bucket-provisioning path. S3Store's constructor probed connectivity but
// ignored the exists bool from BucketExists, so a deployment against a fresh
// MinIO/S3 booted "green" and failed on the first Put — and there was no
// bootstrap API to fix it from inside the framework (consumers shelled out
// to minio-go in OnStart by hand).
//
// Contract: S3Config.CreateBucketIfMissing provisions the configured
// bucket(s) at construction time, and S3Store.EnsureBucket exposes the same
// operation programmatically. Both are opt-in — the default keeps refusing
// to write DDL against production object stores.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func minioTestConfig(t *testing.T, bucket string) S3Config {
	t.Helper()
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
	secret, _ := u.User.Password()
	return S3Config{
		Endpoint:        u.Scheme + "://" + u.Host,
		Bucket:          bucket,
		AccessKeyID:     CredentialSource{Value: u.User.Username()},
		SecretAccessKey: CredentialSource{Value: secret},
	}
}

// A fresh object store plus CreateBucketIfMissing must yield a Store whose
// first Put works — the whole point of a bootstrap path.
func TestS3Live_CreateBucketIfMissing(t *testing.T) {
	bucket := fmt.Sprintf("qcd-fw2-%d", time.Now().UnixNano())
	cfg := minioTestConfig(t, bucket)
	cfg.CreateBucketIfMissing = true

	store, err := NewS3Store(cfg)
	if err != nil {
		t.Fatalf("NewS3Store with CreateBucketIfMissing on a fresh store: %v", err)
	}

	ctx := context.Background()
	if _, err := store.Put(ctx, "bootstrap/probe.txt", bytes.NewReader([]byte("hello")), PutOptions{}); err != nil {
		t.Fatalf("first Put against the freshly provisioned bucket: %v", err)
	}
}

// Without the opt-in the historic behaviour holds — but honestly: the
// constructor must now REFUSE a missing bucket instead of booting green and
// failing on first use ("exit 0 sin efecto", the class this whole arc hunts).
func TestS3Live_MissingBucketFailsLoudlyWithoutOptIn(t *testing.T) {
	bucket := fmt.Sprintf("qcd-fw2-absent-%d", time.Now().UnixNano())
	cfg := minioTestConfig(t, bucket)

	if _, err := NewS3Store(cfg); err == nil {
		t.Fatal("NewS3Store against a missing bucket without CreateBucketIfMissing must fail loudly, got nil")
	}
}

// EnsureBucket is the programmatic form (idempotent).
func TestS3Live_EnsureBucketIdempotent(t *testing.T) {
	bucket := fmt.Sprintf("qcd-fw2-ensure-%d", time.Now().UnixNano())
	cfg := minioTestConfig(t, bucket)
	cfg.CreateBucketIfMissing = true

	store, err := NewS3Store(cfg)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	ctx := context.Background()
	if err := store.EnsureBucket(ctx, bucket); err != nil {
		t.Fatalf("EnsureBucket must be idempotent on an existing bucket: %v", err)
	}
}
