// Package accounts is the part of authentication an end user touches:
// registering, confirming an address, signing in and out, recovering a
// password, and being locked out after enough wrong guesses.
//
// Nucleus already had the substrate — a chain of credential backends, a
// session manager, password hashing, a policy engine — and none of the
// product. Every application built the same six handlers on top of it, and
// each one re-decided how long a reset token lives, whether it can be used
// twice, and what a failed login reveals. Those are not application
// decisions; they are the decisions an authentication library exists to
// have made once, correctly.
//
// # What this package owns and what it does not
//
// It owns the FLOWS and their security properties: single-use tokens stored
// as hashes, answers that do not disclose whether an address is registered,
// session rotation on sign-in, progressive lockout, and re-authentication
// before a password change.
//
// It does not own where accounts live. Store is the port; SQLStore is an
// implementation with its own tables for applications that do not have one
// yet, and an application with an existing users table implements the same
// three-method-plus-tokens interface against it.
package accounts

import (
	"context"
	"errors"
	"time"
)

// Errors a caller is expected to distinguish. Everything else is wrapped
// with context and reported as a server-side failure.
var (
	// ErrNotFound reports that no account matches. Handlers must not turn
	// it into a user-visible "no such account": that is the disclosure
	// this package exists to avoid.
	ErrNotFound = errors.New("accounts: no such account")
	// ErrInvalidCredentials is returned for a wrong password AND for an
	// unknown account, deliberately indistinguishable.
	ErrInvalidCredentials = errors.New("accounts: invalid credentials")
	// ErrAccountLocked reports too many recent failures for this identity.
	ErrAccountLocked = errors.New("accounts: too many attempts, try again later")
	// ErrEmailNotVerified reports a sign-in attempt on an account whose
	// address was never confirmed, when the configuration requires it.
	ErrEmailNotVerified = errors.New("accounts: email address is not verified")
	// ErrAccountDisabled reports an account an operator turned off.
	ErrAccountDisabled = errors.New("accounts: account is disabled")
	// ErrInvalidToken covers a token that never existed, already ran out,
	// or was already used. One error, because telling them apart tells an
	// attacker which guess was closer.
	ErrInvalidToken = errors.New("accounts: invalid or expired token")
	// ErrWeakPassword reports a password below the configured minimum.
	ErrWeakPassword = errors.New("accounts: password is too short")
	// ErrEmailTaken reports a duplicate registration to the STORE layer.
	// The service never returns it to a caller: a registration form that
	// says "already taken" is an account enumeration oracle.
	ErrEmailTaken = errors.New("accounts: email is already registered")
)

// Account is one registered identity.
type Account struct {
	ID           string
	Email        string
	Username     string
	PasswordHash string
	// EmailVerified records whether the address was confirmed. An account
	// created by an operator may start verified; one that registered
	// itself does not.
	EmailVerified bool
	// Disabled is an operator switch. A disabled account cannot sign in
	// and cannot recover a password.
	Disabled  bool
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TokenPurpose says what a single-use token is for. A token issued to
// verify an address must not be usable to reset a password, so the purpose
// is part of what is looked up rather than a comment on the row.
type TokenPurpose string

const (
	// PurposeVerifyEmail confirms an address at registration.
	PurposeVerifyEmail TokenPurpose = "verify_email"
	// PurposeResetPassword authorises setting a new password.
	PurposeResetPassword TokenPurpose = "reset_password"
	// PurposeMagicLink signs in without a password.
	PurposeMagicLink TokenPurpose = "magic_link"
)

// Token is a single-use credential sent out of band.
//
// Hash is stored, never the token itself: the rows are as readable as any
// other table, and a leaked database must not hand over live password-reset
// links. The token is a secret with the same weight as a password, and it
// is treated like one.
type Token struct {
	Hash      string
	AccountID string
	Purpose   TokenPurpose
	ExpiresAt time.Time
	CreatedAt time.Time
	UsedAt    time.Time
}

// Store is where accounts, their single-use tokens and their failed-attempt
// counters live.
//
// The contract has three properties the flows depend on, and an
// implementation that breaks any of them breaks a security property rather
// than a feature:
//
//  1. Create reports ErrEmailTaken rather than overwriting.
//  2. ConsumeToken is ATOMIC: two concurrent uses of the same token return
//     it exactly once. A reset link that works twice is a reset link an
//     attacker can replay after the owner has used it.
//  3. RecordFailure returns the count INCLUDING the failure it just
//     recorded, so a caller cannot be off by one about a lockout.
type Store interface {
	Create(ctx context.Context, account Account) (Account, error)
	ByID(ctx context.Context, id string) (Account, error)
	ByEmail(ctx context.Context, email string) (Account, error)
	Update(ctx context.Context, account Account) error

	CreateToken(ctx context.Context, token Token) error
	// ConsumeToken atomically marks a token used and returns it. It
	// returns ErrInvalidToken for a token that is unknown, expired, of
	// another purpose, or already used.
	ConsumeToken(ctx context.Context, purpose TokenPurpose, hash string) (Token, error)
	// DeleteTokens removes every outstanding token of a purpose for an
	// account — what a completed reset does to the links it superseded.
	DeleteTokens(ctx context.Context, accountID string, purpose TokenPurpose) error

	RecordFailure(ctx context.Context, key string, now time.Time) (int, error)
	ClearFailures(ctx context.Context, key string) error
	// FailureCount reports recent failures without recording one, for the
	// check that happens BEFORE a password is verified.
	FailureCount(ctx context.Context, key string, now time.Time) (int, error)
}
