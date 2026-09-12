package accounts

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	nucleusdb "github.com/jcsvwinston/nucleus/pkg/db"
)

// The store declares three SQL dialects and the unit tests exercise one.
// That gap is exactly where a portable-looking store stops being portable:
// placeholder syntax, timestamp types, how a duplicate is reported and
// whether a conditional UPDATE reports rows affected all differ, and every
// one of them is silent on SQLite.
//
// This runs the whole lifecycle against whatever engine the matrix lane
// points at — the same NUCLEUS_SQL_MATRIX_URL the other live tests read —
// and skips when there is none, so `go test ./...` on a laptop is unchanged.
func TestSQLMatrix_AccountsStore(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping the live accounts store test")
	}

	lower := strings.ToLower(rawURL)
	var flavor Flavor
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		flavor = FlavorPostgres
	case strings.HasPrefix(lower, "mysql://"):
		flavor = FlavorMySQL
	default:
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}

	db := openMatrixDB(t, rawURL)
	// A prefix per run, so two profiles on one database — and a previous
	// flaky run — cannot collide.
	prefix := fmt.Sprintf("accounts_live_%d_", time.Now().UnixNano())
	store, err := NewSQLStore(t.Context(), db, SQLStoreConfig{Flavor: flavor, TablePrefix: prefix})
	if err != nil {
		t.Fatalf("new store against %s: %v", flavor, err)
	}
	t.Cleanup(func() { dropAccountTables(db, prefix) })

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	account := Account{
		ID: "live-1", Email: "ana@example.test", Username: "ana",
		PasswordHash: "$2a$12$notarealhash", CreatedAt: now, UpdatedAt: now,
	}

	// Create, and the duplicate the second one has to be.
	if _, err := store.Create(ctx, account); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, account); err == nil {
		t.Fatal("a duplicate email was accepted")
	} else if !strings.Contains(err.Error(), ErrEmailTaken.Error()) && err != ErrEmailTaken {
		// This is the one an engine gets wrong quietly: the unique
		// violation is reported differently by every driver, and a store
		// that misses it overwrites instead of refusing.
		t.Fatalf("a duplicate was refused with the wrong error on %s: %v", flavor, err)
	}

	found, err := store.ByEmail(ctx, "ANA@example.test")
	if err != nil {
		t.Fatalf("by email: %v", err)
	}
	if found.ID != account.ID || found.Username != "ana" {
		t.Fatalf("read back %+v", found)
	}

	// Tokens: single-use through a conditional UPDATE, which depends on
	// RowsAffected being reported.
	token := Token{
		Hash: "livehash", AccountID: account.ID, Purpose: PurposeResetPassword,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := store.CreateToken(ctx, token); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := store.ConsumeToken(ctx, PurposeResetPassword, "livehash"); err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if _, err := store.ConsumeToken(ctx, PurposeResetPassword, "livehash"); err == nil {
		t.Fatalf("a token was consumed twice on %s", flavor)
	}

	// Failure counters, which read on a timestamp predicate.
	key := "login:ana@example.test"
	count, err := store.RecordFailure(ctx, key, time.Now())
	if err != nil {
		t.Fatalf("record failure: %v", err)
	}
	if count != 1 {
		t.Fatalf("the first failure counted %d", count)
	}
	if err := store.ClearFailures(ctx, key); err != nil {
		t.Fatalf("clear failures: %v", err)
	}
	if count, err := store.FailureCount(ctx, key, time.Now()); err != nil || count != 0 {
		t.Fatalf("failures after clearing: %d (%v)", count, err)
	}

	// Second factors: the MFA tables travel with the same store.
	factor := Factor{AccountID: account.ID, Kind: FactorTOTP, Secret: "sealed", CreatedAt: now}
	if err := store.PutFactor(ctx, factor); err != nil {
		t.Fatalf("put factor: %v", err)
	}
	if err := store.UpdateFactorStep(ctx, account.ID, FactorTOTP, 42); err != nil {
		t.Fatalf("update step: %v", err)
	}
	if err := store.UpdateFactorStep(ctx, account.ID, FactorTOTP, 42); err == nil {
		t.Fatalf("a replayed step was accepted on %s", flavor)
	}
	if err := store.ReplaceRecoveryCodes(ctx, account.ID, []string{"a", "b"}); err != nil {
		t.Fatalf("replace recovery codes: %v", err)
	}
	remaining, err := store.ConsumeRecoveryCode(ctx, account.ID, "a")
	if err != nil || remaining != 1 {
		t.Fatalf("consume recovery code: %d (%v)", remaining, err)
	}
	if _, err := store.ConsumeRecoveryCode(ctx, account.ID, "a"); err == nil {
		t.Fatalf("a recovery code was spent twice on %s", flavor)
	}

	if _, err := store.PurgeExpired(ctx, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatalf("purge: %v", err)
	}
}

// openMatrixDB opens the live database through the framework's own driver
// resolution, so the test exercises the same path an application takes.
func openMatrixDB(t *testing.T, rawURL string) *sql.DB {
	t.Helper()
	database, err := nucleusdb.New(nucleusdb.Config{
		Engine:      nucleusdb.EngineSQL,
		DatabaseURL: rawURL,
	}, nil)
	if err != nil {
		t.Fatalf("open %q: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("sql handle: %v", err)
	}
	return sqlDB
}

func dropAccountTables(db *sql.DB, prefix string) {
	for _, suffix := range []string{"accounts", "account_tokens", "account_failures", "account_factors", "account_recovery_codes"} {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + prefix + suffix)
	}
}
