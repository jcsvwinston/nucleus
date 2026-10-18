package model

import (
	"strings"
	"testing"
)

func TestBuildSQLiteMigrationScaffold_WithForeignKeysAndIndexes(t *testing.T) {
	meta := &ModelMeta{
		Name:  "Order",
		Table: "orders",
		Fields: []FieldMeta{
			{Name: "ID", Column: "id", GoType: "uint", IsPK: true},
			{Name: "TenantID", Column: "tenant_id", GoType: "uint", IsRequired: true},
			{Name: "CustomerID", Column: "customer_id", GoType: "uint", IsRequired: true, IsForeignKey: true, ForeignTable: "customers", ForeignColumn: "id"},
			{Name: "ExternalID", Column: "external_id", GoType: "string", IsRequired: true},
		},
		PrimaryKey: "ID",
		ForeignKeys: []ForeignKey{
			{
				FieldName:     "CustomerID",
				Column:        "customer_id",
				ForeignTable:  "customers",
				ForeignColumn: "id",
			},
		},
		Indexes: []IndexMeta{
			{Name: "idx_orders_tenant_id", Columns: []string{"tenant_id"}},
			{Name: "uq_orders_tenant_external", Columns: []string{"tenant_id", "external_id"}, Unique: true},
		},
	}

	up, down, err := BuildSQLiteMigrationScaffold(meta)
	if err != nil {
		t.Fatalf("BuildSQLiteMigrationScaffold failed: %v", err)
	}

	if !strings.Contains(up, `CREATE TABLE IF NOT EXISTS "orders"`) {
		t.Fatalf("expected create table statement, got:\n%s", up)
	}
	if !strings.Contains(up, `"id" INTEGER PRIMARY KEY AUTOINCREMENT`) {
		t.Fatalf("expected integer autoincrement PK, got:\n%s", up)
	}
	if !strings.Contains(up, `CONSTRAINT "fk_orders_customer_id__customers_id" FOREIGN KEY ("customer_id") REFERENCES "customers" ("id")`) {
		t.Fatalf("expected deterministic FK constraint, got:\n%s", up)
	}
	if !strings.Contains(up, `CREATE INDEX IF NOT EXISTS "idx_orders_tenant_id" ON "orders" ("tenant_id");`) {
		t.Fatalf("expected non-unique index statement, got:\n%s", up)
	}
	if !strings.Contains(up, `CREATE UNIQUE INDEX IF NOT EXISTS "uq_orders_tenant_external" ON "orders" ("tenant_id", "external_id");`) {
		t.Fatalf("expected unique composite index statement, got:\n%s", up)
	}

	firstDrop := strings.Index(down, `DROP INDEX IF EXISTS "uq_orders_tenant_external";`)
	secondDrop := strings.Index(down, `DROP INDEX IF EXISTS "idx_orders_tenant_id";`)
	if firstDrop < 0 || secondDrop < 0 || firstDrop > secondDrop {
		t.Fatalf("expected reverse index drop order, got:\n%s", down)
	}
	if !strings.Contains(down, `DROP TABLE IF EXISTS "orders";`) {
		t.Fatalf("expected table drop statement, got:\n%s", down)
	}
}

func TestBuildSQLiteMigrationScaffold_StringPrimaryKey(t *testing.T) {
	meta := &ModelMeta{
		Name:  "ApiKey",
		Table: "api_keys",
		Fields: []FieldMeta{
			{Name: "Key", Column: "key", GoType: "string", IsPK: true},
			{Name: "Label", Column: "label", GoType: "string"},
		},
		PrimaryKey: "Key",
	}

	up, _, err := BuildSQLiteMigrationScaffold(meta)
	if err != nil {
		t.Fatalf("BuildSQLiteMigrationScaffold failed: %v", err)
	}
	if !strings.Contains(up, `"key" TEXT PRIMARY KEY`) {
		t.Fatalf("expected TEXT PRIMARY KEY for string key, got:\n%s", up)
	}
}

func TestBuildSQLiteMigrationScaffold_InvalidTableName(t *testing.T) {
	_, _, err := BuildSQLiteMigrationScaffold(&ModelMeta{
		Table: "bad-table-name",
		Fields: []FieldMeta{
			{Name: "ID", Column: "id", GoType: "uint", IsPK: true},
		},
	})
	if err == nil {
		t.Fatal("expected invalid table name error")
	}
}
