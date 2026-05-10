package hooks

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wraps every test in this package with a goroutine-leak check.
// The hooks do not spawn goroutines on their own, so the leak set should
// always be empty after a test returns.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
