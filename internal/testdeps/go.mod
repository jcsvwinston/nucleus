// The tests and test servers the framework's go.mod must not carry (NU-106).
// Never published: the module sits under internal/, and the replace below
// points it at the framework in this tree. See doc.go.
module github.com/jcsvwinston/nucleus/internal/testdeps

go 1.26.6

require (
	github.com/alicebob/miniredis/v2 v2.39.0
	github.com/go-sql-driver/mysql v1.10.1
	github.com/jcsvwinston/nucleus v1.31.0
	github.com/microsoft/go-mssqldb v1.11.0
	github.com/sijms/go-ora/v2 v2.9.0
	modernc.org/sqlite v1.58.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace github.com/jcsvwinston/nucleus => ../..
