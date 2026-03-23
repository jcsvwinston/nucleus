package auth

import "context"

// User represents a minimal authenticated user identity.
type User struct {
	ID          string
	Username    string
	Email       string
	Role        string
	IsSuperuser bool
}

// UserProvider is the interface that applications must implement to integrate
// their user model with GoFrame's authentication system.
type UserProvider interface {
	// FindByID retrieves a user by their unique identifier.
	FindByID(ctx context.Context, id string) (*User, error)
	// FindByUsername retrieves a user by username (used for login).
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByEmail retrieves a user by email address.
	FindByEmail(ctx context.Context, email string) (*User, error)
	// ValidateCredentials checks if the username/password combination is valid.
	// Returns the user if valid, an error otherwise.
	ValidateCredentials(ctx context.Context, username, password string) (*User, error)
}
