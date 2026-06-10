package router

import (
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

type BindFormEmbedded struct {
	ID uint `json:"id"`
}

type bindFormTarget struct {
	BindFormEmbedded
	Subject   string     `json:"subject" validate:"required,min=3"`
	Priority  string     `json:"priority" validate:"omitempty,oneof=low normal high urgent"`
	Requester uint       `json:"requester_id"`
	Battery   float64    `json:"battery_pct"`
	Active    bool       `json:"active"`
	Renamed   string     `form:"alias" json:"ignored_json_name"`
	Skipped   string     `form:"-"`
	DueAt     time.Time  `json:"due_at"`
	AckedAt   *time.Time `json:"acked_at"`
	Count     *int       `json:"count"`
}

func formRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestBindFormTypedBinding(t *testing.T) {
	r := formRequest(t, url.Values{
		"id":           {"7"},
		"subject":      {"Device offline"},
		"priority":     {"high"},
		"requester_id": {"42"},
		"battery_pct":  {"17.5"},
		"active":       {"on"}, // checkbox convention
		"alias":        {"via form tag"},
		"due_at":       {"2026-06-10T18:30"}, // datetime-local format
		"acked_at":     {"2026-06-09"},
		"count":        {"3"},
		"unknown_key":  {"ignored"},
		"Skipped":      {"must not bind"},
	})

	var got bindFormTarget
	if err := BindForm(r, &got); err != nil {
		t.Fatalf("BindForm: %v", err)
	}

	if got.ID != 7 {
		t.Errorf("embedded ID = %d, want 7", got.ID)
	}
	if got.Subject != "Device offline" || got.Priority != "high" {
		t.Errorf("strings = %q/%q", got.Subject, got.Priority)
	}
	if got.Requester != 42 {
		t.Errorf("uint Requester = %d, want 42", got.Requester)
	}
	if got.Battery != 17.5 {
		t.Errorf("float Battery = %v, want 17.5", got.Battery)
	}
	if !got.Active {
		t.Error("bool Active: checkbox \"on\" did not bind as true")
	}
	if got.Renamed != "via form tag" {
		t.Errorf("form tag should win over json tag, got %q", got.Renamed)
	}
	if want := time.Date(2026, 6, 10, 18, 30, 0, 0, time.UTC); !got.DueAt.Equal(want) {
		t.Errorf("DueAt = %v, want %v", got.DueAt, want)
	}
	if got.AckedAt == nil || !got.AckedAt.Equal(time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("*time.Time AckedAt = %v", got.AckedAt)
	}
	if got.Count == nil || *got.Count != 3 {
		t.Errorf("*int Count = %v", got.Count)
	}
	if got.Skipped != "" {
		t.Errorf(`form:"-" field bound anyway: %q`, got.Skipped)
	}
}

func TestBindFormEmptyValuesLeaveZero(t *testing.T) {
	r := formRequest(t, url.Values{
		"subject":      {"Optional numerics submit cleanly"},
		"requester_id": {""},
		"battery_pct":  {""},
		"due_at":       {""},
	})
	var got bindFormTarget
	if err := BindForm(r, &got); err != nil {
		t.Fatalf("BindForm with empty optional values: %v", err)
	}
	if got.Requester != 0 || got.Battery != 0 || !got.DueAt.IsZero() {
		t.Errorf("empty values must leave zero values, got %+v", got)
	}
}

func TestBindFormCaseInsensitiveFallback(t *testing.T) {
	type target struct{ Carrier string }
	r := formRequest(t, url.Values{"carrier": {"vodafone"}})
	var got target
	if err := BindForm(r, &got); err != nil {
		t.Fatalf("BindForm: %v", err)
	}
	if got.Carrier != "vodafone" {
		t.Errorf("case-insensitive field match failed, got %q", got.Carrier)
	}
}

func TestBindFormRunsValidation(t *testing.T) {
	r := formRequest(t, url.Values{"subject": {"ok"}}) // min=3 fails
	var got bindFormTarget
	err := BindForm(r, &got)
	if err == nil {
		t.Fatal("BindForm must run validate tags like BindJSON; got nil error")
	}
	var domErr *gferrors.DomainError
	if !errors.As(err, &domErr) {
		t.Fatalf("validation failure must surface as *DomainError, got %T: %v", err, err)
	}
}

func TestBindFormTypeMismatchIsBadRequest(t *testing.T) {
	r := formRequest(t, url.Values{"subject": {"valid subject"}, "requester_id": {"not-a-number"}})
	var got bindFormTarget
	err := BindForm(r, &got)
	var domErr *gferrors.DomainError
	if err == nil || !errors.As(err, &domErr) {
		t.Fatalf("want *DomainError for uint conversion failure, got %T: %v", err, err)
	}
	if domErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", domErr.StatusCode)
	}
}

func TestBindFormRejectsNonStructTarget(t *testing.T) {
	r := formRequest(t, url.Values{"subject": {"x"}})
	var s string
	if err := BindForm(r, &s); err == nil {
		t.Error("non-struct pointer target must be rejected")
	}
	if err := BindForm(r, nil); err == nil {
		t.Error("nil target must be rejected")
	}
}

func TestBindFormMultipart(t *testing.T) {
	var body strings.Builder
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("subject", "From multipart")
	_ = mw.WriteField("requester_id", "9")
	_ = mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(body.String()))
	r.Header.Set("Content-Type", mw.FormDataContentType())

	var got bindFormTarget
	if err := BindForm(r, &got); err != nil {
		t.Fatalf("BindForm multipart: %v", err)
	}
	if got.Subject != "From multipart" || got.Requester != 9 {
		t.Errorf("multipart binding got %+v", got)
	}
}
