package gcs

import (
	"context"
	"errors"
	"fmt"
	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
	"io"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// gcsStore implements the provider.Store interface using Google Cloud Storage.
type gcsStore struct {
	client       *storage.Client
	bucket       string
	publicBucket string
}

// NewGCSStore creates a GCS client using the provided configuration.
// If cfg.CredentialsSource is configured, it resolves the credentials
// and uses them to authenticate. If empty, uses Application Default Credentials (ADC).
func NewGCSStore(cfg provider.GCSConfig) (provider.Store, error) {
	ctx := context.Background()

	var opts []option.ClientOption

	// Resolve credentials if configured
	credSource := cfg.CredentialsSource
	if credSource.Value != "" || credSource.EnvVar != "" || credSource.File != "" {
		creds, err := credSource.Resolve()
		if err != nil {
			return nil, fmt.Errorf("storage: gcs resolve credentials: %w", err)
		}
		if creds != "" {
			// Determine if this is a file path (JSON key file) or inline JSON
			if _, err := os.Stat(creds); err == nil {
				// It's a file path
				opts = append(opts, option.WithCredentialsFile(creds))
			} else {
				// Treat as inline JSON credentials
				opts = append(opts, option.WithCredentialsJSON([]byte(creds)))
			}
		}
	}
	// If no credentials configured, ADC is used automatically by the GCS client.

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: gcs create client: %w", err)
	}

	return &gcsStore{
		client:       client,
		bucket:       cfg.Bucket,
		publicBucket: cfg.PublicBucket,
	}, nil
}

// Put uploads a file from an io.Reader to GCS.
func (s *gcsStore) Put(ctx context.Context, key string, reader io.Reader, opts provider.PutOptions) (provider.ObjectInfo, error) {
	key = provider.NormalizeKey(key)
	if err := provider.ValidateKey(key); err != nil {
		return provider.ObjectInfo{}, err
	}
	bucketName := s.bucket
	if opts.Visibility == provider.Public && s.publicBucket != "" {
		bucketName = s.publicBucket
	}

	obj := s.client.Bucket(bucketName).Object(key)
	w := obj.NewWriter(ctx)

	if opts.ContentType != "" {
		w.ContentType = opts.ContentType
	}
	if len(opts.Metadata) > 0 {
		w.Metadata = opts.Metadata
	}

	if _, err := io.Copy(w, reader); err != nil {
		w.Close()
		return provider.ObjectInfo{}, fmt.Errorf("storage: gcs put %q: %w", key, err)
	}

	if err := w.Close(); err != nil {
		return provider.ObjectInfo{}, fmt.Errorf("storage: gcs put %q close writer: %w", key, err)
	}

	attrs := w.Attrs()
	return provider.ObjectInfo{
		Key:         attrs.Name,
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		Visibility:  opts.Visibility,
		Metadata:    attrs.Metadata,
		UpdatedAt:   attrs.Updated,
	}, nil
}

// Get retrieves a file by key from GCS.
func (s *gcsStore) Get(ctx context.Context, key string) (io.ReadCloser, provider.ObjectInfo, error) {
	key = provider.NormalizeKey(key)
	if err := provider.ValidateKey(key); err != nil {
		return nil, provider.ObjectInfo{}, err
	}

	var lastErr error
	for _, target := range s.lookupBuckets() {
		obj := s.client.Bucket(target.name).Object(key)
		attrs, err := obj.Attrs(ctx)
		if err != nil {
			if isGCSNotFound(err) {
				continue
			}
			return nil, provider.ObjectInfo{}, fmt.Errorf("storage: gcs get %q attrs: %w", key, err)
		}

		r, err := obj.NewReader(ctx)
		if err != nil {
			if isGCSNotFound(err) {
				lastErr = err
				continue
			}
			return nil, provider.ObjectInfo{}, fmt.Errorf("storage: gcs get %q new reader: %w", key, err)
		}

		return r, provider.ObjectInfo{
			Key:         attrs.Name,
			Size:        attrs.Size,
			ContentType: attrs.ContentType,
			Visibility:  target.visibility,
			Metadata:    attrs.Metadata,
			UpdatedAt:   attrs.Updated,
		}, nil
	}
	_ = lastErr
	return nil, provider.ObjectInfo{}, provider.ErrNotFound(key)
}

// Delete removes an object by key from GCS. Idempotent: no error if key doesn't exist.
func (s *gcsStore) Delete(ctx context.Context, key string) error {
	key = provider.NormalizeKey(key)
	if err := provider.ValidateKey(key); err != nil {
		return err
	}
	for _, target := range s.lookupBuckets() {
		obj := s.client.Bucket(target.name).Object(key)
		if err := obj.Delete(ctx); err != nil {
			if isGCSNotFound(err) {
				continue
			}
			return fmt.Errorf("storage: gcs delete %q: %w", key, err)
		}
	}
	return nil
}

// Exists checks if a key exists in GCS.
func (s *gcsStore) Exists(ctx context.Context, key string) (bool, error) {
	key = provider.NormalizeKey(key)
	if err := provider.ValidateKey(key); err != nil {
		return false, err
	}
	for _, target := range s.lookupBuckets() {
		obj := s.client.Bucket(target.name).Object(key)
		_, err := obj.Attrs(ctx)
		if err == nil {
			return true, nil
		}
		if !isGCSNotFound(err) {
			return false, fmt.Errorf("storage: gcs exists %q: %w", key, err)
		}
	}
	return false, nil
}

// List returns objects with the given prefix from GCS.
func (s *gcsStore) List(ctx context.Context, opts provider.ListOptions) (provider.ListResult, error) {
	opts.Prefix = provider.NormalizeKey(opts.Prefix)
	opts.Marker = provider.NormalizeKey(opts.Marker)
	if err := provider.ValidateKeyPrefix(opts.Prefix); err != nil {
		return provider.ListResult{}, err
	}
	if opts.Marker != "" {
		if err := provider.ValidateKey(opts.Marker); err != nil {
			return provider.ListResult{}, err
		}
	}
	query := &storage.Query{
		Prefix:    opts.Prefix,
		Delimiter: opts.Delimiter,
	}
	if opts.Marker != "" {
		query.StartOffset = opts.Marker
	}

	it := s.client.Bucket(s.bucket).Objects(ctx, query)

	var result provider.ListResult
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return result, fmt.Errorf("storage: gcs list: %w", err)
		}

		if attrs.Prefix != "" {
			// This is a common prefix (directory-like entry)
			result.CommonPrefixes = append(result.CommonPrefixes, attrs.Prefix)
		} else {
			result.Objects = append(result.Objects, provider.ObjectInfo{
				Key:         attrs.Name,
				Size:        attrs.Size,
				ContentType: attrs.ContentType,
				Visibility:  provider.Private,
				Metadata:    attrs.Metadata,
				UpdatedAt:   attrs.Updated,
			})
		}

		if opts.Limit > 0 && len(result.Objects) >= opts.Limit {
			result.Truncated = true
			if len(result.Objects) > 0 {
				result.NextMarker = result.Objects[len(result.Objects)-1].Key
			}
			break
		}
	}

	return result, nil
}

// PublicURL returns a publicly accessible URL for a key.
// If cfg.PublicBucket is set and the object is in that bucket, returns
// the direct GCS URL. Otherwise returns empty string.
func (s *gcsStore) PublicURL(ctx context.Context, key string, opts provider.URLConfig) (string, error) {
	key = provider.NormalizeKey(key)
	if err := provider.ValidateKey(key); err != nil {
		return "", err
	}
	if s.publicBucket == "" {
		return "", nil
	}
	// Check if the object exists in the public bucket
	obj := s.client.Bucket(s.publicBucket).Object(key)
	_, err := obj.Attrs(ctx)
	if err != nil {
		if isGCSNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("storage: gcs public url %q: %w", key, err)
	}

	escapedKey := provider.EscapeURLPath(key)
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.publicBucket, escapedKey), nil
}

// SignedURL returns a time-limited URL for accessing a private object.
// Uses 24h expiry by default if expires is zero.
func (s *gcsStore) SignedURL(ctx context.Context, key string, expires time.Duration, opts provider.URLConfig) (string, error) {
	key = provider.NormalizeKey(key)
	if err := provider.ValidateKey(key); err != nil {
		return "", err
	}
	if expires <= 0 {
		expires = 24 * time.Hour
	}

	bucketName := s.bucket
	// Check if object might be in public bucket
	if s.publicBucket != "" {
		obj := s.client.Bucket(s.publicBucket).Object(key)
		if _, err := obj.Attrs(ctx); err == nil {
			bucketName = s.publicBucket
		}
	}

	urlOpts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "GET",
		Expires: time.Now().Add(expires),
	}

	if opts.ContentType != "" {
		urlOpts.ContentType = opts.ContentType
	}

	signedURL, err := storage.SignedURL(bucketName, key, urlOpts)
	if err != nil {
		return "", fmt.Errorf("storage: gcs signed url %q: %w", key, err)
	}

	return signedURL, nil
}

// Copy copies an object from srcKey to dstKey within the same bucket.
func (s *gcsStore) Copy(ctx context.Context, srcKey, dstKey string) (provider.ObjectInfo, error) {
	srcKey = provider.NormalizeKey(srcKey)
	dstKey = provider.NormalizeKey(dstKey)
	if err := provider.ValidateKey(srcKey); err != nil {
		return provider.ObjectInfo{}, err
	}
	if err := provider.ValidateKey(dstKey); err != nil {
		return provider.ObjectInfo{}, err
	}

	bucketName := s.bucket
	visibility := provider.Private
	if s.publicBucket != "" {
		if _, err := s.client.Bucket(s.publicBucket).Object(srcKey).Attrs(ctx); err == nil {
			bucketName = s.publicBucket
			visibility = provider.Public
		} else if !isGCSNotFound(err) {
			return provider.ObjectInfo{}, fmt.Errorf("storage: gcs copy %q attrs: %w", srcKey, err)
		}
	}
	src := s.client.Bucket(bucketName).Object(srcKey)
	dst := s.client.Bucket(bucketName).Object(dstKey)

	copier := dst.CopierFrom(src)
	attrs, err := copier.Run(ctx)
	if err != nil {
		if isGCSNotFound(err) {
			return provider.ObjectInfo{}, provider.ErrNotFound(srcKey)
		}
		return provider.ObjectInfo{}, fmt.Errorf("storage: gcs copy %q to %q: %w", srcKey, dstKey, err)
	}

	return provider.ObjectInfo{
		Key:         attrs.Name,
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		Visibility:  visibility,
		Metadata:    attrs.Metadata,
		UpdatedAt:   attrs.Updated,
	}, nil
}

// Close releases the GCS client resources.
func (s *gcsStore) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("storage: gcs close client: %w", err)
	}
	return nil
}

// isGCSNotFound reports whether err means the object does not exist in GCS.
// It uses errors.Is against the SDK sentinel instead of ==: the GCS client
// may return the sentinel wrapped, and a == comparison silently misses it
// (same class of bug as the S3 text match, issue #227).
func isGCSNotFound(err error) bool {
	return errors.Is(err, storage.ErrObjectNotExist)
}

// Ensure gcsStore implements the provider.Store interface at compile time.
var _ provider.Store = (*gcsStore)(nil)

type gcsLookupBucket struct {
	name       string
	visibility provider.Visibility
}

func (s *gcsStore) lookupBuckets() []gcsLookupBucket {
	buckets := make([]gcsLookupBucket, 0, 2)
	if s.publicBucket != "" {
		buckets = append(buckets, gcsLookupBucket{name: s.publicBucket, visibility: provider.Public})
	}
	if s.bucket != "" {
		buckets = append(buckets, gcsLookupBucket{name: s.bucket, visibility: provider.Private})
	}
	return buckets
}
