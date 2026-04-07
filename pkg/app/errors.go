package app

import (
	"errors"
	"fmt"
)

var (
	// ErrNilConfig indicates New was called with a nil config pointer.
	ErrNilConfig = errors.New("config is nil")
	// ErrNilApp indicates a method was called on a nil *App receiver.
	ErrNilApp = errors.New("app is nil")
	// ErrNotInitialized indicates required app dependencies are missing.
	ErrNotInitialized = errors.New("app is not fully initialized")
	// ErrServerAlreadyRunning indicates Run was invoked while the server is already running.
	ErrServerAlreadyRunning = errors.New("server is already running")
	// ErrModelsRegistryNotInitialized indicates model registration was attempted before models setup.
	ErrModelsRegistryNotInitialized = errors.New("models registry is not initialized")
	// ErrDatabaseAliasNotFound indicates an unknown database alias was requested.
	ErrDatabaseAliasNotFound = errors.New("database alias not found")
	// ErrTenantIsolationViolation indicates tenant routing resolved to a shared DB alias.
	ErrTenantIsolationViolation = errors.New("tenant isolation violation")
)

// OpError wraps an application error with operation context while preserving
// unwrapping semantics for errors.Is / errors.As.
type OpError struct {
	Op  string
	Err error
}

func (e *OpError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("app.%s: %v", e.Op, e.Err)
}

func (e *OpError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrapOp(op string, err error) error {
	if err == nil {
		return nil
	}
	return &OpError{Op: op, Err: err}
}
