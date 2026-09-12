package apikeys

import (
	"context"
	"net/http"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// HeaderName is the dedicated header. The Authorization bearer scheme is
// accepted too, because half the clients in the world only know that one.
const HeaderName = "X-API-Key"

type contextKey struct{}

// FromContext returns the key that authenticated this request.
func FromContext(ctx context.Context) (Key, bool) {
	key, ok := ctx.Value(contextKey{}).(Key)
	return key, ok
}

// Middleware authenticates requests carrying an API key.
//
// It puts the key's OWNER in the observability context, which is not
// bookkeeping: the rate limiter keys on the authenticated user id with the
// tenant as a prefix, so a key that lands there is throttled as an identity
// instead of sharing a bucket with every other caller behind the same
// address. Measuring that the limiter already worked this way is what made
// this three lines instead of a second limiter.
//
// A request with no key passes through untouched. Authentication is what
// this middleware does; authorisation is the policy layer's job, and a
// route that must have a key uses Require.
func Middleware(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := presentedKey(r)
			if presented == "" {
				next.ServeHTTP(w, r)
				return
			}

			key, err := Authenticate(r.Context(), store, presented)
			if err != nil {
				gferrors.WriteError(w, r, gferrors.Unauthorized("invalid API key"), nil)
				return
			}

			ctx := context.WithValue(r.Context(), contextKey{}, key)
			if key.OwnerID != "" {
				ctx = observe.CtxWithUserID(ctx, key.OwnerID)
			}
			// Recording use is best-effort: a store that cannot write a
			// timestamp must not fail the request it was authenticating.
			go func(id string) {
				_ = store.TouchLastUsed(context.WithoutCancel(ctx), id, time.Now().UTC())
			}(key.ID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Require refuses a request that did not present a valid key, and — when
// scopes are named — one whose key does not carry them ALL.
//
//	r.With(apikeys.Require("billing:read")).Get("/invoices", listInvoices)
func Require(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := FromContext(r.Context())
			if !ok {
				gferrors.WriteError(w, r, gferrors.Unauthorized("an API key is required"), nil)
				return
			}
			for _, scope := range scopes {
				if !key.HasScope(scope) {
					// 403 and not 401: the caller IS authenticated, and
					// presenting the same key again will not help.
					gferrors.WriteError(w, r, gferrors.Forbidden("this key does not carry the scope "+scope), nil)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// presentedKey reads the key from either accepted place. A bearer token
// that is not one of ours is left alone, so JWT and API keys can share the
// header.
func presentedKey(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get(HeaderName)); header != "" {
		return header
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") &&
		strings.HasPrefix(strings.TrimSpace(parts[1]), Prefix+"_") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
