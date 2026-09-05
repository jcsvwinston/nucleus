package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMux_ResourceRegistersRESTRoutes(t *testing.T) {
	mux := NewMux()
	mux.Resource("/users", ResourceHandlers{
		List: func(c *Context) error {
			return c.JSON(http.StatusOK, "list")
		},
		Create: func(c *Context) error {
			return c.JSON(http.StatusCreated, "create")
		},
		Retrieve: func(c *Context) error {
			return c.JSON(http.StatusOK, "get:"+c.Param("id"))
		},
		Update: func(c *Context) error {
			return c.JSON(http.StatusOK, "update:"+c.Param("id"))
		},
		Delete: func(c *Context) error {
			return c.JSON(http.StatusOK, "delete:"+c.Param("id"))
		},
	})

	cases := []struct {
		method string
		path   string
		code   int
		body   string
	}{
		{method: http.MethodGet, path: "/users/", code: http.StatusOK, body: "\"list\"\n"},
		{method: http.MethodPost, path: "/users/", code: http.StatusCreated, body: "\"create\"\n"},
		{method: http.MethodGet, path: "/users/7", code: http.StatusOK, body: "\"get:7\"\n"},
		{method: http.MethodPut, path: "/users/7", code: http.StatusOK, body: "\"update:7\"\n"},
		{method: http.MethodDelete, path: "/users/7", code: http.StatusOK, body: "\"delete:7\"\n"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != tc.code {
			t.Fatalf("%s %s expected status %d, got %d", tc.method, tc.path, tc.code, rec.Code)
		}
		if rec.Body.String() != tc.body {
			t.Fatalf("%s %s expected body %q, got %q", tc.method, tc.path, tc.body, rec.Body.String())
		}
	}
}

func TestMux_ResourceSkipsNilHandlers(t *testing.T) {
	mux := NewMux()
	mux.Resource("projects", ResourceHandlers{
		List: func(c *Context) error {
			return c.JSON(http.StatusOK, "list")
		},
	})

	getReq := httptest.NewRequest(http.MethodGet, "/projects/", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /projects/ expected status 200, got %d", getRec.Code)
	}
	if getRec.Body.String() != "\"list\"\n" {
		t.Fatalf("GET /projects/ expected body list, got %q", getRec.Body.String())
	}

	postReq := httptest.NewRequest(http.MethodPost, "/projects/", nil)
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /projects/ expected status 405 when handler is nil, got %d", postRec.Code)
	}
}

// NU-31: the collection answers at /reports and /reports/ alike; the bare
// path used to be a 307 to the trailing-slash spelling.
func TestMux_ResourceCollectionAtBothSpellings(t *testing.T) {
	mux := NewMux()
	mux.Resource("/reports", ResourceHandlers{
		List: func(c *Context) error {
			return c.NoContent()
		},
	})

	for _, path := range []string{"/reports", "/reports/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("GET %s: expected 204 from List, got %d", path, rec.Code)
		}
	}
}
