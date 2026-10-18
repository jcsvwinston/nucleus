package router

import (
	"net/http"
	"testing"
)

// mustCSRF builds the CSRF middleware for a test and fails it on options
// the constructor rejects.
func mustCSRF(t *testing.T, opts CSRFOptions) func(http.Handler) http.Handler {
	t.Helper()
	mw, err := NewCSRFMiddleware(opts)
	if err != nil {
		t.Fatalf("NewCSRFMiddleware: %v", err)
	}
	return mw
}
