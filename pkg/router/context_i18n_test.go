package router

import (
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/i18n"
)

func TestContextT_DegradesToKeyWithoutMiddleware(t *testing.T) {
	c := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), nil)
	if got := c.T("greeting"); got != "greeting" {
		t.Fatalf("c.T without i18n middleware = %q, want the key", got)
	}
	if got := c.T("%d items", 2); got != "2 items" {
		t.Fatalf("c.T formats args without middleware: %q", got)
	}
	var nilCtx *Context
	if got := nilCtx.T("greeting"); got != "greeting" {
		t.Fatalf("nil receiver must degrade to the key, got %q", got)
	}
}

func TestContextT_ReadsMiddlewareState(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	tr := i18n.New(nil, "en")
	ctx := i18n.WithTranslator(i18n.WithLocale(req.Context(), "es"), tr)
	c := NewContext(httptest.NewRecorder(), req.WithContext(ctx), nil)
	// Empty catalog: the chain ends at the key, but the translator ran.
	if got := c.T("greeting"); got != "greeting" {
		t.Fatalf("c.T = %q", got)
	}
}
