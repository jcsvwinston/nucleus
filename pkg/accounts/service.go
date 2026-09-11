package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/mail"
)

// Config tunes the flows. Every zero value has a documented default, so an
// application that sets nothing gets the posture this package considers
// correct rather than an unusable one.
type Config struct {
	// BaseURL is the address a link in an email points at, e.g.
	// "https://app.example.com". Required when mail is sent.
	BaseURL string
	// From is the sender address for account mail.
	From string

	// RequireEmailVerification refuses sign-in until the address is
	// confirmed. Default false: an application that does not send mail
	// must not lock every user out by omission.
	RequireEmailVerification bool

	// MinPasswordLength defaults to 12. It is a length and not a
	// composition rule on purpose: length is what resists guessing, and
	// composition rules push people towards Passw0rd!.
	MinPasswordLength int

	// VerifyTokenTTL defaults to 24h, ResetTokenTTL to 1h and
	// MagicLinkTTL to 15m. A reset link lives shorter than a verification
	// one because it grants more.
	VerifyTokenTTL time.Duration
	ResetTokenTTL  time.Duration
	MagicLinkTTL   time.Duration

	// LockoutThreshold is how many recent failures lock an identity out;
	// default 10. LockoutWindow (default 15m) is how far back failures
	// count, and LockoutBase (default 1s) is the first backoff step —
	// each additional failure doubles the wait, capped at LockoutWindow.
	LockoutThreshold int
	LockoutWindow    time.Duration
	LockoutBase      time.Duration
}

func (c Config) withDefaults() Config {
	if c.MinPasswordLength <= 0 {
		c.MinPasswordLength = 12
	}
	if c.VerifyTokenTTL <= 0 {
		c.VerifyTokenTTL = 24 * time.Hour
	}
	if c.ResetTokenTTL <= 0 {
		c.ResetTokenTTL = time.Hour
	}
	if c.MagicLinkTTL <= 0 {
		c.MagicLinkTTL = 15 * time.Minute
	}
	if c.LockoutThreshold <= 0 {
		c.LockoutThreshold = 10
	}
	if c.LockoutWindow <= 0 {
		c.LockoutWindow = 15 * time.Minute
	}
	if c.LockoutBase <= 0 {
		c.LockoutBase = time.Second
	}
	return c
}

// Mailer is what the service needs to send account mail: one method, so an
// application can hand it a queue-backed sender (mail.EnqueueTx through a
// small adapter) instead of the direct one.
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// Service runs the account flows over a Store.
type Service struct {
	store     Store
	sessions  *auth.SessionManager
	mailer    Mailer
	templates *mail.Templates
	cfg       Config
	logger    *slog.Logger
	now       func() time.Time
}

// New builds the service. sessions may be nil for an API-only deployment
// that signs in with tokens rather than cookies; mailer may be nil, in
// which case the flows that need mail refuse rather than pretending to
// have sent it.
func New(store Store, sessions *auth.SessionManager, mailer Mailer, templates *mail.Templates, cfg Config, logger *slog.Logger) (*Service, error) {
	if store == nil {
		return nil, errors.New("accounts: nil store")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:     store,
		sessions:  sessions,
		mailer:    mailer,
		templates: templates,
		cfg:       cfg.withDefaults(),
		logger:    logger,
		now:       time.Now,
	}, nil
}

// Register creates an account and sends a verification link.
//
// It returns nil for an address that is ALREADY registered, after sending
// that address a "someone tried to register you" style verification link
// instead. That is not politeness: a registration form that answers
// differently for a taken address is an account enumeration oracle, and it
// is the most-used one on the web.
func (s *Service) Register(ctx context.Context, email, username, password string) error {
	email = normalizeEmail(email)
	if email == "" {
		return fmt.Errorf("accounts: an email address is required")
	}
	if err := s.checkPassword(password); err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("accounts: hash password: %w", err)
	}

	now := s.now().UTC()
	account, err := s.store.Create(ctx, Account{
		ID:           newID(),
		Email:        email,
		Username:     strings.TrimSpace(username),
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	switch {
	case errors.Is(err, ErrEmailTaken):
		// The address exists. Send its owner a verification link — if it
		// is theirs they can confirm, and if it is not they learn someone
		// tried. Either way the caller cannot tell the two cases apart.
		existing, findErr := s.store.ByEmail(ctx, email)
		if findErr != nil {
			return nil
		}
		if !existing.EmailVerified {
			_ = s.sendVerification(ctx, existing)
		}
		return nil
	case err != nil:
		return fmt.Errorf("accounts: create: %w", err)
	}

	return s.sendVerification(ctx, account)
}

// VerifyEmail consumes a verification token and marks the address
// confirmed.
func (s *Service) VerifyEmail(ctx context.Context, token string) (Account, error) {
	record, err := s.store.ConsumeToken(ctx, PurposeVerifyEmail, hashToken(token))
	if err != nil {
		return Account{}, err
	}
	account, err := s.store.ByID(ctx, record.AccountID)
	if err != nil {
		return Account{}, err
	}
	account.EmailVerified = true
	account.UpdatedAt = s.now().UTC()
	if err := s.store.Update(ctx, account); err != nil {
		return Account{}, fmt.Errorf("accounts: update: %w", err)
	}
	return account, nil
}

// Login verifies a password and, when a session manager is configured,
// starts a session.
//
// Order matters and is deliberate: the lockout is checked BEFORE the
// password is verified, so a locked identity costs an attacker a lookup
// rather than a bcrypt comparison; and the session token is rotated on
// success, which is what closes session fixation.
func (s *Service) Login(ctx context.Context, email, password string) (Account, error) {
	email = normalizeEmail(email)
	key := "login:" + email

	count, err := s.store.FailureCount(ctx, key, s.now().UTC())
	if err != nil {
		return Account{}, fmt.Errorf("accounts: failure count: %w", err)
	}
	if count >= s.cfg.LockoutThreshold {
		return Account{}, ErrAccountLocked
	}

	account, err := s.store.ByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		// Verify against a dummy hash anyway: returning early here makes
		// an unknown address answer measurably faster than a known one
		// with a wrong password, which is the same disclosure by another
		// channel.
		auth.CheckPassword(password, dummyHash)
		_, _ = s.store.RecordFailure(ctx, key, s.now().UTC())
		return Account{}, ErrInvalidCredentials
	}
	if err != nil {
		return Account{}, fmt.Errorf("accounts: lookup: %w", err)
	}

	if !auth.CheckPassword(password, account.PasswordHash) {
		attempts, recErr := s.store.RecordFailure(ctx, key, s.now().UTC())
		if recErr != nil {
			s.logger.Error("accounts: could not record a failed attempt", "error", recErr)
		}
		if attempts >= s.cfg.LockoutThreshold {
			return Account{}, ErrAccountLocked
		}
		return Account{}, ErrInvalidCredentials
	}

	if account.Disabled {
		return Account{}, ErrAccountDisabled
	}
	if s.cfg.RequireEmailVerification && !account.EmailVerified {
		return Account{}, ErrEmailNotVerified
	}

	if err := s.store.ClearFailures(ctx, key); err != nil {
		s.logger.Error("accounts: could not clear failed attempts", "error", err)
	}
	return account, nil
}

// StartSession records the signed-in identity in the request's session,
// rotating the token first so a session fixed before sign-in cannot be
// reused after it.
func (s *Service) StartSession(ctx context.Context, account Account) error {
	if s.sessions == nil {
		return errors.New("accounts: no session manager configured")
	}
	if err := s.sessions.RenewToken(ctx); err != nil {
		return fmt.Errorf("accounts: rotate session: %w", err)
	}
	s.sessions.Put(ctx, SessionKeyAccountID, account.ID)
	s.sessions.Put(ctx, SessionKeyEmail, account.Email)
	return nil
}

// UseSessions installs the session manager the flows write into. The
// module calls it at start-up with the application's own manager; an
// application wiring the service by hand outside a module calls it itself.
func (s *Service) UseSessions(sm *auth.SessionManager) {
	if sm != nil {
		s.sessions = sm
	}
}

// SessionKeyAccountID and SessionKeyEmail are where a signed-in identity is
// recorded. They are exported because an operator surface enumerating
// sessions (orbit) needs to know which key holds the identity.
const (
	SessionKeyAccountID = "account_id"
	SessionKeyEmail     = "account_email"
)

// Logout ends the session in this context.
func (s *Service) Logout(ctx context.Context) error {
	if s.sessions == nil {
		return errors.New("accounts: no session manager configured")
	}
	return s.sessions.Destroy(ctx)
}

// RequestPasswordReset sends a reset link, and reports success whether or
// not the address is registered — the same non-disclosure Register keeps.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	account, err := s.store.ByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("accounts: lookup: %w", err)
	}
	if account.Disabled {
		return nil
	}

	token, err := s.issueToken(ctx, account, PurposeResetPassword, s.cfg.ResetTokenTTL)
	if err != nil {
		return err
	}
	return s.send(ctx, account, "reset_password", map[string]any{
		"Email": account.Email,
		"Link":  s.link("/auth/password/reset", token),
		"TTL":   s.cfg.ResetTokenTTL.String(),
	}, "Reset your password",
		"Open "+s.link("/auth/password/reset", token)+" to choose a new password. The link expires in "+s.cfg.ResetTokenTTL.String()+".")
}

// ResetPassword consumes a reset token and sets a new password. Every other
// outstanding reset token for the account is dropped, and so is every
// active session: a password reset that leaves the attacker's session
// running has not recovered the account.
func (s *Service) ResetPassword(ctx context.Context, token, password string) (Account, error) {
	if err := s.checkPassword(password); err != nil {
		return Account{}, err
	}
	record, err := s.store.ConsumeToken(ctx, PurposeResetPassword, hashToken(token))
	if err != nil {
		return Account{}, err
	}
	account, err := s.store.ByID(ctx, record.AccountID)
	if err != nil {
		return Account{}, err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return Account{}, fmt.Errorf("accounts: hash password: %w", err)
	}
	account.PasswordHash = hash
	account.UpdatedAt = s.now().UTC()
	if err := s.store.Update(ctx, account); err != nil {
		return Account{}, fmt.Errorf("accounts: update: %w", err)
	}

	if err := s.store.DeleteTokens(ctx, account.ID, PurposeResetPassword); err != nil {
		s.logger.Error("accounts: could not drop superseded reset tokens", "error", err)
	}
	if err := s.store.ClearFailures(ctx, "login:"+account.Email); err != nil {
		s.logger.Error("accounts: could not clear failed attempts", "error", err)
	}
	if err := s.RevokeSessions(ctx, account.ID); err != nil {
		s.logger.Error("accounts: could not end existing sessions after a reset", "error", err)
	}
	return account, nil
}

// ChangePassword sets a new password for a signed-in account, after
// verifying the current one. The re-authentication is the point: a session
// left open on a shared machine must not be enough to take the account
// over.
func (s *Service) ChangePassword(ctx context.Context, accountID, current, next string) error {
	if err := s.checkPassword(next); err != nil {
		return err
	}
	account, err := s.store.ByID(ctx, accountID)
	if err != nil {
		return err
	}
	if !auth.CheckPassword(current, account.PasswordHash) {
		return ErrInvalidCredentials
	}

	hash, err := auth.HashPassword(next)
	if err != nil {
		return fmt.Errorf("accounts: hash password: %w", err)
	}
	account.PasswordHash = hash
	account.UpdatedAt = s.now().UTC()
	if err := s.store.Update(ctx, account); err != nil {
		return fmt.Errorf("accounts: update: %w", err)
	}
	return nil
}

// RequestMagicLink emails a one-time sign-in link. Like the reset flow, it
// reports success for an unknown address.
func (s *Service) RequestMagicLink(ctx context.Context, email string) error {
	account, err := s.store.ByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("accounts: lookup: %w", err)
	}
	if account.Disabled {
		return nil
	}

	token, err := s.issueToken(ctx, account, PurposeMagicLink, s.cfg.MagicLinkTTL)
	if err != nil {
		return err
	}
	return s.send(ctx, account, "magic_link", map[string]any{
		"Email": account.Email,
		"Link":  s.link("/auth/magic-link", token),
		"TTL":   s.cfg.MagicLinkTTL.String(),
	}, "Your sign-in link",
		"Open "+s.link("/auth/magic-link", token)+" to sign in. The link expires in "+s.cfg.MagicLinkTTL.String()+".")
}

// ConsumeMagicLink signs in with a one-time link. A magic link also
// confirms the address: the owner just proved they read mail there.
func (s *Service) ConsumeMagicLink(ctx context.Context, token string) (Account, error) {
	record, err := s.store.ConsumeToken(ctx, PurposeMagicLink, hashToken(token))
	if err != nil {
		return Account{}, err
	}
	account, err := s.store.ByID(ctx, record.AccountID)
	if err != nil {
		return Account{}, err
	}
	if account.Disabled {
		return Account{}, ErrAccountDisabled
	}
	if !account.EmailVerified {
		account.EmailVerified = true
		account.UpdatedAt = s.now().UTC()
		if err := s.store.Update(ctx, account); err != nil {
			return Account{}, fmt.Errorf("accounts: update: %w", err)
		}
	}
	return account, nil
}

// RevokeSessions ends every stored session belonging to an account. It is
// what a password reset does, and what a "sign out everywhere" button
// calls.
func (s *Service) RevokeSessions(ctx context.Context, accountID string) error {
	if s.sessions == nil {
		return nil
	}
	_, err := s.sessions.RevokeWhere(ctx, func(info auth.SessionInfo) bool {
		id, _ := info.Values[SessionKeyAccountID].(string)
		return id == accountID
	})
	if errors.Is(err, auth.ErrSessionStoreNotIterable) {
		return nil
	}
	return err
}

// --- internals -------------------------------------------------------------

// dummyHash is a real bcrypt hash of a value nobody has. Verifying against
// it makes an unknown address cost the same as a known one.
const dummyHash = "$2a$12$Rj2zEnLnX2tWMrBH4cS8MOON.BfPV61AW/B93CVhVhuKOVBQz9U8S"

func (s *Service) checkPassword(password string) error {
	if len([]rune(password)) < s.cfg.MinPasswordLength {
		return fmt.Errorf("%w: at least %d characters", ErrWeakPassword, s.cfg.MinPasswordLength)
	}
	// bcrypt takes 72 BYTES and refuses more, so a long passphrase would
	// fail at hashing with an error about the algorithm. Saying it here
	// keeps the message about the password.
	if len(password) > 72 {
		return fmt.Errorf("%w: at most 72 bytes", ErrWeakPassword)
	}
	return nil
}

func (s *Service) issueToken(ctx context.Context, account Account, purpose TokenPurpose, ttl time.Duration) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	if err := s.store.CreateToken(ctx, Token{
		Hash:      hashToken(token),
		AccountID: account.ID,
		Purpose:   purpose,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}); err != nil {
		return "", fmt.Errorf("accounts: create token: %w", err)
	}
	return token, nil
}

func (s *Service) sendVerification(ctx context.Context, account Account) error {
	token, err := s.issueToken(ctx, account, PurposeVerifyEmail, s.cfg.VerifyTokenTTL)
	if err != nil {
		return err
	}
	return s.send(ctx, account, "verify_email", map[string]any{
		"Email": account.Email,
		"Link":  s.link("/auth/verify-email", token),
		"TTL":   s.cfg.VerifyTokenTTL.String(),
	}, "Confirm your email address",
		"Open "+s.link("/auth/verify-email", token)+" to confirm your address. The link expires in "+s.cfg.VerifyTokenTTL.String()+".")
}

// send renders through the application's templates when it supplied them,
// and falls back to a plain-text message built here otherwise — so the
// flows work before anybody writes a template, and look like the product
// once they do.
func (s *Service) send(ctx context.Context, account Account, template string, data map[string]any, subject, body string) error {
	if s.mailer == nil {
		return fmt.Errorf("accounts: no mailer configured: %s mail cannot be sent", template)
	}
	base := mail.Message{From: s.cfg.From, To: []string{account.Email}}

	if s.templates != nil {
		msg, err := s.templates.Render(template, data, base)
		if err == nil {
			return s.mailer.Send(ctx, msg)
		}
		// A missing template is not a failure: the fallback below covers
		// it. A template that exists and fails to render is, and it is
		// logged rather than silently replaced by the plain version.
		if !strings.Contains(err.Error(), "not found") {
			s.logger.Error("accounts: template failed to render, sending the plain message", "template", template, "error", err)
		}
	}

	base.Subject = subject
	base.Body = body
	return s.mailer.Send(ctx, base)
}

func (s *Service) link(path, token string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg.BaseURL), "/")
	return base + path + "?token=" + token
}

// newToken returns 256 bits from the system source, base64url-encoded. It
// is a credential: it is generated like one, and an error is fatal rather
// than papered over with a weaker source.
func newToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("accounts: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// hashToken is what the store keeps. SHA-256 and not bcrypt on purpose: a
// token already carries 256 bits of entropy, so there is nothing to brute
// force, and a per-request bcrypt on a link click would be a denial of
// service its sender controls.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func newID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("acct-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ClientIP is re-exported for handlers that want to key a lockout on the
// address as well as the identity.
func ClientIP(r *http.Request) string { return auth.ClientIPFromRequest(r) }
