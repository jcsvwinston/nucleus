package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Store implements Store using S3-compatible backends.
// Works with AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces, etc.
type S3Store struct {
	client       *minio.Client
	bucket       string
	publicBucket string
}

// NewS3Store creates an S3-compatible storage backend.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	// Resolve credentials
	accessKeyID, err := cfg.AccessKeyID.Resolve()
	if err != nil {
		return nil, fmt.Errorf("storage: resolve S3 access key: %w", err)
	}
	secretAccessKey, err := cfg.SecretAccessKey.Resolve()
	if err != nil {
		return nil, fmt.Errorf("storage: resolve S3 secret key: %w", err)
	}
	sessionToken, err := cfg.SessionToken.Resolve()
	if err != nil {
		return nil, fmt.Errorf("storage: resolve S3 session token: %w", err)
	}

	// Determine if we're using SSL
	useSSL := true
	endpoint := cfg.Endpoint
	if strings.HasPrefix(endpoint, "http://") {
		useSSL = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}

	// Empty endpoint = AWS S3
	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
		if cfg.Region != "" {
			endpoint = fmt.Sprintf("s3.%s.amazonaws.com", cfg.Region)
		}
	}

	// Credentials
	var creds *credentials.Credentials
	if accessKeyID != "" {
		creds = credentials.NewStaticV4(accessKeyID, secretAccessKey, sessionToken)
	} else {
		// Fallback to IAM role / environment / shared config
		creds = credentials.NewIAM("")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  creds,
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create S3 client: %w", err)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.BucketExists(ctx, cfg.Bucket); err != nil {
		return nil, fmt.Errorf("storage: S3 bucket %q not accessible: %w", cfg.Bucket, err)
	}

	if cfg.PublicBucket != "" {
		if _, err := client.BucketExists(ctx, cfg.PublicBucket); err != nil {
			return nil, fmt.Errorf("storage: S3 public bucket %q not accessible: %w", cfg.PublicBucket, err)
		}
	}

	return &S3Store{
		client:       client,
		bucket:       cfg.Bucket,
		publicBucket: cfg.PublicBucket,
	}, nil
}

func (s *S3Store) resolveBucket(opts PutOptions) string {
	if opts.Visibility == Public && s.publicBucket != "" {
		return s.publicBucket
	}
	return s.bucket
}

func (s *S3Store) detectContentType(key string, opts PutOptions) string {
	if opts.ContentType != "" {
		return opts.ContentType
	}
	ext := ""
	if idx := strings.LastIndex(key, "."); idx >= 0 && idx < len(key)-1 {
		ext = key[idx:]
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func (s *S3Store) Put(ctx context.Context, key string, reader io.Reader, opts PutOptions) (ObjectInfo, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return ObjectInfo{}, err
	}

	contentType := s.detectContentType(key, opts)

	info, err := s.client.PutObject(ctx, s.resolveBucket(opts), key, reader, -1, minio.PutObjectOptions{
		ContentType:  contentType,
		UserMetadata: opts.Metadata,
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage: S3 Put %q: %w", key, err)
	}

	return ObjectInfo{
		Key:         key,
		Size:        info.Size,
		ContentType: contentType,
		Visibility:  opts.Visibility,
		Metadata:    opts.Metadata,
		UpdatedAt:   info.LastModified,
	}, nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}

	bucket := s.bucket
	// Check public bucket first for public objects
	if s.publicBucket != "" {
		obj, err := s.client.StatObject(ctx, s.publicBucket, key, minio.StatObjectOptions{})
		if err == nil {
			reader, err := s.client.GetObject(ctx, s.publicBucket, key, minio.GetObjectOptions{})
			if err != nil {
				return nil, ObjectInfo{}, fmt.Errorf("storage: S3 Get %q: %w", key, err)
			}
			return reader, ObjectInfo{
				Key:         key,
				Size:        obj.Size,
				ContentType: obj.ContentType,
				Visibility:  Public,
				Metadata:    obj.UserMetadata,
				UpdatedAt:   obj.LastModified,
			}, nil
		}
	}

	// Fall back to private bucket
	obj, err := s.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ObjectInfo{}, ErrNotFound(key)
		}
		return nil, ObjectInfo{}, fmt.Errorf("storage: S3 Get %q: %w", key, err)
	}

	reader, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("storage: S3 Get %q: %w", key, err)
	}

	return reader, ObjectInfo{
		Key:         key,
		Size:        obj.Size,
		ContentType: obj.ContentType,
		Visibility:  Private,
		Metadata:    obj.UserMetadata,
		UpdatedAt:   obj.LastModified,
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return err
	}

	// Try both buckets
	for _, bucket := range []string{s.bucket, s.publicBucket} {
		if bucket == "" {
			continue
		}
		err := s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
		if err == nil {
			return nil
		}
		if isS3NotFound(err) {
			continue
		}
		return fmt.Errorf("storage: S3 Delete %q: %w", key, err)
	}
	return nil
}

func (s *S3Store) Exists(ctx context.Context, key string) (bool, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return false, err
	}

	for _, bucket := range []string{s.bucket, s.publicBucket} {
		if bucket == "" {
			continue
		}
		_, err := s.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
		if err == nil {
			return true, nil
		}
		if isS3NotFound(err) {
			continue
		}
		return false, fmt.Errorf("storage: S3 Exists %q: %w", key, err)
	}
	return false, nil
}

func (s *S3Store) List(ctx context.Context, opts ListOptions) (ListResult, error) {
	opts.Prefix = normalizeKey(opts.Prefix)
	opts.Marker = normalizeKey(opts.Marker)
	if err := validateKeyPrefix(opts.Prefix); err != nil {
		return ListResult{}, err
	}
	if opts.Marker != "" {
		if err := validateKey(opts.Marker); err != nil {
			return ListResult{}, err
		}
	}

	doneCh := make(chan struct{})
	defer close(doneCh)

	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	result := ListResult{}
	objectsChecked := 0

	// List from both buckets
	for _, bucket := range []string{s.bucket, s.publicBucket} {
		if bucket == "" {
			continue
		}
		for objInfo := range s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
			Prefix:     opts.Prefix,
			Recursive:  opts.Delimiter == "",
			StartAfter: opts.Marker,
		}) {
			if objInfo.Err != nil {
				return result, fmt.Errorf("storage: S3 List %q: %w", opts.Prefix, objInfo.Err)
			}
			if objectsChecked >= limit {
				result.Truncated = true
				result.NextMarker = objInfo.Key
				return result, nil
			}
			result.Objects = append(result.Objects, ObjectInfo{
				Key:         objInfo.Key,
				Size:        objInfo.Size,
				ContentType: objInfo.ContentType,
				UpdatedAt:   objInfo.LastModified,
			})
			objectsChecked++
		}
	}

	return result, nil
}

func (s *S3Store) PublicURL(ctx context.Context, key string, opts URLConfig) (string, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return "", err
	}

	// PublicURL only works for public objects in the public bucket.
	// If no public bucket is configured, return empty.
	if s.publicBucket == "" {
		return "", nil
	}

	// Check if object exists in public bucket
	_, err := s.client.StatObject(ctx, s.publicBucket, key, minio.StatObjectOptions{})
	if err != nil {
		return "", nil // Not found in public bucket = not publicly accessible
	}

	// Return empty — the actual URL is constructed by the public path mapper
	// in pkg/storage/public.go using Config.PublicURLBase
	return "", nil
}

func (s *S3Store) SignedURL(ctx context.Context, key string, expires time.Duration, opts URLConfig) (string, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return "", err
	}

	bucket := s.bucket
	if s.publicBucket != "" {
		// Check if object exists in public bucket
		_, err := s.client.StatObject(ctx, s.publicBucket, key, minio.StatObjectOptions{})
		if err == nil {
			bucket = s.publicBucket
		}
	}

	reqParams := make(url.Values)
	if opts.ContentType != "" {
		reqParams.Set("response-content-type", opts.ContentType)
	}
	if opts.Disposition != "" {
		reqParams.Set("response-content-disposition", opts.Disposition)
	}

	urlVal, err := s.client.PresignedGetObject(ctx, bucket, key, expires, reqParams)
	if err != nil {
		return "", fmt.Errorf("storage: S3 SignedURL %q: %w", key, err)
	}

	return urlVal.String(), nil
}

func (s *S3Store) Copy(ctx context.Context, srcKey, dstKey string) (ObjectInfo, error) {
	srcKey = normalizeKey(srcKey)
	dstKey = normalizeKey(dstKey)
	if err := validateKey(srcKey); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateKey(dstKey); err != nil {
		return ObjectInfo{}, err
	}

	src := minio.CopySrcOptions{
		Bucket: s.bucket,
		Object: srcKey,
	}
	dst := minio.CopyDestOptions{
		Bucket: s.bucket,
		Object: dstKey,
	}

	info, err := s.client.CopyObject(ctx, dst, src)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage: S3 Copy %q -> %q: %w", srcKey, dstKey, err)
	}

	return ObjectInfo{
		Key:       dstKey,
		Size:      info.Size,
		UpdatedAt: info.LastModified,
	}, nil
}

func (s *S3Store) Close() error {
	return nil
}

func (s *S3Store) PublicURLBase(ctx context.Context, opts URLConfig) string {
	// Return the configured public URL base
	return opts.ContentType // This will be overridden by config
}

// isS3NotFound reports whether err is the S3 API's answer for a missing
// object or bucket. It inspects the SDK's typed minio.ErrorResponse — the
// error code ("NoSuchKey"/"NoSuchBucket") and the HTTP status — never the
// error text: a real S3 endpoint puts "The specified key does not exist."
// in the message and carries the code only in the response struct, so any
// text-based match misses it (issue #227).
func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var resp minio.ErrorResponse
	if !errors.As(err, &resp) {
		return false
	}
	return resp.Code == minio.NoSuchKey ||
		resp.Code == minio.NoSuchBucket ||
		resp.StatusCode == http.StatusNotFound
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "\\", "/")
	key = strings.TrimLeft(key, "/")
	// Collapse multiple slashes
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}
	if key == "." {
		return ""
	}
	return key
}

func validateKey(key string) error {
	if key == "" {
		return ErrInvalidKey("empty key")
	}
	if strings.ContainsRune(key, '\x00') {
		return ErrInvalidKey("contains NUL byte")
	}
	if strings.Contains(key, "//") {
		return ErrInvalidKey("contains double slash")
	}
	if path.IsAbs(key) {
		return ErrInvalidKey("absolute paths are not allowed")
	}
	if path.Clean(key) != key {
		return ErrInvalidKey("contains non-canonical path segments")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." || segment == ".." {
			return ErrInvalidKey("path traversal is not allowed")
		}
	}
	return nil
}

func validateKeyPrefix(prefix string) error {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return nil
	}
	return validateKey(prefix)
}
