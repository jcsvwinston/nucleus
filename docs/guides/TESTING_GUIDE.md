# Testing Guide

Reference date: 2026-05-29.
Status: Current.

This guide covers testing strategies for Nucleus applications, including HTTP handler tests, database fixture patterns, plugin contract tests, and integration tests across multiple database engines.

## Table of Contents

- [Overview](#overview)
- [Running Tests](#running-tests)
- [HTTP Handler Tests](#http-handler-tests)
- [App Container Tests](#app-container-tests)
- [Database Test Fixtures](#database-test-fixtures)
- [Model Tests](#model-tests)
- [Plugin Contract Tests](#plugin-contract-tests)
- [Integration Tests with Multiple DB Engines](#integration-tests-with-multiple-db-engines)
- [Testing Background Tasks](#testing-background-tasks)
- [Test Utilities and Helpers](#test-utilities-and-helpers)

---

## Overview

Nucleus applications should be tested at multiple levels:

| Level | What to Test | Speed |
|-------|-------------|-------|
| **Unit** | Individual functions, validators, helpers | Fastest |
| **Handler** | HTTP handlers with mocked/stubbed dependencies | Fast |
| **Integration** | Full request lifecycle with real DB | Medium |
| **E2E** | CLI commands, full app lifecycle | Slowest |

---

## Running Tests

### Basic test command

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with coverage
go test -cover ./...

# Run specific package
go test ./pkg/auth

# Run specific test
go test ./pkg/router -run TestCORS

# Run with race detection
go test -race ./...
```

### Framework test command

```bash
# Nucleus's built-in test command (runs with framework-friendly defaults)
nucleus test

# Dry run (show what would be tested)
nucleus test --dry-run
```

---

## HTTP Handler Tests

### Testing handlers with `httptest`

```go
package controllers

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestHealthHandler(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
    rec := httptest.NewRecorder()

    HealthHandler(rec, req)

    if rec.Code != http.StatusOK {
        t.Errorf("expected status 200, got %d", rec.Code)
    }

    body := rec.Body.String()
    if !strings.Contains(body, "ok") {
        t.Errorf("expected 'ok' in body, got %s", body)
    }
}
```

### Testing handlers with router context

```go
package controllers

import (
    "encoding/json"
    "log/slog"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/jcsvwinston/nucleus/pkg/router"
)

func TestListArticlesHandler(t *testing.T) {
    r := router.New(slog.Default())
    r.Get("/api/articles", ListArticlesHandler)

    req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
    rec := httptest.NewRecorder()

    r.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", rec.Code)
    }

    // Parse JSON response
    var articles []map[string]any
    if err := json.Unmarshal(rec.Body.Bytes(), &articles); err != nil {
        t.Fatalf("failed to parse JSON: %v", err)
    }
    if len(articles) != 0 {
        t.Errorf("expected empty list, got %d articles", len(articles))
    }
}
```

### Testing handlers with session context

```go
import (
    "testing"

    "github.com/alexedwards/scs/v2"
)

func TestAdminHandler_RequiresSession(t *testing.T) {
    // Create session manager with memory store
    sessionManager := scs.New()
    sessionManager.Lifetime = 30 * time.Minute

    // Create request with session
    req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)

    // Load session (in real app, middleware does this)
    ctx := sessionManager.LoadAndSave(nil)
    req = req.WithContext(ctx)

    rec := httptest.NewRecorder()
    AdminDashboardHandler(rec, req)

    // Should redirect to login if not authenticated
    if rec.Code != http.StatusSeeOther {
        t.Errorf("expected redirect, got %d", rec.Code)
    }
}
```

### Testing handlers with JWT context

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/observe"
)

func TestProfileHandler_WithJWTContext(t *testing.T) {
    // Create context with user info (simulating JWT middleware)
    ctx := context.Background()
    ctx = observe.CtxWithUserID(ctx, "user-123")
    ctx = observe.CtxWithRequestID(ctx, "req-abc")

    req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
    req = req.WithContext(ctx)

    rec := httptest.NewRecorder()
    ProfileHandler(rec, req)

    if rec.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", rec.Code)
    }
}
```

---

## App Container Tests

### Testing with full app container

```go
package internal

import (
    "testing"

    "github.com/jcsvwinston/nucleus/pkg/app"
)

func TestAppWiring(t *testing.T) {
    cfg := &app.Config{
        Host:            "127.0.0.1",
        Port:            0, // Random port
        DatabaseDefault: "default",
        Databases: map[string]app.DatabaseConfig{
            "default": {
                URL: "sqlite://file::memory:?cache=shared",
            },
        },
    }

    application, err := app.New(cfg)
    if err != nil {
        t.Fatalf("app.New failed: %v", err)
    }
    defer application.Shutdown()

    // Verify wiring
    if application.DB == nil {
        t.Error("expected DB to be wired")
    }
    if application.Router == nil {
        t.Error("expected Router to be wired")
    }
    if application.Logger == nil {
        t.Error("expected Logger to be wired")
    }
}
```

### Testing with custom components

```go
func TestApp_WithCustomLogger(t *testing.T) {
    var logOutput bytes.Buffer
    logger := slog.New(slog.NewTextHandler(&logOutput, nil))

    cfg := &app.Config{
        // ... config
    }

    // app.New wires logger from config
    // Verify custom logger is used
    application, err := app.New(cfg)
    if err != nil {
        t.Fatalf("app.New failed: %v", err)
    }

    application.Logger.Info("test message")

    if !strings.Contains(logOutput.String(), "test message") {
        t.Error("expected custom logger to capture message")
    }
}
```

---

## Database Test Fixtures

### Using SQLite in-memory databases

```go
package model

import (
    "database/sql"
    "testing"

    _ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
    t.Helper()

    db, err := sql.Open("sqlite", "file::memory:?cache=shared")
    if err != nil {
        t.Fatalf("failed to open test DB: %v", err)
    }
    t.Cleanup(func() { db.Close() })

    // Create test tables
    _, err = db.Exec(`
        CREATE TABLE articles (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT NOT NULL,
            body TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `)
    if err != nil {
        t.Fatalf("failed to create table: %v", err)
    }

    return db
}
```

### Fixture loading pattern

```go
func loadFixture(t *testing.T, db *sql.DB, file string) {
    t.Helper()

    data, err := os.ReadFile(file)
    if err != nil {
        t.Fatalf("failed to read fixture: %v", err)
    }

    // Execute SQL fixture
    _, err = db.Exec(string(data))
    if err != nil {
        t.Fatalf("failed to load fixture: %v", err)
    }
}

func TestArticleModel_FindAll(t *testing.T) {
    db := setupTestDB(t)
    loadFixture(t, db, "testdata/articles.sql")

    model := NewArticleModel(db)
    articles, err := model.FindAll()
    if err != nil {
        t.Fatalf("FindAll failed: %v", err)
    }

    if len(articles) != 3 {
        t.Errorf("expected 3 articles, got %d", len(articles))
    }
}
```

### JSON fixture pattern (using Nucleus dumpdata/loaddata)

```json
// testdata/articles.json
[
    {
        "model": "article",
        "pk": 1,
        "fields": {
            "title": "First Article",
            "body": "Hello World",
            "created_at": "2026-01-01T00:00:00Z"
        }
    },
    {
        "model": "article",
        "pk": 2,
        "fields": {
            "title": "Second Article",
            "body": "Testing is important",
            "created_at": "2026-01-02T00:00:00Z"
        }
    }
]
```

### Table-driven tests for CRUD

```go
func TestArticleCRUD(t *testing.T) {
    db := setupTestDB(t)
    model := NewArticleModel(db)

    tests := []struct {
        name  string
        setup func(t *testing.T) *Article
        check func(t *testing.T, got *Article, err error)
    }{
        {
            name: "create article",
            setup: func(t *testing.T) *Article {
                return &Article{Title: "Test", Body: "Content"}
            },
            check: func(t *testing.T, got *Article, err error) {
                if err != nil {
                    t.Fatalf("unexpected error: %v", err)
                }
                if got.ID == 0 {
                    t.Error("expected ID to be set")
                }
                if got.Title != "Test" {
                    t.Errorf("expected title 'Test', got %q", got.Title)
                }
            },
        },
        {
            name: "find non-existent article",
            setup: func(t *testing.T) *Article { return nil },
            check: func(t *testing.T, got *Article, err error) {
                if err == nil {
                    t.Fatal("expected error for missing article")
                }
                // model.CRUD returns a *errors.DomainError (pkg/errors) with
                // a 404 status when a row is not found — there is no sentinel.
                var derr *errors.DomainError
                if !stderrors.As(err, &derr) || derr.StatusCode != http.StatusNotFound {
                    t.Errorf("expected a 404 NOT_FOUND DomainError, got %v", err)
                }
            },
        },
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            // ... run test
        })
    }
}
```

---

## Model Tests

### Testing model metadata

```go
import "github.com/jcsvwinston/nucleus/pkg/model"

func TestArticleModel_Metadata(t *testing.T) {
    article := Article{}
    meta, err := model.ExtractMeta(&article)
    if err != nil {
        t.Fatalf("ExtractMeta failed: %v", err)
    }

    if meta.Table != "articles" {
        t.Errorf("expected table 'articles', got %q", meta.Table)
    }

    if len(meta.Fields) == 0 {
        t.Error("expected fields to be extracted")
    }

    // PrimaryKey is the name of the PK field (e.g. "ID").
    if meta.PrimaryKey != "ID" {
        t.Errorf("expected PK field 'ID', got %q", meta.PrimaryKey)
    }
}
```

### Testing model hooks

```go
func TestArticleModel_BeforeSave(t *testing.T) {
    article := &Article{Title: "Test"}

    // Trigger hook
    err := article.BeforeSave()
    if err != nil {
        t.Fatalf("BeforeSave failed: %v", err)
    }

    // Check slug was generated
    if article.Slug != "test" {
        t.Errorf("expected slug 'test', got %q", article.Slug)
    }
}
```

---

## Plugin Contract Tests

### Testing mail plugins

```go
import "github.com/jcsvwinston/nucleus/pkg/plugins"

func TestMailPlugin_Contract(t *testing.T) {
    // Create test plugin binary path
    pluginPath := filepath.Join(t.TempDir(), "nucleus-plugin-testmail")
    buildTestPlugin(t, pluginPath, mailPluginSource)

    // Probe capabilities
    caps, err := plugins.ProbeCapabilities(context.Background(), pluginPath, 5*time.Second)
    if err != nil {
        t.Fatalf("ProbeCapabilities failed: %v", err)
    }

    if !contains(caps, "mail.send") {
        t.Errorf("expected mail.send capability, got %v", caps)
    }

    // Execute mail.send
    request, _ := plugins.NewRequestEnvelope(
        "testmail",
        plugins.CapabilityMailSend,
        5*time.Second,
        plugins.MailSendPayload{
            From:    "test@example.com",
            To:      []string{"recipient@example.com"},
            Subject: "Test",
            Body:    "Hello",
        },
        nil,
    )

    response, err := plugins.ExecuteRequest(context.Background(), pluginPath, request, 5*time.Second)
    if err != nil {
        t.Fatalf("ExecuteRequest failed: %v", err)
    }
    if !response.OK {
        t.Fatalf("expected OK response, got error: %+v", response.Error)
    }
}
```

### Building test plugins

```go
func buildTestPlugin(t *testing.T, path, source string) {
    t.Helper()

    // Write Go source
    srcDir := t.TempDir()
    srcFile := filepath.Join(srcDir, "main.go")
    if err := os.WriteFile(srcFile, []byte(source), 0644); err != nil {
        t.Fatalf("write source failed: %v", err)
    }

    // Write go.mod
    modFile := filepath.Join(srcDir, "go.mod")
    if err := os.WriteFile(modFile, []byte("module testplugin\n\ngo 1.26"), 0644); err != nil {
        t.Fatalf("write go.mod failed: %v", err)
    }

    // Build
    cmd := exec.Command("go", "build", "-o", path, srcFile)
    cmd.Dir = srcDir
    if output, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("build failed: %v\n%s", err, output)
    }
}

const mailPluginSource = `package main

import (
    "encoding/json"
    "fmt"
    "os"
)

func main() {
    if len(os.Args) > 1 && os.Args[1] == "capabilities" {
        fmt.Println(` + "`" + `{"capabilities":["mail.send"]}` + "`" + `)
        return
    }

    var req struct {
        Payload json.RawMessage ` + "`" + `json:"payload"` + "`" + `
    }
    json.NewDecoder(os.Stdin).Decode(&req)

    resp := map[string]any{
        "version": "v1",
        "ok":      true,
        "output":  map[string]any{"accepted": true},
    }
    json.NewEncoder(os.Stdout).Encode(resp)
}`
```

---

## Integration Tests with Multiple DB Engines

### SQL matrix testing

Nucleus supports testing across multiple database engines using environment variables:

```bash
# PostgreSQL
NUCLEUS_SQL_MATRIX_URL="postgres://user:pass@localhost/nucleus_test" go test ./...

# MySQL
NUCLEUS_SQL_MATRIX_URL="mysql://user:pass@localhost/nucleus_test" go test ./...

# MS SQL Server (exploratory)
NUCLEUS_SQL_EXPLORATORY_URL="sqlserver://user:pass@localhost/nucleus_test" go test ./...

# Oracle (exploratory)
NUCLEUS_SQL_EXPLORATORY_URL="oracle://user:pass@localhost/nucleus_test" go test ./...
```

### Writing DB-agnostic tests

```go
func TestMigrations_AcrossEngines(t *testing.T) {
    dbURL := os.Getenv("NUCLEUS_SQL_MATRIX_URL")
    if dbURL == "" {
        t.Skip("NUCLEUS_SQL_MATRIX_URL not set")
    }

    // Run migrations
    err := runMigrations(dbURL)
    if err != nil {
        t.Fatalf("migrations failed: %v", err)
    }

    // Verify schema
    tables, err := listTables(dbURL)
    if err != nil {
        t.Fatalf("list tables failed: %v", err)
    }

    expectedTables := []string{
        "nucleus_migrations",
        "articles",
        "users",
    }

    for _, expected := range expectedTables {
        if !contains(tables, expected) {
            t.Errorf("expected table %q not found in %v", expected, tables)
        }
    }
}
```

### Fixture server testing

```go
func TestFixtureServer(t *testing.T) {
    // Load fixtures and start server for integration testing
    // This is what `nucleus testserver` does internally
}
```

---

## Testing Background Tasks

### Testing task handlers

```go
import (
    "context"
    "testing"

    "github.com/hibiken/asynq"
)

func TestSendWelcomeEmailTask(t *testing.T) {
    // Create task with JSON payload
    payload, _ := json.Marshal(map[string]string{
        "email": "alice@example.com",
        "name":  "Alice",
    })
    task := asynq.NewTask("emails.send_welcome", payload)

    // Execute handler directly (no need for full worker runtime)
    handler := NewEmailTaskHandler()
    err := handler.ProcessTask(context.Background(), task)

    if err != nil {
        t.Fatalf("task handler failed: %v", err)
    }

    // Verify side effects (e.g., email sent)
    // Use a mock email sender in production tests
}
```

### Testing task enqueue

```go
import (
    "os"
    "testing"

    "github.com/jcsvwinston/nucleus/pkg/tasks"
    asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

func TestTaskEnqueue(t *testing.T) {
    // Requires Redis connection
    redisURL := os.Getenv("REDIS_URL")
    if redisURL == "" {
        t.Skip("REDIS_URL not set")
    }

    // tasks.Manager is an interface; the Redis-backed implementation lives in
    // the Asynq provider.
    var mgr tasks.Manager
    mgr, err := asynqprovider.NewManager(tasks.Config{
        RedisURL:    redisURL,
        Concurrency: 1,
    }, nil)
    if err != nil {
        t.Fatalf("NewManager failed: %v", err)
    }
    defer mgr.Close()

    // EnqueueJSON returns the new task's id as a string.
    id, err := mgr.EnqueueJSON("emails.send_welcome", map[string]string{
        "email": "test@example.com",
    })
    if err != nil {
        t.Fatalf("enqueue failed: %v", err)
    }

    if id == "" {
        t.Error("expected task ID")
    }
}
```

---

## Test Utilities and Helpers

### Test server helper

```go
// testserver.go - helper to start test server
type TestServer struct {
    URL    string
    App    *app.App
    Cancel context.CancelFunc
}

func NewTestServer(t *testing.T, cfg *app.Config) *TestServer {
    t.Helper()

    ctx, cancel := context.WithCancel(context.Background())
    application, err := app.New(cfg)
    if err != nil {
        t.Fatalf("app.New failed: %v", err)
    }

    ts := &TestServer{
        App:    application,
        Cancel: cancel,
    }

    // Start server on random port
    go func() {
        application.Run(ctx)
    }()

    // Wait for health check
    waitForHealth(t, application.Config.Port)

    t.Cleanup(func() {
        cancel()
        application.Shutdown()
    })

    return ts
}

func (ts *TestServer) Get(path string) (*http.Response, error) {
    return http.Get(ts.URL + path)
}
```

### Mock email sender

```go
type MockSender struct {
    mu     sync.Mutex
    Sent   []mail.Message
    Err    error
}

func (m *MockSender) Send(ctx context.Context, msg mail.Message) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.Err != nil {
        return m.Err
    }
    m.Sent = append(m.Sent, msg)
    return nil
}

func (m *MockSender) SentCount() int {
    m.mu.Lock()
    defer m.mu.Unlock()
    return len(m.Sent)
}
```

---

## Quick Reference

```bash
# Run all tests
go test ./...

# Run with coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run race detector
go test -race ./...

# Run SQL matrix tests
NUCLEUS_SQL_MATRIX_URL="postgres://..." go test ./...

# Run fixture server test
nucleus testserver --fixtures testdata/fixtures

# CLI integration tests
nucleus test
```
