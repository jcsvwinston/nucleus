package model

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// Test models

type TestUser struct {
	BaseModel
	Email  string `db:"column:email;required" json:"email" validate:"required,email" admin:"list,search"`
	Name   string `db:"column:name;required" json:"name" validate:"required" admin:"list,search"`
	Role   string `db:"column:role" json:"role" admin:"list,filter,choices:admin|Admin;user|User;moderator|Moderator"`
	Active bool   `db:"column:active" json:"active" admin:"list,filter"`
}

type TestProduct struct {
	BaseModel
	Name        string  `db:"column:name;required" json:"name" admin:"list,search"`
	Description string  `db:"column:description" json:"description" admin:"list"`
	Price       float64 `db:"column:price;required" json:"price" admin:"list"`
	CategoryID  uint    `db:"column:category_id" json:"category_id"`
	Category    *TestCategory
}

type TestCategory struct {
	BaseModel
	Name string `db:"column:name;required" json:"name" admin:"list,search"`
}

type TestDBTagModel struct {
	ID        uint      `db:"pk"`
	Email     string    `db:"column:email_addr;required"`
	CreatedAt time.Time `db:"readonly"`
}

type TestCustomPKModel struct {
	OrderCode string `db:"column:order_code;pk"`
	Name      string `db:"column:name;required"`
}

type TestExplicitFKAndIndexesModel struct {
	BaseModel
	TenantID   uint   `db:"column:tenant_id;index:idx_orders_tenant_created;unique:uq_orders_tenant_external"`
	CreatedAt  int64  `db:"column:created_at;index:idx_orders_tenant_created"`
	ExternalID string `db:"column:external_id;unique:uq_orders_tenant_external"`
	AccountID  uint   `db:"column:account_id;fk:model=Account,table=accounts,column=id;index"`
}

type TestMultiplePKModel struct {
	ID    uint `db:"pk"`
	Other uint `db:"pk"`
}

type TestInvalidFKModel struct {
	BaseModel
	OwnerID uint `db:"column:owner_id;fk:model=,column=id"`
}

type TestMixedIndexKindModel struct {
	BaseModel
	TenantID uint `db:"column:tenant_id;index:tenant_lookup"`
	Code     uint `db:"column:code;unique:tenant_lookup"`
}

// --- Fields tests ---

func TestInferHTMLType(t *testing.T) {
	tests := []struct {
		goType, fieldName, expected string
	}{
		{"string", "Email", "email"},
		{"string", "Password", "password"},
		{"string", "Name", "text"},
		{"string", "Description", "textarea"},
		{"string", "WebsiteURL", "url"},
		{"int", "Count", "number"},
		{"float64", "Price", "number"},
		{"bool", "Active", "checkbox"},
		{"time.Time", "CreatedAt", "datetime-local"},
	}
	for _, tt := range tests {
		result := inferHTMLType(tt.goType, tt.fieldName)
		if result != tt.expected {
			t.Errorf("inferHTMLType(%s, %s) = %s, want %s", tt.goType, tt.fieldName, result, tt.expected)
		}
	}
}

func TestParseAdminTag(t *testing.T) {
	opts := parseAdminTag("list,search,filter,label:Correo")
	if !opts.IsList || !opts.IsSearch || !opts.IsFilter {
		t.Error("expected list, search, filter to be true")
	}
	if opts.Label != "Correo" {
		t.Errorf("expected label Correo, got %s", opts.Label)
	}
}

func TestParseAdminTag_Exclude(t *testing.T) {
	opts := parseAdminTag("-")
	if !opts.IsExcluded {
		t.Error("expected excluded")
	}
}

func TestParseAdminTag_Choices(t *testing.T) {
	opts := parseAdminTag("list,choices:admin|Admin;user|User")
	if len(opts.Choices) != 2 {
		t.Fatalf("expected 2 choices, got %d", len(opts.Choices))
	}
	if opts.Choices[0].Value != "admin" || opts.Choices[0].Label != "Admin" {
		t.Errorf("unexpected choice: %+v", opts.Choices[0])
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct{ in, out string }{
		{"CreatedAt", "created_at"},
		{"ID", "id"},
		{"UserID", "user_id"},
		{"Name", "name"},
	}
	for _, tt := range tests {
		if got := toSnakeCase(tt.in); got != tt.out {
			t.Errorf("toSnakeCase(%s) = %s, want %s", tt.in, got, tt.out)
		}
	}
}

func TestToPlural(t *testing.T) {
	tests := []struct{ in, out string }{
		{"User", "Users"},
		{"Category", "Categories"},
		{"Box", "Boxes"},
		{"Dish", "Dishes"},
	}
	for _, tt := range tests {
		if got := toPlural(tt.in); got != tt.out {
			t.Errorf("toPlural(%s) = %s, want %s", tt.in, got, tt.out)
		}
	}
}

// --- Meta tests ---

func TestExtractMeta_User(t *testing.T) {
	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "TestUser" {
		t.Errorf("expected TestUser, got %s", meta.Name)
	}
	if meta.PrimaryKey != "ID" {
		t.Errorf("expected PK=ID, got %s", meta.PrimaryKey)
	}

	// Should have flattened BaseModel fields (ID, CreatedAt, UpdatedAt)
	// plus Email, Name, Role, Active
	fieldNames := make(map[string]bool)
	for _, f := range meta.Fields {
		fieldNames[f.Name] = true
	}
	for _, name := range []string{"ID", "CreatedAt", "UpdatedAt", "Email", "Name", "Role", "Active"} {
		if !fieldNames[name] {
			t.Errorf("expected field %s", name)
		}
	}
}

func TestExtractMeta_ForeignKey(t *testing.T) {
	meta, err := ExtractMeta(&TestProduct{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(meta.ForeignKeys) != 1 {
		t.Fatalf("expected 1 FK, got %d", len(meta.ForeignKeys))
	}
	fk := meta.ForeignKeys[0]
	if fk.FieldName != "CategoryID" || fk.ForeignModel != "Category" {
		t.Errorf("unexpected FK: %+v", fk)
	}
}

func TestExtractMeta_AdminTags(t *testing.T) {
	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range meta.Fields {
		if f.Name == "Email" {
			if !f.IsList || !f.IsSearch {
				t.Error("Email should be list+search")
			}
		}
		if f.Name == "Role" {
			if !f.IsList || !f.IsFilter {
				t.Error("Role should be list+filter")
			}
			if len(f.Choices) != 3 {
				t.Errorf("Role should have 3 choices, got %d", len(f.Choices))
			}
		}
	}
}

func TestExtractMeta_DBTagSupport(t *testing.T) {
	meta, err := ExtractMeta(&TestDBTagModel{})
	if err != nil {
		t.Fatal(err)
	}

	field := map[string]FieldMeta{}
	for _, f := range meta.Fields {
		field[f.Name] = f
	}

	if !field["ID"].IsPK {
		t.Error("ID should be primary key from db tag")
	}
	if field["Email"].Column != "email_addr" {
		t.Fatalf("expected email column email_addr, got %s", field["Email"].Column)
	}
	if !field["Email"].IsRequired {
		t.Error("Email should be required from db tag")
	}
	if !field["CreatedAt"].IsReadOnly {
		t.Error("CreatedAt should be readonly from db tag")
	}
}

func TestExtractMeta_CustomPKAndColumnMapping(t *testing.T) {
	meta, err := ExtractMeta(&TestCustomPKModel{})
	if err != nil {
		t.Fatal(err)
	}
	if meta.PrimaryKey != "OrderCode" {
		t.Fatalf("expected OrderCode as PK, got %s", meta.PrimaryKey)
	}

	fieldByName := make(map[string]FieldMeta, len(meta.Fields))
	for _, f := range meta.Fields {
		fieldByName[f.Name] = f
	}
	pk := fieldByName["OrderCode"]
	if !pk.IsPK {
		t.Fatal("OrderCode should be marked as PK")
	}
	if pk.Column != "order_code" {
		t.Fatalf("expected order_code column, got %s", pk.Column)
	}
}

func TestExtractMeta_ExplicitFKAndIndexDeclarations(t *testing.T) {
	meta, err := ExtractMeta(&TestExplicitFKAndIndexesModel{})
	if err != nil {
		t.Fatal(err)
	}

	var accountFK ForeignKey
	for _, fk := range meta.ForeignKeys {
		if fk.FieldName == "AccountID" {
			accountFK = fk
			break
		}
	}
	if accountFK.FieldName == "" {
		t.Fatal("expected AccountID FK declaration")
	}
	if accountFK.ForeignModel != "Account" {
		t.Fatalf("expected FK model Account, got %s", accountFK.ForeignModel)
	}
	if accountFK.ForeignTable != "accounts" {
		t.Fatalf("expected FK table accounts, got %s", accountFK.ForeignTable)
	}
	if accountFK.ForeignColumn != "id" {
		t.Fatalf("expected FK column id, got %s", accountFK.ForeignColumn)
	}

	indexByName := make(map[string]IndexMeta, len(meta.Indexes))
	for _, idx := range meta.Indexes {
		indexByName[idx.Name] = idx
	}

	composite, ok := indexByName["idx_orders_tenant_created"]
	if !ok {
		t.Fatal("expected composite non-unique index idx_orders_tenant_created")
	}
	if composite.Unique {
		t.Fatal("idx_orders_tenant_created should be non-unique")
	}
	if len(composite.Columns) != 2 || composite.Columns[0] != "tenant_id" || composite.Columns[1] != "created_at" {
		t.Fatalf("unexpected composite columns: %+v", composite.Columns)
	}

	uniqueComposite, ok := indexByName["uq_orders_tenant_external"]
	if !ok {
		t.Fatal("expected composite unique index uq_orders_tenant_external")
	}
	if !uniqueComposite.Unique {
		t.Fatal("uq_orders_tenant_external should be unique")
	}
	if len(uniqueComposite.Columns) != 2 || uniqueComposite.Columns[0] != "tenant_id" || uniqueComposite.Columns[1] != "external_id" {
		t.Fatalf("unexpected unique composite columns: %+v", uniqueComposite.Columns)
	}

	foundDefaultAccountIndex := false
	for _, idx := range meta.Indexes {
		if !idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == "account_id" {
			foundDefaultAccountIndex = true
			break
		}
	}
	if !foundDefaultAccountIndex {
		t.Fatalf("expected default non-unique index for account_id column, got %+v", meta.Indexes)
	}
}

func TestExtractMeta_ErrorOnMultiplePrimaryKeys(t *testing.T) {
	_, err := ExtractMeta(&TestMultiplePKModel{})
	if err == nil {
		t.Fatal("expected error for multiple primary keys")
	}
	if !strings.Contains(err.Error(), "multiple primary keys") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractMeta_ErrorOnInvalidFKSpec(t *testing.T) {
	_, err := ExtractMeta(&TestInvalidFKModel{})
	if err == nil {
		t.Fatal("expected error for invalid fk spec")
	}
	if !strings.Contains(err.Error(), "fk model value cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractMeta_ErrorOnMixedIndexKind(t *testing.T) {
	_, err := ExtractMeta(&TestMixedIndexKindModel{})
	if err == nil {
		t.Fatal("expected error for mixed index uniqueness declarations")
	}
	if !strings.Contains(err.Error(), "mixes unique and non-unique") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Registry tests ---

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(&TestUser{}, ModelConfig{
		Icon:         "U",
		ListFields:   []string{"ID", "Email", "Name"},
		SearchFields: []string{"Email", "Name"},
	})
	if err != nil {
		t.Fatal(err)
	}

	meta, ok := reg.Get("TestUser")
	if !ok {
		t.Fatal("expected to find TestUser")
	}
	if meta.Config.Icon != "U" {
		t.Errorf("expected icon U, got %s", meta.Config.Icon)
	}
	if reg.Count() != 1 {
		t.Errorf("expected count 1, got %d", reg.Count())
	}
}

func TestRegistry_All(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&TestUser{})
	reg.Register(&TestProduct{})
	all := reg.All()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
	// Should be sorted alphabetically
	if all[0].Name > all[1].Name {
		t.Error("expected alphabetical order")
	}
}

// --- CRUD tests (integration with SQLite) ---

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}
	d, err := db.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatalf("failed to access SQL DB: %v", err)
	}

	if err := createModelTestSchema(sqlDB); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	return sqlDB
}

func createModelTestSchema(sqlDB *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS test_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			email TEXT NOT NULL,
			name TEXT NOT NULL,
			role TEXT,
			active BOOLEAN NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS test_categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			name TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS test_products (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			name TEXT NOT NULL,
			description TEXT,
			price REAL NOT NULL,
			category_id INTEGER
		)`,
	}
	for _, statement := range statements {
		if _, err := sqlDB.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func TestCRUD_CreateAndFindByID(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	user := &TestUser{
		Email:  "test@example.com",
		Name:   "Test User",
		Role:   "admin",
		Active: true,
	}
	if err := crud.Create(context.Background(), user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if user.ID == 0 {
		t.Error("ID should be set after create")
	}

	found, err := crud.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	foundUser := found.(*TestUser)
	if foundUser.Email != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", foundUser.Email)
	}
}

func TestCRUD_FindAll_Pagination(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 2, SearchFields: []string{"Email", "Name"}}
	// Re-apply search fields to meta.Fields
	for i := range meta.Fields {
		if meta.Fields[i].Name == "Email" || meta.Fields[i].Name == "Name" {
			meta.Fields[i].IsSearch = true
		}
	}

	crud := NewCRUD(sqlDB, meta, nil)

	// Create 5 test users
	for i := 0; i < 5; i++ {
		crud.Create(context.Background(), &TestUser{
			Email:  "user" + string(rune('0'+i)) + "@test.com",
			Name:   "User " + string(rune('0'+i)),
			Active: true,
		})
	}

	// Page 1
	result, err := crud.FindAll(context.Background(), QueryOpts{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Total may be -1 if database statistics are not available (estimation optimization)
	if result.Total != 5 && result.Total != -1 {
		t.Errorf("expected total 5 or -1 (estimate not available), got %d", result.Total)
	}
	// TotalPages calculation depends on Total being accurate
	if result.Total == 5 && result.TotalPages != 3 {
		t.Errorf("expected 3 pages when total=5, got %d", result.TotalPages)
	}
	// When total is unknown (-1), TotalPages is based on hasMore detection
	if result.Total == -1 {
		// With 5 items and page_size=2, page 1 has more items, so totalPages = page + 1 = 2
		if result.TotalPages != 2 {
			t.Errorf("expected 2 pages when total=-1 with hasMore=true, got %d", result.TotalPages)
		}
	}
}

func TestCRUD_Update(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	user := &TestUser{Email: "update@test.com", Name: "Original"}
	crud.Create(context.Background(), user)

	err := crud.Update(context.Background(), user.ID, map[string]interface{}{"name": "Updated"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	found, _ := crud.FindByID(context.Background(), user.ID)
	if found.(*TestUser).Name != "Updated" {
		t.Errorf("expected Updated, got %s", found.(*TestUser).Name)
	}
}

func TestCRUD_Delete(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	user := &TestUser{Email: "delete@test.com", Name: "ToDelete"}
	crud.Create(context.Background(), user)

	err := crud.Delete(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Should be soft-deleted (BaseModel has DeletedAt)
	_, err = crud.FindByID(context.Background(), user.ID)
	if err == nil {
		t.Error("expected not found after delete")
	}
}

func TestCRUD_FindByID_NotFound(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	_, err := crud.FindByID(context.Background(), 999)
	if err == nil {
		t.Error("expected not found error")
	}
}

// Ensure time fields are set
func TestBaseModel_Timestamps(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	user := &TestUser{Email: "ts@test.com", Name: "Timestamps"}
	crud.Create(context.Background(), user)

	if user.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if user.UpdatedAt.Before(time.Now().Add(-5 * time.Second)) {
		t.Error("UpdatedAt should be recent")
	}
}

func TestCRUD_Hooks_ReceiveSQLContext(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})

	beforeCalled := false
	afterCalled := false
	meta.Config = ModelConfig{
		PageSize: 25,
		BeforeCreate: func(hookCtx HookContext, entity interface{}) error {
			beforeCalled = true
			if hookCtx.Engine != HookEngineSQL {
				t.Fatalf("expected engine %s, got %s", HookEngineSQL, hookCtx.Engine)
			}
			if hookCtx.DB == nil {
				t.Fatal("expected non-nil SQL DB handle in hook context")
			}
			if hookCtx.Tx != nil {
				t.Fatal("expected nil Tx in default CRUD hook context")
			}
			if hookCtx.Context == nil {
				t.Fatal("expected non-nil context in hook context")
			}
			return nil
		},
		AfterCreate: func(hookCtx HookContext, entity interface{}) error {
			afterCalled = true
			if hookCtx.Engine != HookEngineSQL {
				t.Fatalf("expected engine %s, got %s", HookEngineSQL, hookCtx.Engine)
			}
			if hookCtx.DB == nil {
				t.Fatal("expected non-nil SQL DB handle in hook context")
			}
			return nil
		},
	}

	crud := NewCRUD(sqlDB, meta, nil)
	if err := crud.Create(context.Background(), &TestUser{
		Email: "hook-sql@test.com",
		Name:  "Hook SQL",
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !beforeCalled {
		t.Fatal("expected BeforeCreate hook to be called")
	}
	if !afterCalled {
		t.Fatal("expected AfterCreate hook to be called")
	}
}

// --- Utility function tests ---

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2024-01-15T10:30:00Z", false},
		{"2024-01-15T10:30:00.123Z", false},
		{"2024-01-15 10:30:00", false},
		{"2024-01-15", false},
		{"", false}, // Empty string returns zero time
		{"invalid", true},
	}
	for _, tt := range tests {
		_, err := parseTimeString(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseTimeString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		input   interface{}
		want    int64
		wantErr bool
	}{
		{int64(42), 42, false},
		{int(42), 42, false},
		{int32(42), 42, false},
		{uint64(42), 42, false},
		{uint(42), 42, false},
		{float64(42.5), 42, false},
		{string("42"), 42, false},
		{[]byte("42"), 42, false},
		{"invalid", 0, true},
		{nil, 0, true},
	}
	for _, tt := range tests {
		got, err := toInt64(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("toInt64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("toInt64(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input   interface{}
		want    float64
		wantErr bool
	}{
		{float64(42.5), 42.5, false},
		{float32(42.5), 42.5, false},
		{int64(42), 42.0, false},
		{int(42), 42.0, false},
		{string("42.5"), 42.5, false},
		{[]byte("42.5"), 42.5, false},
		{"invalid", 0, true},
		{nil, 0, true},
	}
	for _, tt := range tests {
		got, err := toFloat64(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("toFloat64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("toFloat64(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestStartsWithUpper(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Hello", true},
		{"hello", false},
		{"", false},
		{"A", true},
		{"a", false},
		{"HELLO", true},
	}
	for _, tt := range tests {
		if got := startsWithUpper(tt.input); got != tt.want {
			t.Errorf("startsWithUpper(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
