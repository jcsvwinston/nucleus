package cli

import (
	"strings"
	"testing"
)

func TestTableToStructName(t *testing.T) {
	tests := map[string]string{
		"users":         "User",
		"categories":    "Category",
		"blog_posts":    "BlogPost",
		"status":        "Statu",
		"123_bad_table": "Table123BadTable",
	}

	for in, want := range tests {
		got := tableToStructName(in)
		if got != want {
			t.Fatalf("tableToStructName(%q): got %q want %q", in, got, want)
		}
	}
}

func TestMapInspectTypeSQLite(t *testing.T) {
	if got := mapInspectType(dbFlavorSQLite, "INTEGER", false, true); got != "int64" {
		t.Fatalf("unexpected sqlite integer type: %s", got)
	}
	if got := mapInspectType(dbFlavorSQLite, "DATETIME", true, false); got != "*time.Time" {
		t.Fatalf("unexpected sqlite datetime type: %s", got)
	}
	if got := mapInspectType(dbFlavorSQLite, "TEXT", true, false); got != "*string" {
		t.Fatalf("unexpected sqlite text type: %s", got)
	}
}

func TestBuildInspectTag(t *testing.T) {
	if got := buildInspectTag("id", false, true); got != `db:"column:id;primaryKey;required"` {
		t.Fatalf("unexpected inspect tag for PK: %s", got)
	}
	if got := buildInspectTag("name", true, false); got != `db:"column:name"` {
		t.Fatalf("unexpected inspect tag for nullable field: %s", got)
	}
}

func TestRenderInspectModels(t *testing.T) {
	src, err := renderInspectModels("models", []inspectModel{
		{
			TableName:  "users",
			StructName: "User",
			Columns: []inspectColumn{
				{ColumnName: "id", FieldName: "ID", GoType: "int64", Tag: `db:"column:id;primaryKey;required"`},
				{ColumnName: "created_at", FieldName: "CreatedAt", GoType: "*time.Time", Tag: `db:"column:created_at"`},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderInspectModels failed: %v", err)
	}

	out := string(src)
	if !strings.Contains(out, "package models") {
		t.Fatalf("rendered source missing package: %s", out)
	}
	if !strings.Contains(out, "import \"time\"") {
		t.Fatalf("rendered source missing time import: %s", out)
	}
	if !strings.Contains(out, "type User struct") {
		t.Fatalf("rendered source missing struct: %s", out)
	}
	if !strings.Contains(out, "func (User) TableName() string") {
		t.Fatalf("rendered source missing TableName method: %s", out)
	}
}
