package validate

import (
	"errors"
	"testing"

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
