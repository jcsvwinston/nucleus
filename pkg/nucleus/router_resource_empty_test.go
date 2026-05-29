package nucleus

import (
	"strings"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// TestJoinPath_NeverEmptyNoDoubleSlash is the primary deterministic guard for
// the empty-path panic: routerAdapter.joinPath must never produce "" (which
// reaches net/http.ServeMux.Handle as the invalid pattern "GET " and panics
// the mux at startup) and must never produce a pattern containing "//".
//
// See examples/mvc_api/internal/notes/module.go for the documented footgun and
// pkg/nucleus/router.go joinPath/Resource for the fix.
func TestJoinPath_NeverEmptyNoDoubleSlash(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		// Empty prefix (the live framework always constructs adapters with an
		// empty prefix — see newRouterAdapter / newRouterAdapterFromMux call
		// sites in nucleus.go), empty path: the Resource("") trigger. Must
		// floor to "/" rather than return "".
		{name: "empty prefix, empty path", prefix: "", path: "", want: "/"},
		{name: "empty prefix, root path", prefix: "", path: "/", want: "/"},
		{name: "empty prefix, normal path", prefix: "", path: "/notes", want: "/notes"},
		// Non-empty prefix joins: the trailing-slash-on-prefix + leading-slash-
		// on-path case is the one that would otherwise yield "/api//notes".
		{name: "prefix no trailing, path with leading", prefix: "/api", path: "/notes", want: "/api/notes"},
		{name: "prefix trailing slash, path leading slash", prefix: "/api/", path: "/notes", want: "/api/notes"},
		{name: "prefix trailing slash, root path", prefix: "/api/", path: "/", want: "/api/"},
		{name: "prefix set, empty path", prefix: "/api", path: "", want: "/api"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &routerAdapter{prefix: tc.prefix}
			got := a.joinPath(tc.path)

			if got == "" {
				t.Fatalf("joinPath(prefix=%q, path=%q) returned empty string; an empty pattern panics net/http.ServeMux at startup", tc.prefix, tc.path)
			}
			if strings.Contains(got, "//") {
				t.Errorf("joinPath(prefix=%q, path=%q) = %q; result must not contain \"//\"", tc.prefix, tc.path, got)
			}
			if got != tc.want {
				t.Errorf("joinPath(prefix=%q, path=%q) = %q; want %q", tc.prefix, tc.path, got, tc.want)
			}
		})
	}
}

// emptyResourceController is a minimal controller implementing every REST
// sub-interface the notes module registers (Index/Show/Create/Update/Destroy),
// modeled on examples/mvc_api/internal/notes. It carries no database handle
// because Resource registration builds route patterns and type-asserts the
// controller at registration time — the panic under test fires before any
// handler runs, so no DB/app wiring is needed.
type emptyResourceController struct{}

func (emptyResourceController) Index(*Context) error   { return nil }
func (emptyResourceController) Show(*Context) error    { return nil }
func (emptyResourceController) Create(*Context) error  { return nil }
func (emptyResourceController) Update(*Context) error  { return nil }
func (emptyResourceController) Destroy(*Context) error { return nil }

// Compile-time assertions that the test controller satisfies the sub-interfaces
// the framework asserts in Resource. If the nucleus sub-interface contract
// changes, this test fails to compile rather than silently skipping coverage.
var (
	_ Indexer   = emptyResourceController{}
	_ Shower    = emptyResourceController{}
	_ Creator   = emptyResourceController{}
	_ Updater   = emptyResourceController{}
	_ Destroyer = emptyResourceController{}
)

// TestResourceEmptyPathDoesNotPanic is the higher-level guard: it constructs
// the real routerAdapter over a real *routerpkg.Mux (the same type Start()
// hands a module) with an empty prefix — matching how nucleus.go always builds
// adapters — and registers Resource("") exactly as a module would. Before the
// fix this registered the pattern "GET " and panicked net/http.ServeMux. The
// explicit recover() converts any panic into a test failure with the message.
func TestResourceEmptyPathDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Resource(\"\") panicked during registration: %v", r)
		}
	}()

	// Empty prefix mirrors newRouterAdapterFromMux(sub, "") / newRouterAdapter(
	// core.Router, "") in nucleus.go.
	a := newRouterAdapterFromMux(routerpkg.NewMux(), "")
	a.Resource("", emptyResourceController{}, Methods(
		Index, Show, Create, Update, Destroy,
	))
}
