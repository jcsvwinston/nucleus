package nucleus

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

func TestContext_Query(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?key=value", nil)
	ctx := &routerpkg.Context{
		Request: req,
	}
	fc := &Context{Context: ctx}

	result := fc.Query("key")
	if result != "value" {
		t.Errorf("Expected value, got %s", result)
	}
}

func TestContext_JSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
		Writer:  w,
	}
	fc := &Context{Context: ctx}

	err := fc.JSON(200, map[string]string{"message": "hello"})
	if err != nil {
		t.Fatalf("JSON() returned error: %v", err)
	}
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestContext_String(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
		Writer:  w,
	}
	fc := &Context{Context: ctx}

	err := fc.String(200, "hello")
	if err != nil {
		t.Fatalf("String() returned error: %v", err)
	}
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "hello") {
		t.Errorf("Expected body to contain 'hello', got %s", w.Body.String())
	}
}

func TestContext_HTML(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
		Writer:  w,
	}
	fc := &Context{Context: ctx}

	err := fc.HTML(200, "<html><body>hello</body></html>")
	if err != nil {
		t.Fatalf("HTML() returned error: %v", err)
	}
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestContext_Status(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
		Writer:  w,
	}
	fc := &Context{Context: ctx}

	fc.Status(404)
	if w.Code != 404 {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestContext_NoContent(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
		Writer:  w,
	}
	fc := &Context{Context: ctx}

	err := fc.NoContent()
	if err != nil {
		t.Fatalf("NoContent() returned error: %v", err)
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}
}

func TestContext_Redirect(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
		Writer:  w,
	}
	fc := &Context{Context: ctx}

	err := fc.Redirect(302, "/new-location")
	if err != nil {
		t.Fatalf("Redirect() returned error: %v", err)
	}
	if w.Code != 302 {
		t.Errorf("Expected status 302, got %d", w.Code)
	}
}

func TestContext_Set(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
	}
	fc := &Context{Context: ctx}

	fc.Set("key", "value")
	// Just verify it doesn't panic - actual storage is in router.Context
}

func TestContext_Get(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := &routerpkg.Context{
		Request: req,
	}
	fc := &Context{Context: ctx}

	result := fc.Get("nonexistent")
	if result != nil {
		t.Errorf("Expected nil for nonexistent key, got %v", result)
	}
}

// BindXML used to hand the raw body to the decoder with no cap and no
// validation, while BindJSON and BindForm had both. The three binders are
// one discipline: 1 MiB, then 413; malformed, then 400; then the
// `validate` tags.
func TestBindXML_CapValidateAndClassify(t *testing.T) {
	type doc struct {
		XMLName struct{} `xml:"doc"`
		Name    string   `xml:"name" validate:"required"`
	}
	bind := func(body string) error {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/xml")
		rec := httptest.NewRecorder()
		c := &Context{Context: routerpkg.NewContext(rec, req, nil)}
		var d doc
		return c.BindXML(&d)
	}

	if err := bind("<doc><name>ok</name></doc>"); err != nil {
		t.Fatalf("a valid document must bind: %v", err)
	}

	var domErr *gferrors.DomainError
	err := bind("<doc><name>" + strings.Repeat("x", 2<<20) + "</name></doc>")
	if !errors.As(err, &domErr) || domErr.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("a 2 MiB body must be a 413 DomainError, got %v", err)
	}

	err = bind("<doc><name>unterminated")
	if !errors.As(err, &domErr) || domErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("a malformed document must be a 400 DomainError, got %v", err)
	}

	err = bind("<doc><name></name></doc>")
	if err == nil {
		t.Fatal("a document that fails its validate tags must not bind")
	}
	if errors.As(err, &domErr) && domErr.StatusCode == http.StatusRequestEntityTooLarge {
		t.Fatalf("validation failure misclassified: %v", err)
	}
}
