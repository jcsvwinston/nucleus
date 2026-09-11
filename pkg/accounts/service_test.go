package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/mail"

	_ "modernc.org/sqlite"
)

const goodPassword = "a-long-enough-passphrase"

type recordingMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *recordingMailer) messages() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.sent...)
}

// lastLinkToken pulls the token out of the last message sent, the way a
// user does out of their inbox.
func (m *recordingMailer) lastLinkToken(t *testing.T) string {
	t.Helper()
	msgs := m.messages()
	if len(msgs) == 0 {
		t.Fatal("no mail was sent")
	}
	body := msgs[len(msgs)-1].Body
	_, after, found := strings.Cut(body, "token=")
	if !found {
		t.Fatalf("no token in the message: %q", body)
	}
	token, _, _ := strings.Cut(after, " ")
	return strings.TrimSpace(strings.TrimSuffix(token, "."))
}

func newTestService(t *testing.T) (*Service, *recordingMailer, *SQLStore) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:accounts_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewSQLStore(t.Context(), db, SQLStoreConfig{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	mailer := &recordingMailer{}
	svc, err := New(store, auth.NewSessionManager(auth.SessionConfig{}), mailer, nil, Config{
		BaseURL: "https://app.example.test",
		From:    "no-reply@example.test",
	}, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc, mailer, store
}

func TestRegister_CreatesAccountAndSendsVerification(t *testing.T) {
	svc, mailer, store := newTestService(t)

	if err := svc.Register(t.Context(), "Ana@Example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	account, err := store.ByEmail(t.Context(), "ana@example.test")
	if err != nil {
		t.Fatalf("the account was not created: %v", err)
	}
	if account.Email != "ana@example.test" {
		t.Errorf("the address was not normalised: %q", account.Email)
	}
	if account.EmailVerified {
		t.Error("a self-registered account starts verified")
	}
	if account.PasswordHash == goodPassword || !strings.HasPrefix(account.PasswordHash, "$2") {
		t.Error("the password was not hashed")
	}
	if len(mailer.messages()) != 1 {
		t.Fatalf("expected one verification mail, got %d", len(mailer.messages()))
	}
}

// A registration form that answers differently for a taken address is an
// account enumeration oracle — the most-used one on the web.
func TestRegister_TakenAddressIsIndistinguishable(t *testing.T) {
	svc, _, store := newTestService(t)

	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("first register: %v", err)
	}
	first, _ := store.ByEmail(t.Context(), "ana@example.test")

	if err := svc.Register(t.Context(), "ana@example.test", "impostor", "another-long-passphrase"); err != nil {
		t.Fatalf("a second registration reported an error: %v", err)
	}

	second, _ := store.ByEmail(t.Context(), "ana@example.test")
	if second.PasswordHash != first.PasswordHash {
		t.Fatal("the second registration overwrote the account")
	}
	if second.Username != first.Username {
		t.Fatal("the second registration changed the account")
	}
}

func TestRegister_RefusesAShortPassword(t *testing.T) {
	svc, _, _ := newTestService(t)
	err := svc.Register(t.Context(), "ana@example.test", "ana", "short")
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}
}

func TestVerifyEmail_ConfirmsOnceAndOnlyOnce(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	token := mailer.lastLinkToken(t)

	account, err := svc.VerifyEmail(t.Context(), token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !account.EmailVerified {
		t.Fatal("the address was not marked verified")
	}

	if _, err := svc.VerifyEmail(t.Context(), token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a verification link worked twice (%v)", err)
	}
}

// A token issued to verify an address must not reset a password. The
// purpose is part of the lookup, not a comment on the row.
func TestTokens_PurposeIsEnforced(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	verification := mailer.lastLinkToken(t)

	if _, err := svc.ResetPassword(t.Context(), verification, "yet-another-long-pass"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a verification token reset a password (%v)", err)
	}
}

// The store must hold a HASH: a leaked database must not hand over live
// reset links.
func TestTokens_AreStoredHashed(t *testing.T) {
	svc, mailer, store := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	token := mailer.lastLinkToken(t)

	var stored string
	row := store.db.QueryRow("SELECT hash FROM nucleus_account_tokens LIMIT 1")
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("read token row: %v", err)
	}
	if stored == token {
		t.Fatal("the token itself is stored, not its hash")
	}
	if stored != hashToken(token) {
		t.Fatal("the stored value is not the token's hash")
	}
}

func TestLogin_Succeeds(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	account, err := svc.Login(t.Context(), "ANA@example.test", goodPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if account.Email != "ana@example.test" {
		t.Fatalf("logged in as %q", account.Email)
	}
}

// A wrong password and an unknown account answer the same.
func TestLogin_UnknownAccountAndWrongPasswordAreIndistinguishable(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, wrongPassword := svc.Login(t.Context(), "ana@example.test", "the-wrong-passphrase")
	_, unknownAccount := svc.Login(t.Context(), "nobody@example.test", "the-wrong-passphrase")

	if !errors.Is(wrongPassword, ErrInvalidCredentials) || !errors.Is(unknownAccount, ErrInvalidCredentials) {
		t.Fatalf("the two cases differ: %v vs %v", wrongPassword, unknownAccount)
	}
}

func TestLogin_LocksOutAfterEnoughFailures(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	var last error
	for i := 0; i < svc.cfg.LockoutThreshold; i++ {
		_, last = svc.Login(t.Context(), "ana@example.test", "wrong-passphrase-here")
	}
	if !errors.Is(last, ErrAccountLocked) {
		t.Fatalf("the account was not locked after %d failures: %v", svc.cfg.LockoutThreshold, last)
	}

	// And the lockout holds even for the RIGHT password: otherwise it
	// only slows down an attacker who is already wrong.
	if _, err := svc.Login(t.Context(), "ana@example.test", goodPassword); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("the correct password bypassed the lockout: %v", err)
	}
}

func TestLogin_SuccessClearsTheFailureCounter(t *testing.T) {
	svc, _, store := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, _ = svc.Login(t.Context(), "ana@example.test", "wrong-passphrase-here")
	}
	if _, err := svc.Login(t.Context(), "ana@example.test", goodPassword); err != nil {
		t.Fatalf("login: %v", err)
	}

	count, err := store.FailureCount(t.Context(), "login:ana@example.test", time.Now())
	if err != nil {
		t.Fatalf("failure count: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d failures survived a successful sign-in", count)
	}
}

func TestLogin_RequiresVerificationWhenConfigured(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.cfg.RequireEmailVerification = true
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := svc.Login(t.Context(), "ana@example.test", goodPassword); !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("an unverified account signed in: %v", err)
	}
}

func TestPasswordReset_EndToEnd(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.RequestPasswordReset(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	token := mailer.lastLinkToken(t)

	newPassword := "a-brand-new-passphrase"
	if _, err := svc.ResetPassword(t.Context(), token, newPassword); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := svc.Login(t.Context(), "ana@example.test", newPassword); err != nil {
		t.Fatalf("the new password does not work: %v", err)
	}
	if _, err := svc.Login(t.Context(), "ana@example.test", goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("the old password still works")
	}
	if _, err := svc.ResetPassword(t.Context(), token, "third-passphrase-here"); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("the reset link worked twice")
	}
}

// An unknown address must not be disclosed by the reset form either.
func TestPasswordReset_UnknownAddressSendsNothingAndSaysNothing(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.RequestPasswordReset(t.Context(), "nobody@example.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	if n := len(mailer.messages()); n != 0 {
		t.Fatalf("%d messages were sent for an unknown address", n)
	}
}

// A reset supersedes every other outstanding link: two "forgot password"
// clicks must not leave the first link live after the second is used.
func TestPasswordReset_DropsOutstandingTokens(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.RequestPasswordReset(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	first := mailer.lastLinkToken(t)
	if err := svc.RequestPasswordReset(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("second request: %v", err)
	}
	second := mailer.lastLinkToken(t)

	if _, err := svc.ResetPassword(t.Context(), second, "a-new-long-passphrase"); err != nil {
		t.Fatalf("reset with the newest link: %v", err)
	}
	if _, err := svc.ResetPassword(t.Context(), first, "another-long-passphrase"); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("the superseded link still works")
	}
}

func TestChangePassword_RequiresTheCurrentOne(t *testing.T) {
	svc, _, store := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	account, _ := store.ByEmail(t.Context(), "ana@example.test")

	if err := svc.ChangePassword(t.Context(), account.ID, "not-the-current-one", "a-new-long-passphrase"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("the password changed without the current one: %v", err)
	}
	if err := svc.ChangePassword(t.Context(), account.ID, goodPassword, "a-new-long-passphrase"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if _, err := svc.Login(t.Context(), "ana@example.test", "a-new-long-passphrase"); err != nil {
		t.Fatalf("the new password does not work: %v", err)
	}
}

func TestMagicLink_SignsInAndConfirmsTheAddress(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.RequestMagicLink(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	token := mailer.lastLinkToken(t)

	account, err := svc.ConsumeMagicLink(t.Context(), token)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !account.EmailVerified {
		t.Error("using a mailed link did not confirm the address")
	}
	if _, err := svc.ConsumeMagicLink(t.Context(), token); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("the magic link worked twice")
	}
}

// Two concurrent uses of one link: exactly one wins. A read-then-write
// store would let both through under exactly the race an attacker causes on
// purpose.
func TestConsumeToken_IsAtomicUnderConcurrency(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.RequestPasswordReset(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	token := mailer.lastLinkToken(t)

	var wg sync.WaitGroup
	results := make([]error, 8)
	start := make(chan struct{})
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = svc.ResetPassword(context.Background(), token, fmt.Sprintf("passphrase-number-%d", i))
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d of %d concurrent uses of one link succeeded", wins, len(results))
	}
}

// An expired token is refused, and refused the same way an unknown one is.
func TestTokens_Expire(t *testing.T) {
	svc, mailer, _ := newTestService(t)
	svc.cfg.ResetTokenTTL = time.Millisecond
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.RequestPasswordReset(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	token := mailer.lastLinkToken(t)

	time.Sleep(5 * time.Millisecond)
	if _, err := svc.ResetPassword(t.Context(), token, "a-new-long-passphrase"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("an expired token was accepted: %v", err)
	}
}

// A disabled account cannot sign in and cannot recover a password.
func TestDisabledAccount(t *testing.T) {
	svc, mailer, store := newTestService(t)
	if err := svc.Register(t.Context(), "ana@example.test", "ana", goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	account, _ := store.ByEmail(t.Context(), "ana@example.test")
	account.Disabled = true
	if err := store.Update(t.Context(), account); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, err := svc.Login(t.Context(), "ana@example.test", goodPassword); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("a disabled account signed in: %v", err)
	}
	before := len(mailer.messages())
	if err := svc.RequestPasswordReset(t.Context(), "ana@example.test"); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	if len(mailer.messages()) != before {
		t.Fatal("a disabled account was sent a reset link")
	}
}
