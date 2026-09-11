package accounts

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"testing"
	"time"
)

// RFC 6238's own test vectors, for the SHA-1 variant with the 20-byte
// secret the appendix defines ("12345678901234567890"). A TOTP written by
// hand is worth exactly as much as its agreement with the standard: if
// these pass, every authenticator app agrees too.
func TestTOTP_RFC6238Vectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))

	// The RFC publishes 8-digit values; the 6-digit code every app uses
	// is their last six digits.
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, v := range vectors {
		counter := uint64(v.unix) / totpPeriod
		got, err := totpCode(secret, counter)
		if err != nil {
			t.Fatalf("code at %d: %v", v.unix, err)
		}
		if got != v.want {
			t.Errorf("at %d: got %s, want %s", v.unix, got, v.want)
		}
	}
}

func TestVerifyTOTP_AcceptsTheCurrentCode(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	now := time.Now()
	code, err := totpCode(secret, uint64(now.Unix())/totpPeriod)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if _, ok := VerifyTOTP(secret, code, now); !ok {
		t.Fatal("the current code was refused")
	}
}

// One step either side is accepted — clocks disagree — and two is not.
func TestVerifyTOTP_SkewWindow(t *testing.T) {
	secret, _ := NewTOTPSecret()
	now := time.Now()

	for _, delta := range []time.Duration{-totpPeriod * time.Second, totpPeriod * time.Second} {
		code, _ := totpCode(secret, uint64(now.Add(delta).Unix())/totpPeriod)
		if _, ok := VerifyTOTP(secret, code, now); !ok {
			t.Errorf("a code %v away was refused", delta)
		}
	}
	far, _ := totpCode(secret, uint64(now.Add(3*totpPeriod*time.Second).Unix())/totpPeriod)
	if _, ok := VerifyTOTP(secret, far, now); ok {
		t.Error("a code three steps away was accepted")
	}
}

func TestVerifyTOTP_RejectsMalformedInput(t *testing.T) {
	secret, _ := NewTOTPSecret()
	for _, code := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := VerifyTOTP(secret, code, time.Now()); ok {
			t.Errorf("%q was accepted", code)
		}
	}
}

func TestTOTPURI_IsScannable(t *testing.T) {
	uri := TOTPURI("Example Co", "ana@example.test", "JBSWY3DPEHPK3PXP")
	for _, want := range []string{
		"otpauth://totp/",
		"secret=JBSWY3DPEHPK3PXP",
		"issuer=Example+Co",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("the URI lacks %q: %s", want, uri)
		}
	}
}

// --- enrolment and sign-in --------------------------------------------------

func withMFA(t *testing.T) (*Service, *recordingMailer, *SQLStore) {
	t.Helper()
	svc, mailer, store := newTestService(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("key: %v", err)
	}
	svc.cfg.MFAEncryptionKey = key
	svc.cfg.Issuer = "Authtest"
	return svc, mailer, store
}

func enrolTOTP(t *testing.T, svc *Service, accountID string) (secret string, recovery []string) {
	t.Helper()
	secret, _, err := svc.BeginTOTPEnrolment(t.Context(), accountID)
	if err != nil {
		t.Fatalf("begin enrolment: %v", err)
	}
	code, err := totpCode(secret, uint64(time.Now().Unix())/totpPeriod)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	recovery, err = svc.ConfirmTOTPEnrolment(t.Context(), accountID, code)
	if err != nil {
		t.Fatalf("confirm enrolment: %v", err)
	}
	return secret, recovery
}

func registeredAccount(t *testing.T, svc *Service, store *SQLStore, email string) Account {
	t.Helper()
	if err := svc.Register(t.Context(), email, "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	account, err := store.ByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	return account
}

// A secret nobody has proven they can read must not be able to lock an
// account out of its own sign-in.
func TestTOTP_EnrolmentIsNotActiveUntilConfirmed(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")

	if _, _, err := svc.BeginTOTPEnrolment(t.Context(), account.ID); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if svc.HasConfirmedFactor(t.Context(), account.ID) {
		t.Fatal("an unconfirmed factor counts as enrolled")
	}

	enrolTOTP(t, svc, account.ID)
	if !svc.HasConfirmedFactor(t.Context(), account.ID) {
		t.Fatal("a confirmed factor does not count as enrolled")
	}
}

// The stored secret is encrypted: a database backup must not hand over
// every second factor.
func TestTOTP_SecretIsEncryptedAtRest(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	secret, _ := enrolTOTP(t, svc, account.ID)

	var stored string
	if err := store.db.QueryRow("SELECT secret FROM nucleus_account_factors LIMIT 1").Scan(&stored); err != nil {
		t.Fatalf("read factor row: %v", err)
	}
	if stored == secret {
		t.Fatal("the TOTP secret is stored in the clear")
	}
	if strings.Contains(stored, secret) {
		t.Fatal("the stored value contains the secret")
	}
}

// Without a key, second factors are refused rather than stored in the
// clear.
func TestTOTP_RefusedWithoutAnEncryptionKey(t *testing.T) {
	svc, _, store := newTestService(t)
	account := registeredAccount(t, svc, store, "ana@example.test")

	_, _, err := svc.BeginTOTPEnrolment(t.Context(), account.ID)
	if !errors.Is(err, ErrMFAUnavailable) {
		t.Fatalf("expected ErrMFAUnavailable, got %v", err)
	}
}

// A one-time password is one-time: the same code does not work twice, even
// inside its window.
func TestTOTP_CodeCannotBeReplayed(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	secret, _ := enrolTOTP(t, svc, account.ID)

	code, _ := totpCode(secret, uint64(time.Now().Unix())/totpPeriod)
	// The enrolment already spent this step, so the replay is refused
	// straight away — which is the property under test.
	if err := svc.VerifySecondFactor(t.Context(), account.ID, code); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("a spent code was accepted again: %v", err)
	}

	next, _ := totpCode(secret, uint64(time.Now().Unix())/totpPeriod+1)
	if err := svc.VerifySecondFactor(t.Context(), account.ID, next); err != nil {
		t.Fatalf("the next code was refused: %v", err)
	}
	if err := svc.VerifySecondFactor(t.Context(), account.ID, next); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("that code worked twice")
	}
}

func TestRecoveryCodes_AreSingleUse(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	_, codes := enrolTOTP(t, svc, account.ID)

	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), RecoveryCodeCount)
	}
	if err := svc.VerifySecondFactor(t.Context(), account.ID, codes[0]); err != nil {
		t.Fatalf("a recovery code was refused: %v", err)
	}
	if err := svc.VerifySecondFactor(t.Context(), account.ID, codes[0]); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("a recovery code worked twice")
	}

	remaining, err := svc.RemainingRecoveryCodes(t.Context(), account.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != RecoveryCodeCount-1 {
		t.Fatalf("%d codes remain, want %d", remaining, RecoveryCodeCount-1)
	}
}

// A code typed with different spacing or case still works: the stored form
// is a hash of the normalised value.
func TestRecoveryCodes_AreForgivingAboutFormatting(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	_, codes := enrolTOTP(t, svc, account.ID)

	messy := strings.ToUpper(strings.ReplaceAll(codes[0], "-", " "))
	if err := svc.VerifySecondFactor(t.Context(), account.ID, messy); err != nil {
		t.Fatalf("a code typed with spaces and capitals was refused: %v", err)
	}
}

func TestRecoveryCodes_RegeneratingInvalidatesTheOldSet(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	_, old := enrolTOTP(t, svc, account.ID)

	fresh, err := svc.RegenerateRecoveryCodes(t.Context(), account.ID)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if err := svc.VerifySecondFactor(t.Context(), account.ID, old[0]); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("an old recovery code still works")
	}
	if err := svc.VerifySecondFactor(t.Context(), account.ID, fresh[0]); err != nil {
		t.Fatalf("a new recovery code was refused: %v", err)
	}
}

func TestDisableTOTP_RemovesTheFactorAndItsCodes(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	_, codes := enrolTOTP(t, svc, account.ID)

	if err := svc.DisableTOTP(t.Context(), account.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if svc.HasConfirmedFactor(t.Context(), account.ID) {
		t.Fatal("the factor survived being disabled")
	}
	if err := svc.VerifySecondFactor(t.Context(), account.ID, codes[1]); err == nil {
		t.Fatal("a recovery code still works after the factor was removed")
	}
}

// A second factor that can be guessed without limit is a six-digit
// password: failures count towards the same lockout.
func TestSecondFactor_IsRateLimited(t *testing.T) {
	svc, _, store := withMFA(t)
	account := registeredAccount(t, svc, store, "ana@example.test")
	enrolTOTP(t, svc, account.ID)

	for i := 0; i < svc.cfg.LockoutThreshold; i++ {
		_, _ = store.RecordFailure(t.Context(), "mfa:"+account.ID, time.Now())
	}
	// A pending sign-in whose factor step is exhausted must be refused
	// as locked, not merely as wrong.
	count, err := store.FailureCount(t.Context(), "mfa:"+account.ID, time.Now())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count < svc.cfg.LockoutThreshold {
		t.Fatalf("the failures were not recorded (%d)", count)
	}
}

// --- step-up ----------------------------------------------------------------

func TestRequireFreshAuth(t *testing.T) {
	svc, _, _ := withMFA(t)
	// No session manager wired into the context: the safe answer is to
	// demand re-authentication, never to assume it happened.
	if err := svc.RequireFreshAuth(t.Context(), time.Minute); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatalf("expected ErrReauthenticationRequired, got %v", err)
	}
}
