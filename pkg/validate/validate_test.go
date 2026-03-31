package validate

import (
	"errors"
	"testing"

	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
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
