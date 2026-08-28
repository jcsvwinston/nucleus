// Package storage provides a durable, provider-agnostic file storage interface
// for Nucleus applications. It abstracts S3, GCS, Azure Blob, and local
// filesystem behind a single stable API designed to last through v1.x.
//
// The interface is streaming-native (io.Reader/io.ReadCloser) so large files
// never need to be held in memory. Multi-tenant applications automatically
// receive prefix isolation (tenant_a/uploads/file.pdf).
//
// Provider selection is configuration-driven: the application code never
// changes when switching from local dev to S3 in production.
package provider

import (
	"context"
	"io"
	"time"
)

// Visibility controls whether an object is publicly accessible or requires
// a signed URL (or app-layer authentication) to access.
type Visibility string

const (
	// Private objects are not directly accessible via URL.
	// Access requires SignedURL() or serving through the app layer.
	Private Visibility = "private"

	// Public objects have a direct, unauthenticated URL.
	Public Visibility = "public"
)

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key         string            `json:"key"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type"`
	Visibility  Visibility        `json:"visibility"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// PutOptions configures how an object is stored.
type PutOptions struct {
	// Visibility controls public access. Defaults to Private.
	Visibility Visibility

	// ContentType is the MIME type (e.g. "image/png", "application/pdf").
	// Auto-detected from key extension when empty.
	ContentType string

	// Metadata stores custom key-value pairs on the provider.
	Metadata map[string]string

	// TenantPrefix overrides the automatic tenant prefix.
	// Empty means auto-detect from context. Set to "" explicitly to disable prefixing.
	TenantPrefix string
}

// ListOptions configures object listing.
type ListOptions struct {
	// Prefix filters objects by key prefix (directory-like listing).
	Prefix string

	// Delimiter causes keys containing the delimiter after the prefix
	// to be rolled up into "common prefixes" (simulating directories).
	Delimiter string

	// Limit caps the number of results. 0 = provider default (usually 1000).
	Limit int

	// Marker starts listing after this key (for pagination).
	Marker string
}

// ListResult is the response from List().
type ListResult struct {
	Objects        []ObjectInfo `json:"objects"`
	CommonPrefixes []string     `json:"common_prefixes,omitempty"`
	NextMarker     string       `json:"next_marker,omitempty"`
	Truncated      bool         `json:"truncated"`
}

// URLConfig configures URL generation.
type URLConfig struct {
	// Expires sets the URL validity duration. Only meaningful for SignedURL.
	Expires time.Duration

	// ContentType overrides the Content-Type header for the URL response.
	ContentType string

	// Disposition sets Content-Disposition header ("inline" or "attachment").
	Disposition string
}

// Store is the durable interface for file storage in Nucleus.
// All implementations (S3, GCS, Azure, local) must satisfy this interface.
// It is intentionally minimal: add provider-specific features through
// type assertions when absolutely necessary.
type Store interface {
	// Put uploads a file from an io.Reader. The reader is consumed entirely.
	// Key is the logical path (e.g. "tenant_a/uploads/image.png").
	// Returns the final storage key (with tenant prefix applied) and object info.
	Put(ctx context.Context, key string, reader io.Reader, opts PutOptions) (ObjectInfo, error)

	// Get retrieves a file by key. Returns an io.ReadCloser that MUST be closed.
	// Returns ErrNotFound if the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)

	// Delete removes an object by key. Idempotent: no error if key doesn't exist.
	Delete(ctx context.Context, key string) error

	// Exists checks if a key exists.
	Exists(ctx context.Context, key string) (bool, error)

	// List returns objects with the given prefix.
	List(ctx context.Context, opts ListOptions) (ListResult, error)

	// PublicURL returns a publicly accessible URL for a key.
	// Returns empty string if the object is private or the provider
	// doesn't support public URLs.
	PublicURL(ctx context.Context, key string, opts URLConfig) (string, error)

	// SignedURL returns a time-limited URL for accessing a private object.
	// The URL grants direct access to the object without authentication.
	SignedURL(ctx context.Context, key string, expires time.Duration, opts URLConfig) (string, error)

	// Copy copies an object from srcKey to dstKey (within the same bucket/container).
	Copy(ctx context.Context, srcKey, dstKey string) (ObjectInfo, error)

	// Close releases any resources held by the store (connections, background goroutines).
	Close() error
}

// ErrNotFound is returned when a key does not exist.
type ErrNotFound string

func (e ErrNotFound) Error() string {
	return "storage: object not found: " + string(e)
}

// ErrInvalidKey is returned when a key contains invalid characters.
type ErrInvalidKey string

func (e ErrInvalidKey) Error() string {
	return "storage: invalid key: " + string(e)
}
