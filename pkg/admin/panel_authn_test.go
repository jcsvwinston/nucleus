package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// TestAdminAPI_RoutesCarryAuthMiddleware is the structural regression guard
// for ADR-016: when an admin auth provider is configured, every /api/* route
// must be mounted behind router-layer middleware (authMiddleware), not left
// to rely solely on each handler's authorizeAction() call. Before the fix the
// /api/* routes were registered flat on the root mux with zero middleware, so
// a handler that forgot its authorizeAction() call was silently public. This
// test walks the registered routes and fails if any /api/* route has no
// router-layer middleware.
//
// Note: this is a count guard, not an identity guard — Walk reports how many
// middlewares a route carries, not which ones. The end-to-end proof that the
// middleware is specifically authMiddleware (and rejects unauthenticated
// requests) is TestAdminAPI_UnauthenticatedRequestRejected below.
func TestAdminAPI_RoutesCarryAuthMiddleware(t *testing.T) {
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, &testAdminAuth{})
	defer cleanup()

	mux := panel.Handler()

	apiRoutes := 0
	var bare []string
	err := mux.Walk(func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/") {
			apiRoutes++
			if len(mws) == 0 {
				bare = append(bare, method+" "+route)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if apiRoutes == 0 {
		t.Fatal("no /api/ routes discovered — route shape changed; update this guard")
	}
	if len(bare) > 0 {
		t.Errorf("%d /api route(s) have NO router-layer middleware (authn bypass risk): %v", len(bare), bare)
	}
}

// TestAdminAPI_UnauthenticatedRequestRejected pins, end-to-end, that sensitive
// enumerate/mutate endpoints reject an unauthenticated request when auth is
// configured. testAdminAuth with a nil user fails every Authenticate call.
func TestAdminAPI_UnauthenticatedRequestRejected(t *testing.T) {
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, &testAdminAuth{}) // user == nil => unauthenticated
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/rbac/policies"},
		{http.MethodPost, "/api/rbac/policies"},
		{http.MethodPost, "/api/rbac/roles/assign"},
		{http.MethodGet, "/api/rbac/roles"},
		{http.MethodGet, "/api/audit"},
		{http.MethodPost, "/api/audit/clear"},
		{http.MethodPost, "/api/migrations/apply"},
		{http.MethodGet, "/api/system/flags"},
		{http.MethodPost, "/api/system/flags"},
		{http.MethodPost, "/api/logout"}, // now behind authn (ADR-016)
		{http.MethodGet, "/api/live/ws"}, // websocket upgrade rejected pre-auth
	}
	for _, tc := range cases {
		t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s: expected 401, got %d", tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}

// TestAdminAPI_AuthenticatedRequestReachesHandler confirms the edge
// authMiddleware passes an authenticated request through to the handler, so
// the hardening does not break the normal authorized flow.
func TestAdminAPI_AuthenticatedRequestReachesHandler(t *testing.T) {
	authProvider := &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin"}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, authProvider)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/rbac/policies")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authenticated GET /api/rbac/policies: expected 200, got %d", resp.StatusCode)
	}
}
