package accounts

import (
	"testing"
	"time"
)

// TestingTOTPCode returns the code a factor's secret produces at a given
// time. It is exported for tests OUTSIDE this package — the module's own
// HTTP tests, and an application's tests for a login page it wrote — which
// otherwise have no way to produce a valid code without reimplementing
// RFC 6238 to check an implementation of RFC 6238.
//
// It takes a testing.TB so it cannot be called from production code by
// accident.
func TestingTOTPCode(tb testing.TB, secret string, at time.Time) string {
	tb.Helper()
	code, err := totpCode(secret, uint64(at.UTC().Unix())/totpPeriod)
	if err != nil {
		tb.Fatalf("accounts: generate TOTP code: %v", err)
	}
	return code
}
