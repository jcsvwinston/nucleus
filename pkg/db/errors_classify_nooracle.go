//go:build !oracle

package db

// isOracleUniqueViolation is the stub used when the Oracle driver is not
// built in. Without `-tags oracle` the driver is never registered, so no
// go-ora error can reach the classifier — and the import must stay out of
// the default build.
func isOracleUniqueViolation(error) bool { return false }
