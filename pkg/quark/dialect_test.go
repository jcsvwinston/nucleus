package quark_test

import (
	"github.com/jcsvwinston/GoFrame/pkg/quark"
	"testing"
)

func TestMSSQLDialect(t *testing.T) {
	d := quark.MSSQL()
	
	if d.Name() != "mssql" {
		t.Errorf("expected mssql, got %s", d.Name())
	}
	
	if d.Placeholder(1) != "@p1" {
		t.Errorf("expected @p1, got %s", d.Placeholder(1))
	}
	
	if d.Quote("user") != "[user]" {
		t.Errorf("expected [user], got %s", d.Quote("user"))
	}
	
	limitOffset := d.LimitOffset(10, 20)
	expected := "OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY"
	if limitOffset != expected {
		t.Errorf("expected %s, got %s", expected, limitOffset)
	}
	
	if d.SupportsReturning() {
		t.Error("mssql should not support returning in current implementation")
	}
}

func TestOracleDialect(t *testing.T) {
	d := quark.Oracle()
	
	if d.Name() != "oracle" {
		t.Errorf("expected oracle, got %s", d.Name())
	}
	
	if d.Placeholder(1) != ":1" {
		t.Errorf("expected :1, got %s", d.Placeholder(1))
	}
	
	if d.Quote("user") != "\"user\"" {
		t.Errorf("expected \"user\", got %s", d.Quote("user"))
	}
	
	limitOffset := d.LimitOffset(10, 20)
	expected := "OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY"
	if limitOffset != expected {
		t.Errorf("expected %s, got %s", expected, limitOffset)
	}
	
	if !d.SupportsReturning() {
		t.Error("oracle should support returning")
	}
	
	returning := d.Returning("id")
	expectedReturning := "RETURNING \"id\" INTO ?"
	if returning != expectedReturning {
		t.Errorf("expected %s, got %s", expectedReturning, returning)
	}
}

func TestDetectDialect(t *testing.T) {
	tests := []struct {
		driver string
		want   string
	}{
		{"sqlserver", "mssql"},
		{"mssql", "mssql"},
		{"oracle", "oracle"},
		{"godror", "oracle"},
	}
	
	for _, tt := range tests {
		d, err := quark.DetectDialect(tt.driver)
		if err != nil {
			t.Errorf("quark.DetectDialect(%s) error: %v", tt.driver, err)
			continue
		}
		if d.Name() != tt.want {
			t.Errorf("quark.DetectDialect(%s) = %s, want %s", tt.driver, d.Name(), tt.want)
		}
	}
}
