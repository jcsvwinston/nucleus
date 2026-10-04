package validate

import (
	"errors"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

type testUser struct {
	Email string `json:"email" validate:"required,email"`
	Name  string `json:"name" validate:"required,min=2"`
	Age   int    `json:"age" validate:"gte=0,lte=150"`
}

func TestValidate_Valid(t *testing.T) {
	u := testUser{Email: "test@example.com", Name: "Alice", Age: 30}
	if err := Validate(u); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidate_Invalid(t *testing.T) {
	u := testUser{Email: "not-an-email", Name: "A", Age: -1}
	err := Validate(u)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var domErr *gferrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatal("expected *DomainError")
	}
	if domErr.Code != "VALIDATION_FAILED" {
		t.Errorf("expected VALIDATION_FAILED, got %s", domErr.Code)
	}
	if domErr.StatusCode != 422 {
		t.Errorf("expected 422, got %d", domErr.StatusCode)
	}

	details, ok := domErr.Details.(map[string]string)
	if !ok {
		t.Fatal("expected map[string]string details")
	}
	if _, exists := details["email"]; !exists {
		t.Error("expected email field in details")
	}
	if _, exists := details["name"]; !exists {
		t.Error("expected name field in details")
	}
}

func TestValidate_RequiredMissing(t *testing.T) {
	u := testUser{}
	err := Validate(u)
	if err == nil {
		t.Fatal("expected error for empty required fields")
	}
}

func TestBoundMessagesSayWhatTheBoundCounts(t *testing.T) {
	var in struct {
		Page  int      `json:"page" validate:"min=1"`
		Name  string   `json:"name" validate:"min=3"`
		Tags  []string `json:"tags" validate:"min=2"`
		Limit int      `json:"limit" validate:"max=10"`
	}
	in.Name, in.Tags, in.Limit = "ab", []string{"x"}, 11
	err := Validate(&in)
	var de *gferrors.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Validate = %v", err)
	}
	got, _ := de.Details.(map[string]string)
	want := map[string]string{
		"page":  "must be at least 1",
		"name":  "must be at least 3 characters",
		"tags":  "must be at least 2 items",
		"limit": "must be at most 10",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %q, want %q", k, got[k], w)
		}
	}
}

// NU-107: a JSON body can be an array, so the binders hand Validate a
// slice. It used to go straight to the struct validator, which refuses
// anything that is not a struct, and every array body was a 400.

type note struct {
	Title string `json:"title" validate:"required"`
	Body  string `json:"body" validate:"max=5"`
}

func validationDetails(t *testing.T, err error) map[string]string {
	t.Helper()
	var de *gferrors.DomainError
	if !errors.As(err, &de) || de.Code != "VALIDATION_FAILED" || de.StatusCode != 422 {
		t.Fatalf("want the 422 VALIDATION_FAILED DomainError, got %v", err)
	}
	details, ok := de.Details.(map[string]string)
	if !ok {
		t.Fatalf("details: %#v", de.Details)
	}
	return details
}

func TestValidate_SliceOfStructsIsValidatedElementByElement(t *testing.T) {
	valid := []note{{Title: "a"}, {Title: "b", Body: "ok"}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid slice: %v", err)
	}
	if err := Validate(&valid); err != nil {
		t.Fatalf("pointer to a valid slice: %v", err)
	}

	invalid := []note{{Title: "a"}, {Body: "too long"}, {Title: "c"}}
	for _, v := range []any{invalid, &invalid, [3]note(invalid)} {
		got := validationDetails(t, Validate(v))
		want := map[string]string{
			"[1].title": "this field is required",
			"[1].body":  "must be at most 5 characters",
		}
		if len(got) != len(want) {
			t.Fatalf("%T: details %v, want %v", v, got, want)
		}
		for k, w := range want {
			if got[k] != w {
				t.Errorf("%T: %s = %q, want %q (all: %v)", v, k, got[k], w, got)
			}
		}
	}
}

func TestValidate_NestedAndPointerElements(t *testing.T) {
	ptrs := []*note{{Title: "a"}, nil, {}}
	got := validationDetails(t, Validate(ptrs))
	if _, ok := got["[2].title"]; !ok || len(got) != 1 {
		t.Fatalf("[]*note: %v (a nil element passes, [2] fails)", got)
	}

	nested := [][]note{{{Title: "a"}}, {{Title: "b"}, {}}}
	got = validationDetails(t, Validate(nested))
	if _, ok := got["[1][1].title"]; !ok || len(got) != 1 {
		t.Fatalf("[][]note: %v", got)
	}

	byKey := map[string]note{"ok": {Title: "a"}, "bad": {}}
	got = validationDetails(t, Validate(byKey))
	if _, ok := got["[bad].title"]; !ok || len(got) != 1 {
		t.Fatalf("map[string]note: %v", got)
	}

	// What encoding/json decodes into an `any`: a []any of maps.
	var decoded any = []any{map[string]any{"title": ""}}
	if err := Validate(&decoded); err != nil {
		t.Fatalf("a decoded []any carries no tags: %v", err)
	}
}

func TestValidate_ValuesWithoutTagsPass(t *testing.T) {
	type tree []tree
	for _, v := range []any{
		[]string{"a", ""},
		&[]int{1, 2},
		[]note{},
		&[]note{},
		[]note(nil),
		map[string]int{"a": 1},
		map[string]any{"title": ""},
		[]time.Time{{}},
		tree{tree{}},
		"plain",
		42,
	} {
		if err := Validate(v); err != nil {
			t.Errorf("Validate(%#v) = %v, want nil", v, err)
		}
	}
}

func TestValidate_StructPathUnchanged(t *testing.T) {
	got := validationDetails(t, Validate(&note{Body: "too long"}))
	if got["title"] != "this field is required" || got["body"] != "must be at most 5 characters" || len(got) != 2 {
		t.Fatalf("struct details: %v", got)
	}
	// A nil value or a nil pointer is still the 400 it was.
	for _, v := range []any{nil, (*note)(nil), (*[]note)(nil)} {
		var de *gferrors.DomainError
		if err := Validate(v); !errors.As(err, &de) || de.StatusCode != 400 {
			t.Errorf("Validate(%#v) = %v, want the 400", v, err)
		}
	}
}
