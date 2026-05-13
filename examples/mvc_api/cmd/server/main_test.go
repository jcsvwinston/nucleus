package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/config"
	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/services"
	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestExampleMVCAPI_Minimal_Smoke(t *testing.T) {
	a, svc := newMVCAPISmokeApp(t)
	defer a.Shutdown(context.Background())

	if got := svc.CountRows("articles"); got != 1 {
		t.Fatalf("expected seeded article count 1, got %d", got)
	}
	// The default-deny middleware (ADR-004) gates user routes. The
	// example's main() seeds anonymous allows for its public surface;
	// the smoke harness exercises only /openapi.json so we seed that
	// path here to mirror production wiring.
	if err := a.Authorizer.AddPolicy("anonymous", "/openapi.json", "*"); err != nil {
		t.Fatalf("seed anonymous /openapi.json: %v", err)
	}
	if err := a.MountOpenAPI("/openapi.json", exampleOpenAPIDocument); err != nil {
		t.Fatalf("mount openapi: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected openapi smoke 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExampleMVCAPIAdmin_Smoke(t *testing.T) {
	a, _ := newMVCAPISmokeApp(t)
	defer a.Shutdown(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/admin/api/models", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated admin API 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	pageRec := httptest.NewRecorder()
	a.Router.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusFound {
		t.Fatalf("expected admin page redirect, got %d", pageRec.Code)
	}
	if loc := pageRec.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/login") {
		t.Fatalf("expected redirect to admin login, got %q", loc)
	}
}

func newMVCAPISmokeApp(t *testing.T) (*app.App, *services.Services) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Databases["default"] = app.DatabaseConfig{
		URL:         "sqlite://:memory:",
		MaxOpen:     1,
		MaxIdle:     1,
		MaxLifetime: time.Minute,
	}
	cfg.LogLevel = "error"
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	svc, err := services.New(a)
	if err != nil {
		t.Fatalf("new services: %v", err)
	}
	if err := registerModels(a); err != nil {
		t.Fatalf("register models: %v", err)
	}
	return a, svc
}
