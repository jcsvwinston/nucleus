//go:build !mssql

package db

// isMSSQLUniqueViolation is the stub used when the SQL Server driver is not
// built in. Without `-tags mssql` the driver is never registered, so no
// go-mssqldb error can reach the classifier — and the import must stay out of
// the default build.
func isMSSQLUniqueViolation(error) bool { return false }
