package admin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// leakyAuth is an AdminAuth whose Authenticate fails with a crafted error
// carrying a sensitive sentinel, used to prove the 401 body never echoes it.
type leakyAuth struct{ err error }

func (a *leakyAuth) Authenticate(*http.Request) (*auth.User, error) { return nil, a.err }
func (a *leakyAuth) Authorize(*auth.User, string, string) bool      { return false }
func (a *leakyAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

// TestAdminAPI_AuthError_DoesNotLeakRawError asserts that the 401 response for
// a failed admin authentication carries only a fixed client-facing message and
// never echoes the raw provider error (which can contain internal detail such
// as a DB DSN or connectivity diagnostics). Regression guard for the ADR-016
// review follow-up.
func TestAdminAPI_AuthError_DoesNotLeakRawError(t *testing.T) {
	const sentinel = "postgres://admin:SUPERSECRET@10.0.0.5/db: connection refused"
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, &leakyAuth{err: errors.New(sentinel)})
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/rbac/policies")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body=%s)", resp.StatusCode, body)
	}
	for _, leak := range []string{"SUPERSECRET", "connection refused", "postgres://", "10.0.0.5"} {
		if strings.Contains(body, leak) {
			t.Errorf("401 body leaked raw auth error fragment %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "authentication required") {
		t.Errorf("expected fixed 'authentication required' message, got: %s", body)
	}
}
