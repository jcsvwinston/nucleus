package nucleus

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// newTestRouter builds a Router backed by a fresh routerpkg.Router so
// the unit tests can exercise the nucleus Router interface end-to-end
// without spinning up an *app.App.
func newTestRouter(t *testing.T) (Router, http.Handler) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	rp := routerpkg.New(logger)
	return newRouter(rp.Mux), rp
}

// TestRouter_FlatRoutes confirms the Get/Post/Put/Patch/Delete shape
// against a real underlying Mux.
func TestRouter_FlatRoutes(t *testing.T) {
	r, srv := newTestRouter(t)

	r.Get("/ping", func(c *Context) error { return c.JSON(200, map[string]string{"v": "g"}) })
	r.Post("/items", func(c *Context) error { return c.JSON(201, map[string]string{"v": "p"}) })

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ping: got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"v":"g"`) {
		t.Fatalf("GET /ping body unexpected: %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/items", nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /items: got %d body=%q", rec.Code, rec.Body.String())
	}
}

// TestRouter_GroupPrefix confirms Group prepends its prefix and that
// routes inside the group inherit it.
func TestRouter_GroupPrefix(t *testing.T) {
	r, srv := newTestRouter(t)

	r.Group("/api", func(g Router) {
		g.Get("/users", func(c *Context) error { return c.JSON(200, "ok") })
	})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/users: got %d body=%q", rec.Code, rec.Body.String())
	}
}

// fakeController implements the explicit Resourceful interfaces for
// the methods the test exercises and intentionally omits the others
// so the panic path can be observed when those methods are requested.
type fakeController struct {
	called map[string]bool
}

func (f *fakeController) Index(c *Context) error {
	f.called["Index"] = true
	return c.JSON(200, "list")
}
func (f *fakeController) Show(c *Context) error {
	f.called["Show"] = true
	return c.JSON(200, map[string]string{"id": c.Param("id")})
}
func (f *fakeController) Create(c *Context) error {
	f.called["Create"] = true
	return c.JSON(201, "created")
}

// TestRouter_ResourceExplicitMethods confirms Resource registers only
// the methods listed in the explicit Methods() slice (Index + Show +
// Create here) and that Update/Destroy are NOT silently registered.
func TestRouter_ResourceExplicitMethods(t *testing.T) {
	r, srv := newTestRouter(t)
	ctrl := &fakeController{called: map[string]bool{}}

	r.Resource("/items", ctrl, Methods(Index, Show, Create))

	cases := []struct {
		method string
		path   string
		want   int
		key    string
	}{
		{http.MethodGet, "/items", http.StatusOK, "Index"},
		{http.MethodGet, "/items/42", http.StatusOK, "Show"},
		{http.MethodPost, "/items", http.StatusCreated, "Create"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d want %d body=%q", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
		if !ctrl.called[tc.key] {
			t.Fatalf("%s %s did not invoke controller.%s", tc.method, tc.path, tc.key)
		}
	}

	// PUT /items/42 must NOT be registered (Update was not in the
	// explicit Methods list). The default Mux returns 405 / 404 for
	// unregistered routes; either way the controller's Update would
	// not have been called.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/items/42", nil))
	if ctrl.called["Update"] {
		t.Fatal("Update was silently registered; explicit Methods discipline broken")
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("unexpected 200 for unregistered PUT /items/42; got body %q", rec.Body.String())
	}
}

// TestRouter_ResourceUnsatisfiedInterface confirms Resource panics with
// a clear message when the controller does not implement the
// interface for a requested Method.
func TestRouter_ResourceUnsatisfiedInterface(t *testing.T) {
	r, _ := newTestRouter(t)
	ctrl := &fakeController{called: map[string]bool{}} // does not implement Updater

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected Resource to panic when controller missed Updater")
		}
		msg, ok := rec.(string)
		if !ok || !strings.Contains(msg, "Updater") {
			t.Fatalf("expected panic message to name Updater, got %v", rec)
		}
	}()
	r.Resource("/items", ctrl, Methods(Update))
}

// TestRouter_ResourceNilController confirms Resource panics on a nil
// controller — easier to debug than a later type-assertion failure.
func TestRouter_ResourceNilController(t *testing.T) {
	r, _ := newTestRouter(t)
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected Resource to panic on nil controller")
		}
	}()
	r.Resource("/x", nil, Methods(Index))
}

// TestMethods_Helper confirms Methods returns the supplied slice.
func TestMethods_Helper(t *testing.T) {
	got := Methods(Index, Show, Create)
	want := []Method{Index, Show, Create}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("at %d: got %v want %v", i, got[i], want[i])
		}
	}
}
