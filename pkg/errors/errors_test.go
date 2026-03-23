package errors

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotFound(t *testing.T) {
	err := NotFound("User", "123")
	if err.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %s", err.Code)
	}
	if err.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", err.StatusCode)
	}
	if err.Error() != "User '123' not found" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("invalid input")
	if err.Code != "BAD_REQUEST" || err.StatusCode != 400 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("token expired")
	if err.Code != "UNAUTHORIZED" || err.StatusCode != 401 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestForbidden(t *testing.T) {
	err := Forbidden("access denied")
	if err.Code != "FORBIDDEN" || err.StatusCode != 403 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestConflict(t *testing.T) {
	err := Conflict("already exists")
	if err.Code != "CONFLICT" || err.StatusCode != 409 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestInternalError(t *testing.T) {
	err := InternalError("something broke")
	if err.Code != "INTERNAL_ERROR" || err.StatusCode != 500 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestValidationFailed(t *testing.T) {
	fields := map[string]string{"email": "required"}
	err := ValidationFailed(fields)
	if err.Code != "VALIDATION_FAILED" || err.StatusCode != 422 {
		t.Errorf("unexpected: %+v", err)
	}
	details, ok := err.Details.(map[string]string)
	if !ok || details["email"] != "required" {
		t.Errorf("unexpected details: %v", err.Details)
	}
}

func TestErrorsAs(t *testing.T) {
	err := NotFound("User", "1")
	var domErr *DomainError
	if !errors.As(err, &domErr) {
		t.Error("errors.As should match *DomainError")
	}
}

func TestWithDetails(t *testing.T) {
	err := BadRequest("bad").WithDetails(map[string]string{"key": "val"})
	if err.Details == nil {
		t.Error("expected details")
	}
}

func TestWriteError_DomainError(t *testing.T) {
	w := httptest.NewRecorder()
	err := NotFound("User", "42")
	WriteError(w, err, nil)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %s", resp.Error.Code)
	}
}

func TestWriteError_GenericError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, errors.New("oops"), nil)

	if w.Code != 500 {
		t.Errorf("expected 500, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", resp.Error.Code)
	}
	// Should not leak internal error message
	if resp.Error.Message == "oops" {
		t.Error("should not leak internal error message")
	}
}
