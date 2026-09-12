package accounts

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Errors the second-factor flows return.
var (
	// ErrMFARequired reports that the password was right and a second
	// factor is still needed. It is not a failure: the caller continues
	// with VerifySecondFactor.
	ErrMFARequired = errors.New("accounts: a second factor is required")
	// ErrInvalidCode covers a wrong, expired or already-used TOTP code
	// and a wrong recovery code. One error, for the same reason
	// ErrInvalidToken is one.
	ErrInvalidCode = errors.New("accounts: invalid code")
	// ErrMFANotEnrolled reports an account with no second factor.
	ErrMFANotEnrolled = errors.New("accounts: no second factor is enrolled")
	// ErrMFAUnavailable reports that the deployment cannot store factors
	// — no MFAStore, or no encryption key for the secrets.
	ErrMFAUnavailable = errors.New("accounts: second factors are not available in this deployment")
	// ErrReauthenticationRequired reports an operation that needs a
	// fresher proof of identity than the session carries.
	ErrReauthenticationRequired = errors.New("accounts: re-authentication required")
)

// FactorKind names a second factor.
type FactorKind string

// FactorTOTP is a time-based one-time password from an authenticator app.
const FactorTOTP FactorKind = "totp"

// Factor is one enrolled second factor.
//
// Secret is stored ENCRYPTED (see MFAEncryptionKey): a TOTP secret is a
// password equivalent — anyone holding it can mint codes forever — and a
// database backup that leaks it hands over every second factor at once.
// LastStep is the counter step most recently accepted, which is what makes
// a one-time password one-time.
type Factor struct {
	AccountID string
	Kind      FactorKind
	Secret    string
	Confirmed bool
	LastStep  uint64
	CreatedAt time.Time
}

// MFAStore is where factors and recovery codes live.
//
// It is a SEPARATE interface from Store, and deliberately: an application
// that implemented Store against its own users table keeps working without
// it, and gains second factors by implementing this too. Widening Store
// would have broken every existing implementation to add a feature they may
// not want.
type MFAStore interface {
	PutFactor(ctx context.Context, factor Factor) error
	// GetFactor returns ErrMFANotEnrolled when there is none.
	GetFactor(ctx context.Context, accountID string, kind FactorKind) (Factor, error)
	DeleteFactor(ctx context.Context, accountID string, kind FactorKind) error
	// UpdateFactorStep records the last accepted counter step, refusing
	// to move it backwards.
	UpdateFactorStep(ctx context.Context, accountID string, kind FactorKind, step uint64) error

	ReplaceRecoveryCodes(ctx context.Context, accountID string, hashes []string) error
	// ConsumeRecoveryCode marks one code used, ATOMICALLY, and reports
	// how many remain. ErrInvalidCode when it does not match.
	ConsumeRecoveryCode(ctx context.Context, accountID, hash string) (remaining int, err error)
	CountRecoveryCodes(ctx context.Context, accountID string) (int, error)
}

// RecoveryCodeCount is how many single-use codes an enrolment issues.
const RecoveryCodeCount = 10

// mfaStore returns the store when the deployment can do second factors.
func (s *Service) mfaStore() (MFAStore, error) {
	store, ok := s.store.(MFAStore)
	if !ok {
		return nil, ErrMFAUnavailable
	}
	if len(s.cfg.MFAEncryptionKey) == 0 {
		// Refusing is the point. Storing TOTP secrets in the clear would
		// work perfectly and fail only once, silently, in a backup.
		return nil, fmt.Errorf("%w: set MFAEncryptionKey (32 bytes) so factor secrets are encrypted at rest", ErrMFAUnavailable)
	}
	return store, nil
}

// BeginTOTPEnrolment issues a secret and the otpauth URI to show as a QR
// code. The factor is stored UNCONFIRMED: a secret nobody has proven they
// can read must not be able to lock an account out of its own sign-in.
func (s *Service) BeginTOTPEnrolment(ctx context.Context, accountID string) (secret, uri string, err error) {
	store, err := s.mfaStore()
	if err != nil {
		return "", "", err
	}
	account, err := s.store.ByID(ctx, accountID)
	if err != nil {
		return "", "", err
	}

	secret, err = NewTOTPSecret()
	if err != nil {
		return "", "", err
	}
	sealed, err := s.sealSecret(secret)
	if err != nil {
		return "", "", err
	}
	if err := store.PutFactor(ctx, Factor{
		AccountID: accountID,
		Kind:      FactorTOTP,
		Secret:    sealed,
		Confirmed: false,
		CreatedAt: s.now().UTC(),
	}); err != nil {
		return "", "", fmt.Errorf("accounts: store factor: %w", err)
	}

	issuer := s.cfg.Issuer
	if strings.TrimSpace(issuer) == "" {
		issuer = "Nucleus"
	}
	return secret, TOTPURI(issuer, account.Email, secret), nil
}

// ConfirmTOTPEnrolment verifies the first code and turns the factor on,
// returning the recovery codes. They are shown ONCE — only their hashes are
// kept — which is why they are returned here and nowhere else.
func (s *Service) ConfirmTOTPEnrolment(ctx context.Context, accountID, code string) ([]string, error) {
	store, err := s.mfaStore()
	if err != nil {
		return nil, err
	}
	factor, err := store.GetFactor(ctx, accountID, FactorTOTP)
	if err != nil {
		return nil, err
	}
	secret, err := s.openSecret(factor.Secret)
	if err != nil {
		return nil, err
	}
	step, ok := VerifyTOTP(secret, code, s.now())
	if !ok {
		return nil, ErrInvalidCode
	}

	factor.Confirmed = true
	factor.LastStep = step
	if err := store.PutFactor(ctx, factor); err != nil {
		return nil, fmt.Errorf("accounts: confirm factor: %w", err)
	}
	return s.issueRecoveryCodes(ctx, store, accountID)
}

// DisableTOTP removes the factor and every recovery code with it.
func (s *Service) DisableTOTP(ctx context.Context, accountID string) error {
	store, err := s.mfaStore()
	if err != nil {
		return err
	}
	if err := store.DeleteFactor(ctx, accountID, FactorTOTP); err != nil {
		return err
	}
	return store.ReplaceRecoveryCodes(ctx, accountID, nil)
}

// HasConfirmedFactor reports whether sign-in needs a second step.
func (s *Service) HasConfirmedFactor(ctx context.Context, accountID string) bool {
	store, ok := s.store.(MFAStore)
	if !ok {
		return false
	}
	factor, err := store.GetFactor(ctx, accountID, FactorTOTP)
	return err == nil && factor.Confirmed
}

// VerifySecondFactor checks a TOTP code or a recovery code.
//
// The step the code matched is recorded, and a code at or below the last
// accepted step is refused: without that, a code read over someone's
// shoulder stays valid for the rest of its thirty seconds and every replay
// inside the window succeeds.
func (s *Service) VerifySecondFactor(ctx context.Context, accountID, code string) error {
	store, err := s.mfaStore()
	if err != nil {
		return err
	}
	factor, err := store.GetFactor(ctx, accountID, FactorTOTP)
	if err != nil {
		return err
	}
	if !factor.Confirmed {
		return ErrMFANotEnrolled
	}

	secret, err := s.openSecret(factor.Secret)
	if err != nil {
		return err
	}
	if step, ok := VerifyTOTP(secret, code, s.now()); ok {
		if step <= factor.LastStep {
			// Right code, already spent.
			return ErrInvalidCode
		}
		if err := store.UpdateFactorStep(ctx, accountID, FactorTOTP, step); err != nil {
			return fmt.Errorf("accounts: record factor step: %w", err)
		}
		return nil
	}

	// Not a TOTP code: it may be a recovery code. They are tried second
	// so a mistyped TOTP code does not burn one.
	if _, err := store.ConsumeRecoveryCode(ctx, accountID, hashRecoveryCode(code)); err != nil {
		return ErrInvalidCode
	}
	return nil
}

// RegenerateRecoveryCodes issues a fresh set and invalidates the old one.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, accountID string) ([]string, error) {
	store, err := s.mfaStore()
	if err != nil {
		return nil, err
	}
	return s.issueRecoveryCodes(ctx, store, accountID)
}

// RemainingRecoveryCodes reports how many are left, for the warning an
// account page shows before there are none.
func (s *Service) RemainingRecoveryCodes(ctx context.Context, accountID string) (int, error) {
	store, err := s.mfaStore()
	if err != nil {
		return 0, err
	}
	return store.CountRecoveryCodes(ctx, accountID)
}

func (s *Service) issueRecoveryCodes(ctx context.Context, store MFAStore, accountID string) ([]string, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	hashes := make([]string, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	if err := store.ReplaceRecoveryCodes(ctx, accountID, hashes); err != nil {
		return nil, fmt.Errorf("accounts: store recovery codes: %w", err)
	}
	return codes, nil
}

// --- step-up ---------------------------------------------------------------

// SessionKeyAuthenticatedAt records when identity was last PROVEN in this
// session — a password or a second factor, not merely a cookie that still
// resolves.
const SessionKeyAuthenticatedAt = "account_authenticated_at"

// MarkAuthenticated stamps the session as freshly authenticated. Login and
// a completed second factor call it.
func (s *Service) MarkAuthenticated(ctx context.Context) {
	if s.sessions == nil || !s.sessions.HasSession(ctx) {
		return
	}
	s.sessions.Put(ctx, SessionKeyAuthenticatedAt, s.now().UTC().Format(time.RFC3339))
}

// RequireFreshAuth returns ErrReauthenticationRequired unless identity was
// proven within maxAge.
//
// This is what a sensitive operation asks before it proceeds — changing an
// email address, disabling a second factor, issuing an API key. A session
// that has been open for three weeks is evidence that somebody signed in
// three weeks ago, and nothing about who is at the keyboard now.
func (s *Service) RequireFreshAuth(ctx context.Context, maxAge time.Duration) error {
	if s.sessions == nil || !s.sessions.HasSession(ctx) {
		return ErrReauthenticationRequired
	}
	stamp := s.sessions.GetString(ctx, SessionKeyAuthenticatedAt)
	if stamp == "" {
		return ErrReauthenticationRequired
	}
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ErrReauthenticationRequired
	}
	if s.now().UTC().Sub(at) > maxAge {
		return ErrReauthenticationRequired
	}
	return nil
}

// --- secrets at rest --------------------------------------------------------

// sealSecret encrypts a factor secret with AES-256-GCM. The nonce is
// random per secret and stored with the ciphertext, which is what keeps two
// enrolments with the same key from being comparable.
func (s *Service) sealSecret(plain string) (string, error) {
	gcm, err := s.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("accounts: generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Service) openSecret(sealed string) (string, error) {
	gcm, err := s.aead()
	if err != nil {
		return "", err
	}
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("accounts: decode factor secret: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("accounts: stored factor secret is truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// This is the shape of "the key changed": say so, because the
		// alternative is an account that cannot sign in for no visible
		// reason.
		return "", fmt.Errorf("accounts: cannot decrypt the factor secret (was MFAEncryptionKey rotated?): %w", err)
	}
	return string(plain), nil
}

func (s *Service) aead() (cipher.AEAD, error) {
	key := s.cfg.MFAEncryptionKey
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: MFAEncryptionKey must be exactly 32 bytes, got %d", ErrMFAUnavailable, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("accounts: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("accounts: gcm: %w", err)
	}
	return gcm, nil
}

// newRecoveryCode returns a code with 80 bits of entropy, grouped for
// typing. It is long because it bypasses the second factor entirely.
func newRecoveryCode() (string, error) {
	var buf [10]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("accounts: generate recovery code: %w", err)
	}
	raw := strings.ToLower(hex.EncodeToString(buf[:]))
	return raw[:5] + "-" + raw[5:10] + "-" + raw[10:15] + "-" + raw[15:], nil
}

// hashRecoveryCode normalises and hashes. Normalising means a code typed
// with spaces, upper case or missing dashes still works, without storing
// anything but the hash.
func hashRecoveryCode(code string) string {
	normalized := strings.ToLower(strings.TrimSpace(code))
	normalized = strings.NewReplacer("-", "", " ", "").Replace(normalized)
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// --- the two-step sign-in ---------------------------------------------------

// SessionKeyPendingAccountID holds the account that has passed the
// password step and not the second factor. It is a DIFFERENT key from
// SessionKeyAccountID on purpose: anything that reads the signed-in
// identity must not see a half-finished sign-in as a finished one.
const SessionKeyPendingAccountID = "account_pending_id"

// StartPendingSession records a sign-in waiting on its second factor.
func (s *Service) StartPendingSession(ctx context.Context, account Account) error {
	if s.sessions == nil {
		return errors.New("accounts: no session manager configured")
	}
	if !s.sessions.HasSession(ctx) {
		return errors.New("accounts: this request did not pass through the session middleware")
	}
	// Rotate here as well as at completion: the pending state is already
	// something an attacker would like to plant in a victim's session.
	if err := s.sessions.RenewToken(ctx); err != nil {
		return fmt.Errorf("accounts: rotate session: %w", err)
	}
	s.sessions.Put(ctx, SessionKeyPendingAccountID, account.ID)
	return nil
}

// CompleteSecondFactor finishes a pending sign-in with a TOTP or recovery
// code and returns the account it signed in.
//
// Failures here count towards the same lockout as a wrong password: a
// second factor that can be guessed without limit is a six-digit password.
func (s *Service) CompleteSecondFactor(ctx context.Context, code string) (Account, error) {
	if s.sessions == nil {
		return Account{}, errors.New("accounts: no session manager configured")
	}
	if !s.sessions.HasSession(ctx) {
		return Account{}, ErrInvalidCredentials
	}
	accountID := s.sessions.GetString(ctx, SessionKeyPendingAccountID)
	if accountID == "" {
		return Account{}, ErrInvalidCredentials
	}

	key := "mfa:" + accountID
	count, err := s.store.FailureCount(ctx, key, s.now().UTC())
	if err != nil {
		return Account{}, fmt.Errorf("accounts: failure count: %w", err)
	}
	if count >= s.cfg.LockoutThreshold {
		return Account{}, ErrAccountLocked
	}

	if err := s.VerifySecondFactor(ctx, accountID, code); err != nil {
		attempts, recErr := s.store.RecordFailure(ctx, key, s.now().UTC())
		if recErr != nil {
			s.logger.Error("accounts: could not record a failed second factor", "error", recErr)
		}
		if attempts >= s.cfg.LockoutThreshold {
			return Account{}, ErrAccountLocked
		}
		return Account{}, err
	}

	account, err := s.store.ByID(ctx, accountID)
	if err != nil {
		return Account{}, err
	}
	if err := s.store.ClearFailures(ctx, key); err != nil {
		s.logger.Error("accounts: could not clear failed attempts", "error", err)
	}

	s.sessions.Remove(ctx, SessionKeyPendingAccountID)
	if err := s.StartSession(ctx, account); err != nil {
		return Account{}, err
	}
	return account, nil
}
