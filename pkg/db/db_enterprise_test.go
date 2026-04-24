//go:build mssql && oracle

package db

import "testing"

func TestOpenSQLDB_MSSQLAndOracleSchemes(t *testing.T) {
	cases := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			conn, err := openSQLDB(raw)
			if err != nil || conn == nil {
				t.Fatalf("expected valid DB handle for %q, got err=%v", raw, err)
			}
			_ = conn.Close()
		})
	}
}

func TestOpenSQLDB_EnterpriseCandidatesSupported(t *testing.T) {
	candidates := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}

	for _, rawURL := range candidates {
		t.Run(rawURL, func(t *testing.T) {
			conn, err := openSQLDB(rawURL)
			if err != nil {
				t.Fatalf("expected supported sql DB URL for %q, got err=%v", rawURL, err)
			}
			_ = conn.Close()
		})
	}
}
