package nucleus

import (
	"encoding/json"
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

// NU-107: BindJSON answered 400 to every JSON array body — the decoded
// slice went to the struct validator, which refuses anything that is not a
// struct. A bulk endpoint (POST /notes/import taking []Note) could not
// bind at all.
func TestBindJSON_ArrayBodies(t *testing.T) {
	type note struct {
		Title string `json:"title" validate:"required"`
		Body  string `json:"body"`
	}
	bind := func(body string, v any) error {
		req := httptest.NewRequest(http.MethodPost, "/notes/import", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c := &Context{Context: routerpkg.NewContext(httptest.NewRecorder(), req, nil)}
		return c.BindJSON(v)
	}

	t.Run("array of valid structs binds", func(t *testing.T) {
		var notes []note
		if err := bind(`[{"title":"a"},{"title":"b","body":"x"}]`, &notes); err != nil {
			t.Fatalf("BindJSON: %v", err)
		}
		if len(notes) != 2 || notes[0].Title != "a" || notes[1].Body != "x" {
			t.Fatalf("bound %+v", notes)
		}
	})

	t.Run("one invalid element is the struct's 422, named by index and field", func(t *testing.T) {
		var notes []note
		err := bind(`[{"title":"a"},{"body":"no title"}]`, &notes)
		var de *gferrors.DomainError
		if !errors.As(err, &de) || de.StatusCode != http.StatusUnprocessableEntity || de.Code != "VALIDATION_FAILED" {
			t.Fatalf("want the 422 VALIDATION_FAILED DomainError, got %v", err)
		}
		details, _ := de.Details.(map[string]string)
		if details["[1].title"] != "this field is required" || len(details) != 1 {
			t.Fatalf("details %v, want [1].title", de.Details)
		}

		// Both error shapes carry the name: the envelope and problem+json.
		for _, accept := range []string{"application/json", "application/problem+json"} {
			req := httptest.NewRequest(http.MethodPost, "/notes/import", nil)
			req.Header.Set("Accept", accept)
			rec := httptest.NewRecorder()
			gferrors.WriteError(rec, req, err, nil)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s: status %d", accept, rec.Code)
			}
			var got struct {
				Error struct {
					Details map[string]string `json:"details"`
				} `json:"error"`
				Details map[string]string `json:"details"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("%s: %v: %s", accept, err, rec.Body)
			}
			d := got.Error.Details
			if accept == "application/problem+json" {
				d = got.Details
			}
			if d["[1].title"] == "" {
				t.Fatalf("%s: body %s does not name [1].title", accept, rec.Body)
			}
		}
	})

	t.Run("pointer to a slice binds and validates", func(t *testing.T) {
		notes := &[]*note{}
		if err := bind(`[{"title":"a"}]`, &notes); err != nil {
			t.Fatalf("valid: %v", err)
		}
		if len(*notes) != 1 || (*notes)[0].Title != "a" {
			t.Fatalf("bound %+v", *notes)
		}
		var de *gferrors.DomainError
		if err := bind(`[{"title":"a"},{"title":""}]`, &notes); !errors.As(err, &de) || de.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("invalid element: %v", err)
		}
	})

	t.Run("an empty array binds", func(t *testing.T) {
		notes := []note{{Title: "stale"}}
		if err := bind(`[]`, &notes); err != nil {
			t.Fatalf("BindJSON: %v", err)
		}
		if len(notes) != 0 {
			t.Fatalf("bound %+v", notes)
		}
	})

	t.Run("an array of non-structs binds without validation", func(t *testing.T) {
		var tags []string
		if err := bind(`["a",""]`, &tags); err != nil || len(tags) != 2 {
			t.Fatalf("[]string: %v %v", tags, err)
		}
		var ids []int
		if err := bind(`[1,2,3]`, &ids); err != nil || len(ids) != 3 {
			t.Fatalf("[]int: %v %v", ids, err)
		}
		var byName map[string]int
		if err := bind(`{"a":1}`, &byName); err != nil || byName["a"] != 1 {
			t.Fatalf("map: %v %v", byName, err)
		}
	})

	t.Run("a struct behaves as before", func(t *testing.T) {
		var n note
		if err := bind(`{"title":"a"}`, &n); err != nil || n.Title != "a" {
			t.Fatalf("valid struct: %+v %v", n, err)
		}
		var de *gferrors.DomainError
		err := bind(`{"body":"x"}`, &note{})
		if !errors.As(err, &de) || de.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("invalid struct: %v", err)
		}
		if d, _ := de.Details.(map[string]string); d["title"] == "" || len(d) != 1 {
			t.Fatalf("struct details %v, want title", de.Details)
		}
		// An array body into a struct is still the decoder's 400.
		if err := bind(`[{"title":"a"}]`, &note{}); !errors.As(err, &de) || de.StatusCode != http.StatusBadRequest {
			t.Fatalf("array into a struct: %v", err)
		}
	})

	t.Run("malformed JSON and the body cap are unchanged", func(t *testing.T) {
		var notes []note
		var de *gferrors.DomainError
		if err := bind(`[{"title":"a"},`, &notes); !errors.As(err, &de) || de.StatusCode != http.StatusBadRequest {
			t.Fatalf("malformed array: %v", err)
		}
		big := `[{"title":"` + strings.Repeat("x", 2<<20) + `"}]`
		if err := bind(big, &notes); !errors.As(err, &de) || de.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("2 MiB array: %v", err)
		}
	})
}
