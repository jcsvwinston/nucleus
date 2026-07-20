package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gcstorage "cloud.google.com/go/storage"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/minio/minio-go/v7"
)

func TestLocalStore_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	content := strings.NewReader("hello world")
	info, err := store.Put(ctx, "test/file.txt", content, PutOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if info.Key != "test/file.txt" {
		t.Errorf("expected key 'test/file.txt', got %q", info.Key)
	}
	if info.Size != 11 {
		t.Errorf("expected size 11, got %d", info.Size)
	}

	reader, gotInfo, err := store.Get(ctx, "test/file.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer reader.Close()

	if gotInfo.Key != "test/file.txt" {
		t.Errorf("expected key 'test/file.txt', got %q", gotInfo.Key)
	}
}

func TestLocalStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.Put(ctx, "test/to-delete.txt", strings.NewReader("data"), PutOptions{})

	exists, _ := store.Exists(ctx, "test/to-delete.txt")
	if !exists {
		t.Fatal("expected file to exist after Put")
	}

	if err := store.Delete(ctx, "test/to-delete.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, _ = store.Exists(ctx, "test/to-delete.txt")
	if exists {
		t.Error("expected file to be deleted")
	}
}

func TestLocalStore_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_, _, err = store.Get(ctx, "nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if _, ok := err.(ErrNotFound); !ok {
		t.Errorf("expected ErrNotFound, got %T: %v", err, err)
	}
}

func TestLocalStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		store.Put(ctx, fmt.Sprintf("test/file%d.txt", i), strings.NewReader("data"), PutOptions{})
	}

	result, err := store.List(ctx, ListOptions{Prefix: "test/"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(result.Objects) != 3 {
		t.Errorf("expected 3 objects, got %d", len(result.Objects))
	}
}

func TestTenantStore_PrefixesKeys(t *testing.T) {
	dir := t.TempDir()
	baseStore, _ := NewLocalStore(LocalConfig{Path: dir})
	defer baseStore.Close()

	tenantGetter := func(ctx context.Context) string {
		if tenant, ok := ctx.Value(TenantKey{}).(string); ok {
			return tenant
		}
		return ""
	}

	store := NewTenantStore(baseStore, tenantGetter)

	ctx := context.WithValue(context.Background(), TenantKey{}, "acme")
	content := strings.NewReader("tenant data")
	info, err := store.Put(ctx, "uploads/doc.pdf", content, PutOptions{})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if info.Key != "acme/uploads/doc.pdf" {
		t.Errorf("expected key 'acme/uploads/doc.pdf', got %q", info.Key)
	}

	// Verify the file exists with the prefixed path
	fullPath := filepath.Join(dir, "acme/uploads/doc.pdf")
	if _, err := os.Stat(fullPath); err != nil {
		t.Errorf("expected file at %q, got error: %v", fullPath, err)
	}
}

func TestTenantStore_NoTenant_NoPrefix(t *testing.T) {
	dir := t.TempDir()
	baseStore, _ := NewLocalStore(LocalConfig{Path: dir})
	defer baseStore.Close()

	store := NewTenantStore(baseStore, nil)

	ctx := context.Background()
	info, err := store.Put(ctx, "uploads/doc.pdf", strings.NewReader("data"), PutOptions{})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if info.Key != "uploads/doc.pdf" {
		t.Errorf("expected key 'uploads/doc.pdf', got %q", info.Key)
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/foo/bar", "foo/bar"},
		{"foo//bar", "foo/bar"},
		{"foo\\\\bar", "foo/bar"},
		{"", ""},
	}

	for _, tt := range tests {
		got := normalizeKey(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestValidateKeyRejectsTraversal(t *testing.T) {
	tests := []string{
		"",
		"../escape.txt",
		"safe/../../escape.txt",
		"safe/./file.txt",
		"safe/file.txt/",
		"safe/\x00/file.txt",
	}
	for _, input := range tests {
		if err := validateKey(normalizeKey(input)); err == nil {
			t.Fatalf("expected invalid key error for %q", input)
		}
	}
}

func TestConfig_Validate(t *testing.T) {
	// Valid local config
	cfg := Config{
		Provider: ProviderLocal,
		Local:    LocalConfig{Path: t.TempDir()},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}

	// Invalid provider
	cfg2 := Config{Provider: "invalid"}
	if err := cfg2.Validate(); err == nil {
		t.Error("expected error for invalid provider")
	}

	// Missing S3 bucket
	cfg3 := Config{Provider: ProviderS3}
	if err := cfg3.Validate(); err == nil {
		t.Error("expected error for missing S3 bucket")
	}
}

func TestCredentialSource_Resolve(t *testing.T) {
	// 1. Direct value
	cs := CredentialSource{Value: "my-secret-key"}
	val, err := cs.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if val != "my-secret-key" {
		t.Errorf("expected 'my-secret-key', got %q", val)
	}

	// 2. Environment variable
	t.Setenv("TEST_STORAGE_SECRET", "env-secret-value")
	cs2 := CredentialSource{EnvVar: "TEST_STORAGE_SECRET"}
	val2, err := cs2.Resolve()
	if err != nil {
		t.Fatalf("Resolve env var: %v", err)
	}
	if val2 != "env-secret-value" {
		t.Errorf("expected 'env-secret-value', got %q", val2)
	}

	// 3. Missing env var
	cs3 := CredentialSource{EnvVar: "NONEXISTENT_VAR_XYZ"}
	_, err = cs3.Resolve()
	if err == nil {
		t.Error("expected error for missing env var")
	}

	// 4. File
	tmpFile := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(tmpFile, []byte("file-secret\n"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	cs4 := CredentialSource{File: tmpFile}
	val4, err := cs4.Resolve()
	if err != nil {
		t.Fatalf("Resolve file: %v", err)
	}
	if val4 != "file-secret" {
		t.Errorf("expected 'file-secret', got %q", val4)
	}

	// 5. Missing file
	cs5 := CredentialSource{File: "/nonexistent/path/secret"}
	_, err = cs5.Resolve()
	if err == nil {
		t.Error("expected error for missing file")
	}

	// 6. Empty file
	emptyFile := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(emptyFile, []byte(""), 0600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	cs6 := CredentialSource{File: emptyFile}
	_, err = cs6.Resolve()
	if err == nil {
		t.Error("expected error for empty file")
	}

	// 7. nil source
	var cs7 *CredentialSource
	val7, err := cs7.Resolve()
	if err != nil {
		t.Fatalf("nil Resolve: %v", err)
	}
	if val7 != "" {
		t.Errorf("expected empty string for nil, got %q", val7)
	}

	// 8. Priority: Value > EnvVar
	t.Setenv("TEST_STORAGE_SECRET2", "env-value")
	cs8 := CredentialSource{Value: "direct-value", EnvVar: "TEST_STORAGE_SECRET2"}
	val8, err := cs8.Resolve()
	if err != nil {
		t.Fatalf("priority Resolve: %v", err)
	}
	if val8 != "direct-value" {
		t.Errorf("expected 'direct-value' (priority), got %q", val8)
	}

	// 9. Priority: EnvVar > File
	cs9 := CredentialSource{EnvVar: "TEST_STORAGE_SECRET2", File: tmpFile}
	val9, err := cs9.Resolve()
	if err != nil {
		t.Fatalf("priority2 Resolve: %v", err)
	}
	if val9 != "env-value" {
		t.Errorf("expected 'env-value' (env var priority), got %q", val9)
	}

	// 10. SecretManager with env: prefix
	t.Setenv("SECRET_FROM_ENV", "secret-value")
	cs10 := CredentialSource{SecretManager: "env:SECRET_FROM_ENV"}
	val10, err := cs10.Resolve()
	if err != nil {
		t.Fatalf("SecretManager env: prefix Resolve: %v", err)
	}
	if val10 != "secret-value" {
		t.Errorf("expected 'secret-value', got %q", val10)
	}

	// 11. SecretManager without env: prefix (should error)
	cs11 := CredentialSource{SecretManager: "projects/PROJECT/secrets/SECRET"}
	_, err = cs11.Resolve()
	if err == nil {
		t.Error("expected error for SecretManager without env: prefix")
	}
}

func TestErrNotFound(t *testing.T) {
	err := ErrNotFound("test-key")
	expected := "storage: object not found: test-key"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestErrInvalidKey(t *testing.T) {
	err := ErrInvalidKey("bad-key")
	expected := "storage: invalid key: bad-key"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestVisibility(t *testing.T) {
	if Private != "private" {
		t.Errorf("Expected Private='private', got %s", Private)
	}
	if Public != "public" {
		t.Errorf("Expected Public='public', got %s", Public)
	}
}

func TestProviderType(t *testing.T) {
	if ProviderS3 != "s3" {
		t.Errorf("Expected ProviderS3='s3', got %s", ProviderS3)
	}
	if ProviderGCS != "gcs" {
		t.Errorf("Expected ProviderGCS='gcs', got %s", ProviderGCS)
	}
	if ProviderAzure != "azure" {
		t.Errorf("Expected ProviderAzure='azure', got %s", ProviderAzure)
	}
	if ProviderLocal != "local" {
		t.Errorf("Expected ProviderLocal='local', got %s", ProviderLocal)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider != ProviderLocal {
		t.Errorf("Expected default provider to be local, got %s", cfg.Provider)
	}
	if cfg.DefaultVisibility != Private {
		t.Errorf("Expected default visibility to be private, got %s", cfg.DefaultVisibility)
	}
	if cfg.Local.Path != "storage/" {
		t.Errorf("Expected default local path to be storage/, got %s", cfg.Local.Path)
	}
	if cfg.Cleanup.Enabled {
		t.Error("Expected cleanup to be disabled by default")
	}
}

func TestLocalStore_Copy(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	content := strings.NewReader("copy test")
	store.Put(ctx, "source/file.txt", content, PutOptions{})

	info, err := store.Copy(ctx, "source/file.txt", "dest/file.txt")
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if info.Key != "dest/file.txt" {
		t.Errorf("expected key 'dest/file.txt', got %q", info.Key)
	}

	// Verify the copy exists
	exists, _ := store.Exists(ctx, "dest/file.txt")
	if !exists {
		t.Error("expected destination file to exist after copy")
	}
}

func TestLocalStore_SignedURL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_, err = store.SignedURL(ctx, "test/file.txt", time.Hour, URLConfig{})
	if err == nil {
		t.Error("expected error for signed URL on local store")
	}
}

func TestLocalStore_PublicURL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	url, err := store.PublicURL(ctx, "test/file.txt", URLConfig{})
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL for local store, got %q", url)
	}
}

func TestLocalStore_ContentTypeDetection(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	t.Run("explicit content type", func(t *testing.T) {
		content := strings.NewReader("test")
		info, _ := store.Put(ctx, "test.txt", content, PutOptions{ContentType: "text/plain"})
		if info.ContentType != "text/plain" {
			t.Errorf("expected explicit content type, got %s", info.ContentType)
		}
	})

	t.Run("auto-detected content type", func(t *testing.T) {
		content := strings.NewReader("test")
		info, _ := store.Put(ctx, "test.png", content, PutOptions{})
		if info.ContentType != "image/png" {
			t.Errorf("expected auto-detected image/png, got %s", info.ContentType)
		}
	})

	t.Run("unknown extension defaults to octet-stream", func(t *testing.T) {
		content := strings.NewReader("test")
		info, _ := store.Put(ctx, "test.unknown", content, PutOptions{})
		if info.ContentType != "application/octet-stream" {
			t.Errorf("expected application/octet-stream, got %s", info.ContentType)
		}
	})
}

func TestLocalStore_Metadata(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	content := strings.NewReader("test")
	metadata := map[string]string{"key1": "value1", "key2": "value2"}

	info, _ := store.Put(ctx, "test.txt", content, PutOptions{Metadata: metadata})
	if len(info.Metadata) != 2 {
		t.Errorf("expected 2 metadata entries, got %d", len(info.Metadata))
	}
}

func TestLocalStore_InvalidKey(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	content := strings.NewReader("test")

	_, err = store.Put(ctx, "../escape.txt", content, PutOptions{})
	if err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, ok := err.(ErrInvalidKey); !ok {
		t.Fatalf("expected ErrInvalidKey, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "..", "escape.txt")); statErr == nil {
		t.Fatal("path traversal created a file outside the storage root")
	}
}

func TestLocalStore_InvalidKeyAcrossOperations(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, _, err := store.Get(ctx, "../escape.txt"); err == nil {
		t.Fatal("Get should reject traversal")
	}
	if err := store.Delete(ctx, "../escape.txt"); err == nil {
		t.Fatal("Delete should reject traversal")
	}
	if _, err := store.Exists(ctx, "../escape.txt"); err == nil {
		t.Fatal("Exists should reject traversal")
	}
	if _, err := store.List(ctx, ListOptions{Prefix: "../"}); err == nil {
		t.Fatal("List should reject traversal")
	}
	if _, err := store.Copy(ctx, "source.txt", "../escape.txt"); err == nil {
		t.Fatal("Copy should reject traversal")
	}
}

func TestProviderMethodsValidateKeysBeforeClients(t *testing.T) {
	ctx := context.Background()

	s3 := &S3Store{}
	if _, err := s3.Put(ctx, "../escape.txt", strings.NewReader("x"), PutOptions{}); err == nil {
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
	if _, err := s3.List(ctx, ListOptions{Prefix: "../"}); err == nil {
		t.Fatal("S3 List should reject invalid prefixes before using client")
	}
	if _, err := s3.PublicURL(ctx, "../escape.txt", URLConfig{}); err == nil {
		t.Fatal("S3 PublicURL should reject invalid keys before using client")
	}
	if _, err := s3.SignedURL(ctx, "../escape.txt", time.Hour, URLConfig{}); err == nil {
		t.Fatal("S3 SignedURL should reject invalid keys before using client")
	}
	if _, err := s3.Copy(ctx, "source.txt", "../escape.txt"); err == nil {
		t.Fatal("S3 Copy should reject invalid keys before using client")
	}

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

func TestS3Helpers(t *testing.T) {
	store := &S3Store{bucket: "private-bucket", publicBucket: "public-bucket"}

	if got := store.resolveBucket(PutOptions{}); got != "private-bucket" {
		t.Fatalf("private bucket = %q", got)
	}
	if got := store.resolveBucket(PutOptions{Visibility: Public}); got != "public-bucket" {
		t.Fatalf("public bucket = %q", got)
	}
	if got := store.detectContentType("image.png", PutOptions{}); got != "image/png" {
		t.Fatalf("detected content type = %q", got)
	}
	if got := store.detectContentType("file.unknown", PutOptions{}); got != "application/octet-stream" {
		t.Fatalf("fallback content type = %q", got)
	}
	if got := store.detectContentType("file.bin", PutOptions{ContentType: "application/custom"}); got != "application/custom" {
		t.Fatalf("explicit content type = %q", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.PublicURLBase(context.Background(), URLConfig{ContentType: "https://cdn.example.com"}); got != "https://cdn.example.com" {
		t.Fatalf("PublicURLBase = %q", got)
	}
	if isS3NotFound(nil) {
		t.Fatal("nil should not be treated as not found")
	}
}

// TestIsS3NotFound_TypedSDKErrors pins issue #227: a real S3 endpoint answers
// a missing key with the message "The specified key does not exist." — the
// NoSuchKey code travels only in the typed response struct (HTTP 404). The
// classifier must read the SDK type, never the error text.
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

// TestIsGCSNotFound_WrappedSentinel pins the GCS half of issue #227: the
// SDK sentinel must be detected with errors.Is, so it still matches when a
// client layer returns it wrapped.
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
	if url, err := gcs.PublicURL(ctx, "safe/file.txt", URLConfig{}); err != nil || url != "" {
		t.Fatalf("GCS PublicURL without public bucket = %q, %v", url, err)
	}
	if _, err := gcs.PublicURL(ctx, "../escape.txt", URLConfig{}); err == nil {
		t.Fatal("GCS PublicURL should validate keys")
	}
	if got := escapeURLPath("folder/a b.txt"); got != "folder/a%20b.txt" {
		t.Fatalf("escapeURLPath = %q", got)
	}

	azure := &azureStore{}
	if url, err := azure.PublicURL(ctx, "safe/file.txt", URLConfig{}); err != nil || url != "" {
		t.Fatalf("Azure PublicURL without public container = %q, %v", url, err)
	}
	if _, err := azure.PublicURL(ctx, "../escape.txt", URLConfig{}); err == nil {
		t.Fatal("Azure PublicURL should validate keys")
	}
}

func TestTenantStore_Unwrap(t *testing.T) {
	dir := t.TempDir()
	baseStore, _ := NewLocalStore(LocalConfig{Path: dir})
	defer baseStore.Close()

	tenantGetter := func(ctx context.Context) string {
		return "tenant"
	}

	store := NewTenantStore(baseStore, tenantGetter)

	unwrapped := store.Unwrap()
	if unwrapped != baseStore {
		t.Error("expected unwrapped store to be the base store")
	}
}

func TestTenantStore_UnwrapIfCleaner(t *testing.T) {
	dir := t.TempDir()
	baseStore, _ := NewLocalStore(LocalConfig{Path: dir})
	defer baseStore.Close()

	store := NewTenantStore(baseStore, nil)

	unwrapped := store.UnwrapIfCleaner()
	if unwrapped != baseStore {
		t.Error("expected unwrapped store to be the base store")
	}
}

func TestTenantStore_TenantPrefixOverride(t *testing.T) {
	dir := t.TempDir()
	baseStore, _ := NewLocalStore(LocalConfig{Path: dir})
	defer baseStore.Close()

	tenantGetter := func(ctx context.Context) string {
		return "default-tenant"
	}

	store := NewTenantStore(baseStore, tenantGetter)

	ctx := context.Background()
	content := strings.NewReader("data")

	// With explicit tenant prefix override
	info, err := store.Put(ctx, "uploads/file.txt", content, PutOptions{TenantPrefix: "custom-tenant"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if info.Key != "custom-tenant/uploads/file.txt" {
		t.Errorf("expected custom-tenant prefix, got %s", info.Key)
	}
}

func TestTenantStore_OperationsUseTenantPrefix(t *testing.T) {
	dir := t.TempDir()
	baseStore, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer baseStore.Close()

	store := NewTenantStore(baseStore, func(ctx context.Context) string {
		if tenant, _ := ctx.Value(TenantKey{}).(string); tenant != "" {
			return tenant
		}
		return ""
	})
	ctx := context.WithValue(context.Background(), TenantKey{}, "tenant-a")

	if _, err := store.Put(ctx, "docs/source.txt", strings.NewReader("tenant data"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	reader, info, err := store.Get(ctx, "docs/source.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != "tenant data" || info.Key != "tenant-a/docs/source.txt" {
		t.Fatalf("Get returned body=%q info=%+v", string(body), info)
	}
	if exists, err := store.Exists(ctx, "docs/source.txt"); err != nil || !exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
	list, err := store.List(ctx, ListOptions{Prefix: "docs"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Objects) != 1 || list.Objects[0].Key != "tenant-a/docs/source.txt" {
		t.Fatalf("List returned %+v", list.Objects)
	}
	if _, err := store.Copy(ctx, "docs/source.txt", "docs/copy.txt"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if _, err := store.PublicURL(ctx, "docs/source.txt", URLConfig{}); err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	if _, err := store.SignedURL(ctx, "docs/source.txt", time.Hour, URLConfig{}); err == nil {
		t.Fatal("SignedURL should surface local store unsupported error")
	}
	if err := store.Delete(ctx, "docs/source.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists, err := baseStore.Exists(ctx, "tenant-a/docs/source.txt"); err != nil || exists {
		t.Fatalf("base Exists after tenant delete = %v, %v", exists, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestPublicMapper(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewLocalStore(LocalConfig{Path: dir})
	defer store.Close()

	publicPaths := map[string]string{
		"/media": "storage/public/media",
		"/files": "storage/public/files",
	}

	mapper := NewPublicMapper(store, publicPaths, "https://cdn.example.com")

	t.Run("PublicURL for public key", func(t *testing.T) {
		ctx := context.Background()
		url, err := mapper.PublicURL(ctx, "storage/public/media/image.png", URLConfig{})
		if err != nil {
			t.Fatalf("PublicURL: %v", err)
		}
		expected := "https://cdn.example.com/media/image.png"
		if url != expected {
			t.Errorf("expected %q, got %q", expected, url)
		}
	})

	t.Run("PublicURL for private key", func(t *testing.T) {
		ctx := context.Background()
		url, err := mapper.PublicURL(ctx, "storage/private/file.txt", URLConfig{})
		// Local store doesn't support signed URLs, so should return error
		if err == nil {
			t.Error("expected error for private key on local store")
		}
		if url != "" {
			t.Errorf("expected empty URL for private key, got %q", url)
		}
	})

	t.Run("IsPublicKey", func(t *testing.T) {
		if !mapper.IsPublicKey("storage/public/media/test.png") {
			t.Error("expected public key to be recognized as public")
		}
		if mapper.IsPublicKey("storage/private/file.txt") {
			t.Error("expected private key to not be recognized as public")
		}
	})
}

func TestPublicMapper_NilStore(t *testing.T) {
	mapper := NewPublicMapper(nil, map[string]string{"/media": "public"}, "https://cdn.example.com")

	ctx := context.Background()
	url, err := mapper.PublicURL(ctx, "public/file.txt", URLConfig{})
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	// With nil store, it still generates URLs based on public paths mapping
	expected := "https://cdn.example.com/media/file.txt"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestPublicMapper_MountAndMountAll(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, err := store.Put(ctx, "storage/public/media/image.txt", strings.NewReader("hello"), PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	mux := router.NewMux()
	mapper := NewPublicMapper(store, map[string]string{"/media": "storage/public/media"}, "https://cdn.example.com")
	mapper.MountAll(mux)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/media/image.txt")
	if err != nil {
		t.Fatalf("GET mounted public object: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("mounted response status=%d body=%q", res.StatusCode, string(body))
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("missing nosniff header: %q", got)
	}
}

func TestFactoryAndCleanupHelpers(t *testing.T) {
	dir := t.TempDir()
	store, err := New(Config{
		Provider: ProviderLocal,
		Local:    LocalConfig{Path: dir},
	}, nil)
	if err != nil {
		t.Fatalf("New local store: %v", err)
	}
	defer store.Close()

	wrapped := NewWithTenant(store, func(context.Context) string { return "tenant" })
	if wrapped.Unwrap() != store {
		t.Fatal("NewWithTenant did not wrap the provided store")
	}
	mapper := NewPublicMapperForConfig(store, Config{
		PublicPaths:   map[string]string{"/media": "public"},
		PublicURLBase: "https://cdn.example.com/",
	})
	if url, err := mapper.PublicURL(context.Background(), "public/file.txt", URLConfig{}); err != nil || url != "https://cdn.example.com/media/file.txt" {
		t.Fatalf("NewPublicMapperForConfig URL = %q, %v", url, err)
	}
	if _, err := New(Config{Provider: "bogus"}, nil); err == nil {
		t.Fatal("New should reject invalid config")
	}

	cleaner, err := NewCleaner(store, CleanupConfig{Enabled: false, MaxAge: "bad", Interval: "bad"}, nil)
	if err != nil {
		t.Fatalf("NewCleaner: %v", err)
	}
	if cleaner.maxAge != 24*time.Hour || cleaner.interval != time.Hour || cleaner.prefix != "_tmp/" {
		t.Fatalf("unexpected cleaner defaults: maxAge=%s interval=%s prefix=%q", cleaner.maxAge, cleaner.interval, cleaner.prefix)
	}
	cleaner.Stop()

	tempKey := CleanupTempKey("upload")
	if !strings.HasPrefix(tempKey, "_tmp/upload_") {
		t.Fatalf("CleanupTempKey = %q", tempKey)
	}
	if !IsTempKey("/_tmp/upload_20200101000000") || IsTempKey("uploads/file.txt") {
		t.Fatal("IsTempKey returned unexpected result")
	}
}

func TestCleanerRunCleanupDeletesExpiredObjects(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, err := store.Put(ctx, "_tmp/old.txt", strings.NewReader("old"), PutOptions{}); err != nil {
		t.Fatalf("Put old: %v", err)
	}
	if _, err := store.Put(ctx, "_tmp/fresh.txt", strings.NewReader("fresh"), PutOptions{}); err != nil {
		t.Fatalf("Put fresh: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "_tmp", "old.txt"), oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes old: %v", err)
	}

	cleaner, err := NewCleaner(store, CleanupConfig{Enabled: false, Prefix: "_tmp/", MaxAge: "24h", Interval: "1h"}, nil)
	if err != nil {
		t.Fatalf("NewCleaner: %v", err)
	}
	cleaner.runCleanup()

	if exists, err := store.Exists(ctx, "_tmp/old.txt"); err != nil || exists {
		t.Fatalf("old temp exists=%v err=%v", exists, err)
	}
	if exists, err := store.Exists(ctx, "_tmp/fresh.txt"); err != nil || !exists {
		t.Fatalf("fresh temp exists=%v err=%v", exists, err)
	}
}

func TestConfig_Validate_S3(t *testing.T) {
	t.Run("valid S3 config", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderS3,
			S3: S3Config{
				Bucket:          "test-bucket",
				AccessKeyID:     CredentialSource{Value: "key"},
				SecretAccessKey: CredentialSource{Value: "secret"},
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid S3 config, got error: %v", err)
		}
	})

	t.Run("S3 missing bucket", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderS3,
			S3:       S3Config{},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing S3 bucket")
		}
	})

	t.Run("S3 with MinIO endpoint", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderS3,
			S3: S3Config{
				Bucket:   "test-bucket",
				Endpoint: "http://minio:9000",
			},
		}
		// MinIO endpoint doesn't require credentials in validation
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid S3 config with MinIO endpoint, got error: %v", err)
		}
	})
}

func TestConfig_Validate_GCS(t *testing.T) {
	t.Run("valid GCS config", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderGCS,
			GCS: GCSConfig{
				Bucket: "test-bucket",
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid GCS config, got error: %v", err)
		}
	})

	t.Run("GCS missing bucket", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderGCS,
			GCS:      GCSConfig{},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing GCS bucket")
		}
	})
}

func TestConfig_Validate_Azure(t *testing.T) {
	t.Run("valid Azure config", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderAzure,
			Azure: AzureConfig{
				AccountName: CredentialSource{Value: "account"},
				Container:   "test-container",
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid Azure config, got error: %v", err)
		}
	})

	t.Run("Azure missing account name", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderAzure,
			Azure: AzureConfig{
				Container: "test-container",
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing Azure account name")
		}
	})

	t.Run("Azure missing container", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderAzure,
			Azure: AzureConfig{
				AccountName: CredentialSource{Value: "account"},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing Azure container")
		}
	})
}

func TestConfig_Validate_Local(t *testing.T) {
	t.Run("valid local config", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderLocal,
			Local:    LocalConfig{Path: t.TempDir()},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid local config, got error: %v", err)
		}
	})

	t.Run("local missing path", func(t *testing.T) {
		cfg := Config{
			Provider: ProviderLocal,
			Local:    LocalConfig{},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing local path")
		}
	})
}

func TestConfig_Validate_NilConfig(t *testing.T) {
	var cfg *Config
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for nil config")
	}
}

func TestNewLocalStore_DefaultPath(t *testing.T) {
	store, err := NewLocalStore(LocalConfig{})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer os.RemoveAll(store.root)

	if store.root == "" {
		t.Error("expected non-empty root path")
	}
}
