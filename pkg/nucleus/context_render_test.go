package nucleus

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// Render is the template path, and so is HTML: since v2.0.0 removed the
// raw-string HTML(code, html) that shadowed it (DEP-2026-009, AN-07), the
// HTML a *nucleus.Context exposes is the promoted router.Context.HTML, so
// c.HTML(code, name, data) and c.Render(code, name, data) render the same
// template and generated code no longer needs to spell c.Context.HTML.
func TestContext_RenderUsesTemplateEngine(t *testing.T) {
	tpl := template.Must(template.New("greet/index.html").Parse("hello {{.name}}"))

	for name, render := range map[string]func(*Context) error{
		"Render": func(fc *Context) error {
			return fc.Render(http.StatusOK, "greet/index.html", map[string]interface{}{"name": "world"})
		},
		"HTML": func(fc *Context) error {
			return fc.HTML(http.StatusOK, "greet/index.html", map[string]interface{}{"name": "world"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			ctx := routerpkg.NewContext(w, req, nil, routerpkg.WithTemplates(tpl))
			fc := &Context{Context: ctx}

			if err := render(fc); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got := w.Body.String(); !strings.Contains(got, "hello world") {
				t.Fatalf("%s body = %q; want template output", name, got)
			}
		})
	}
}

func TestContext_RenderWithoutEngineReturnsClearError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	fc := &Context{Context: routerpkg.NewContext(w, req, nil)}

	err := fc.Render(http.StatusOK, "greet/index.html", nil)
	if err == nil {
		t.Fatal("Render without a template engine returned nil error")
	}
	if !strings.Contains(err.Error(), "templates_dir") {
		t.Fatalf("Render error does not point at templates_dir: %v", err)
	}
}
