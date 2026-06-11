package admin

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	_ "modernc.org/sqlite"
)

// medianLoginDuration POSTs the form through the real login handler n
// times, asserts every response is 401, and returns the median duration.
func medianLoginDuration(t *testing.T, h http.Handler, form url.Values, n int) time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		start := time.Now()
		h.ServeHTTP(rec, req)
		durations = append(durations, time.Since(start))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("login = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations[n/2]
}

// TestHandleLoginPOST_UnknownUserBurnsBcryptCompare pins the fix for the
// login timing oracle (fleetdesk finding #17): the unknown-username
// branch must cost one bcrypt compare like the wrong-password branch.
// Pre-fix the unknown path returned in microseconds versus a full
// cost-12 compare (~50-300 ms) — a ratio near 10⁻⁴. The assertion allows
// the unknown path to be 4× faster before failing, three orders of
// magnitude looser than the expected ≈1.0 ratio, so scheduler noise
// cannot flake it while the pre-fix behaviour fails it by a mile.
func TestHandleLoginPOST_UnknownUserBurnsBcryptCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping bcrypt timing test in short mode (6 cost-12 compares, ~1.3s)")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE nucleus_admin_users (
		id TEXT PRIMARY KEY, username TEXT, email TEXT,
		password_hash TEXT, is_superuser INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO nucleus_admin_users VALUES ('1','admin','admin@example.com',?,1)`,
		hash,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// No session manager: both rejection branches render before any
	// session use, which is exactly the surface under test.
	a := NewDatabaseAdminAuth(db, nil, "/admin")
	h := a.LoginHandler()

	wrongPassword := medianLoginDuration(t, h,
		url.Values{"username": {"admin"}, "password": {"nope"}}, 3)
	unknownUser := medianLoginDuration(t, h,
		url.Values{"username": {"ghost"}, "password": {"nope"}}, 3)

	if unknownUser*4 < wrongPassword {
		t.Fatalf("unknown-user path (%v) is >4x faster than wrong-password path (%v): ratio=%.2f, timing oracle is back",
			unknownUser, wrongPassword, float64(wrongPassword)/float64(unknownUser))
	}
}
