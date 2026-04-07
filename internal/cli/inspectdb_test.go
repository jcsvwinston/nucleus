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

func TestMapInspectTypeEnterpriseFlavors(t *testing.T) {
	if got := mapInspectType(dbFlavorMSSQL, "bit", false, false); got != "bool" {
		t.Fatalf("unexpected mssql bit type: %s", got)
	}
	if got := mapInspectType(dbFlavorMSSQL, "datetime2", true, false); got != "*time.Time" {
		t.Fatalf("unexpected mssql datetime2 type: %s", got)
	}
	if got := mapInspectType(dbFlavorOracle, "NUMBER", false, true); got != "int64" {
		t.Fatalf("unexpected oracle number type: %s", got)
	}
	if got := mapInspectType(dbFlavorOracle, "TIMESTAMP", true, false); got != "*time.Time" {
		t.Fatalf("unexpected oracle timestamp type: %s", got)
	}
}

func TestBuildInspectTag(t *testing.T) {
	if got := buildInspectTag("id", false, true, nil, nil); got != `db:"column:id;pk;required"` {
		t.Fatalf("unexpected inspect tag for PK: %s", got)
	}
	if got := buildInspectTag("name", true, false, nil, nil); got != `db:"column:name"` {
		t.Fatalf("unexpected inspect tag for nullable field: %s", got)
	}
	fk := &introspectedForeignKey{ForeignTable: "customers", ForeignColumn: "id"}
	refs := []introspectedIndexRef{
		{Unique: false},
		{Name: "uq_customers_tenant_email", Unique: true},
	}
	if got := buildInspectTag("customer_id", false, false, fk, refs); got != `db:"column:customer_id;required;fk:customers.id;index;unique:uq_customers_tenant_email"` {
		t.Fatalf("unexpected inspect tag with fk/index metadata: %s", got)
	}
}

func TestBuildIndexRefsByColumn(t *testing.T) {
	refs := buildIndexRefsByColumn([]introspectedIndex{
		{Name: "idx_orders_tenant", Unique: false, Columns: []string{"tenant_id"}},
		{Name: "uq_orders_tenant_external", Unique: true, Columns: []string{"tenant_id", "external_id"}},
	})

	tenantRefs := refs["tenant_id"]
	if len(tenantRefs) != 2 {
		t.Fatalf("expected 2 refs for tenant_id, got %+v", tenantRefs)
	}
	if tenantRefs[0].Name != "" || tenantRefs[0].Unique {
		t.Fatalf("expected unnamed non-unique single-column ref first, got %+v", tenantRefs[0])
	}
	if tenantRefs[1].Name != "uq_orders_tenant_external" || !tenantRefs[1].Unique {
		t.Fatalf("expected named unique composite ref second, got %+v", tenantRefs[1])
	}

	externalRefs := refs["external_id"]
	if len(externalRefs) != 1 || externalRefs[0].Name != "uq_orders_tenant_external" || !externalRefs[0].Unique {
		t.Fatalf("unexpected refs for external_id: %+v", externalRefs)
	}
}

func TestRenderInspectModels(t *testing.T) {
	src, err := renderInspectModels("models", []inspectModel{
		{
			TableName:  "users",
			StructName: "User",
			Columns: []inspectColumn{
				{ColumnName: "id", FieldName: "ID", GoType: "int64", Tag: `db:"column:id;pk;required"`},
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
