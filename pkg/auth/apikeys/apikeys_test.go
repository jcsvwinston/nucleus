package apikeys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *SQLStore {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:apikeys_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewSQLStore(t.Context(), db, SQLStoreConfig{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return store
}

func TestIssueAndAuthenticate(t *testing.T) {
	store := testStore(t)
	key, presented, err := Issue(t.Context(), store, Key{
		Name: "ci", OwnerID: "user-1", Scopes: []string{"billing:read"},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if !strings.HasPrefix(presented, Prefix+"_") {
		t.Errorf("the key has no recognisable prefix: %q", presented)
	}
	if strings.Contains(presented, key.SecretHash) {
		t.Error("the presented key contains its own stored hash")
	}

	authenticated, err := Authenticate(t.Context(), store, presented)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authenticated.ID != key.ID || authenticated.OwnerID != "user-1" {
		t.Fatalf("authenticated as %+v", authenticated)
	}
}

// The secret is not in the database: a leaked backup must not be a leak of
// every key.
func TestIssue_StoresOnlyAHash(t *testing.T) {
	store := testStore(t)
	_, presented, err := Issue(t.Context(), store, Key{Name: "ci"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	_, secret, _ := parse(presented)

	var stored string
	if err := store.db.QueryRow("SELECT secret_hash FROM nucleus_api_keys LIMIT 1").Scan(&stored); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if stored == secret || strings.Contains(stored, secret) {
		t.Fatal("the secret is recoverable from the database")
	}
	if stored != hashSecret(secret) {
		t.Fatal("the stored value is not the secret's hash")
	}
}

func TestAuthenticate_RejectsEverythingThatIsNotAValidKey(t *testing.T) {
	store := testStore(t)
	key, presented, err := Issue(t.Context(), store, Key{Name: "ci"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	id, secret, _ := parse(presented)

	cases := map[string]string{
		"empty":         "",
		"not ours":      "sk_live_something",
		"missing parts": Prefix + "_" + id,
		"unknown id":    Prefix + "_aaaaaaaa_" + secret,
		"wrong secret":  Prefix + "_" + id + "_wrongwrongwrong",
		"truncated":     presented[:len(presented)-4],
	}
	for name, candidate := range cases {
		if _, err := Authenticate(t.Context(), store, candidate); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s was accepted (or failed differently): %v", name, err)
		}
	}
	_ = key
}

func TestRevoke_StopsAuthentication(t *testing.T) {
	store := testStore(t)
	key, presented, _ := Issue(t.Context(), store, Key{Name: "ci"})

	if err := store.Revoke(t.Context(), key.ID, time.Now()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := Authenticate(t.Context(), store, presented); !errors.Is(err, ErrInvalidKey) {
		t.Fatal("a revoked key still authenticates")
	}
}

func TestExpiry_StopsAuthentication(t *testing.T) {
	store := testStore(t)
	_, presented, err := Issue(t.Context(), store, Key{
		Name: "ci", ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := Authenticate(t.Context(), store, presented); !errors.Is(err, ErrInvalidKey) {
		t.Fatal("an expired key still authenticates")
	}
}

// A rotation that breaks the caller at the moment it happens is a rotation
// nobody performs: both keys work during the grace period, and only the new
// one after it.
func TestRotate_KeepsTheOldKeyAliveForTheGracePeriod(t *testing.T) {
	store := testStore(t)
	original, originalPresented, _ := Issue(t.Context(), store, Key{
		Name: "ci", OwnerID: "user-1", Scopes: []string{"billing:read"},
	})

	replacement, replacementPresented, err := Rotate(t.Context(), store, original.ID, time.Hour)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if replacement.RotatedFrom != original.ID {
		t.Error("the replacement does not record what it replaced")
	}
	if len(replacement.Scopes) != 1 || replacement.Scopes[0] != "billing:read" {
		t.Errorf("the replacement lost its scopes: %v", replacement.Scopes)
	}

	if _, err := Authenticate(t.Context(), store, originalPresented); err != nil {
		t.Fatalf("the old key stopped working immediately: %v", err)
	}
	if _, err := Authenticate(t.Context(), store, replacementPresented); err != nil {
		t.Fatalf("the new key does not work: %v", err)
	}

	// With no grace, the old one stops at once.
	second, _, err := Rotate(t.Context(), store, replacement.ID, 0)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := Authenticate(t.Context(), store, replacementPresented); !errors.Is(err, ErrInvalidKey) {
		t.Fatal("a key rotated with no grace still works")
	}
	_ = second
}

// An empty scope list means "no scopes", never "every scope". The opposite
// default is how an unscoped key ends up more powerful than a scoped one.
func TestScopes_EmptyMeansNone(t *testing.T) {
	key := Key{}
	if key.HasScope("anything") {
		t.Fatal("a key with no scopes claimed one")
	}
}

// --- middleware -------------------------------------------------------------

func TestMiddleware_AuthenticatesBothHeaderForms(t *testing.T) {
	store := testStore(t)
	_, presented, _ := Issue(t.Context(), store, Key{Name: "ci", OwnerID: "user-1"})

	for name, set := range map[string]func(*http.Request){
		"X-API-Key": func(r *http.Request) { r.Header.Set(HeaderName, presented) },
		"Bearer":    func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+presented) },
	} {
		var sawKey bool
		var sawUser string
		h := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, sawKey = FromContext(r.Context())
			sawUser = observe.UserIDFromCtx(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		set(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if !sawKey {
			t.Errorf("%s: the handler did not see the key", name)
		}
		// The owner lands where the rate limiter already looks, so a key
		// is throttled as an identity rather than sharing a bucket with
		// everyone behind the same address.
		if sawUser != "user-1" {
			t.Errorf("%s: the owner did not reach the observability context (%q)", name, sawUser)
		}
	}
}

func TestMiddleware_RefusesABadKeyAndIgnoresNone(t *testing.T) {
	store := testStore(t)

	var reached bool
	h := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderName, Prefix+"_aaaaaa_bbbbbb")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if reached {
		t.Error("a request with an invalid key reached the handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an invalid key answered %d, want 401", rec.Code)
	}

	// A request with no key is not this middleware's business.
	reached = false
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !reached {
		t.Error("a request with no key was refused")
	}
}

// A JWT bearer token must pass through untouched: the two schemes share the
// header.
func TestMiddleware_LeavesOtherBearerTokensAlone(t *testing.T) {
	store := testStore(t)
	var reached bool
	h := Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.e30.signature")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !reached {
		t.Fatal("a JWT was refused by the API-key middleware")
	}
}

func TestRequire_EnforcesScopes(t *testing.T) {
	store := testStore(t)
	_, presented, _ := Issue(t.Context(), store, Key{Name: "ci", Scopes: []string{"billing:read"}})

	handler := Middleware(store)(Require("billing:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderName, presented)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("a key with the scope answered %d", rec.Code)
	}

	denied := Middleware(store)(Require("billing:write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderName, presented)
	rec = httptest.NewRecorder()
	denied.ServeHTTP(rec, req)
	// 403, not 401: the caller IS authenticated and presenting the same
	// key again will not help.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a key without the scope answered %d, want 403", rec.Code)
	}
}

func TestRequire_WithoutAKeyIs401(t *testing.T) {
	h := Require("billing:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("answered %d, want 401", rec.Code)
	}
}

func TestList(t *testing.T) {
	store := testStore(t)
	if _, _, err := Issue(t.Context(), store, Key{Name: "one", OwnerID: "user-1"}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, _, err := Issue(t.Context(), store, Key{Name: "two", OwnerID: "user-2"}); err != nil {
		t.Fatalf("issue: %v", err)
	}

	mine, err := store.List(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mine) != 1 || mine[0].Name != "one" {
		t.Fatalf("listing for user-1 returned %d keys", len(mine))
	}
	all, err := store.List(t.Context(), "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("listing everything returned %d keys", len(all))
	}
}

var _ = context.Background

// The defect this guards against was intermittent: base64url secrets
// contain "_", the separator between a key's parts, so roughly one key in
// ten split into four pieces and was rejected as invalid. A hundred keys
// is enough to catch a one-in-ten failure with certainty.
func TestIssue_EveryKeyParsesBack(t *testing.T) {
	store := testStore(t)
	for i := 0; i < 100; i++ {
		key, presented, err := Issue(t.Context(), store, Key{Name: fmt.Sprintf("key-%d", i)})
		if err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
		id, secret, err := parse(presented)
		if err != nil {
			t.Fatalf("key %q does not parse: %v", presented, err)
		}
		if id != key.ID {
			t.Fatalf("parsed id %q, issued %q", id, key.ID)
		}
		if hashSecret(secret) != key.SecretHash {
			t.Fatalf("the parsed secret does not match the stored hash for %q", presented)
		}
		if _, err := Authenticate(t.Context(), store, presented); err != nil {
			t.Fatalf("key %d does not authenticate: %v", i, err)
		}
	}
}
