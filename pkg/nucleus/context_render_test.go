package nucleus

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// AN-07: nucleus.Context.HTML (raw string writer) shadows the embedded
// router.Context.HTML (template render), which forced generated code to
// spell c.Context.HTML. Render is the unshadowed template path; this test
// pins that it goes through the engine while HTML stays the raw writer.
func TestContext_RenderUsesTemplateEngine(t *testing.T) {
	tpl := template.Must(template.New("greet/index.html").Parse("hello {{.name}}"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := routerpkg.NewContext(w, req, nil, routerpkg.WithTemplates(tpl))
	fc := &Context{Context: ctx}

	if err := fc.Render(http.StatusOK, "greet/index.html", map[string]interface{}{"name": "world"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := w.Body.String(); !strings.Contains(got, "hello world") {
		t.Fatalf("Render body = %q; want template output", got)
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
