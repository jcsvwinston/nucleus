package model

import (
	"context"
	"testing"
	"time"

	"github.com/goframe/goframe/pkg/app"
	"github.com/goframe/goframe/pkg/db"
	"github.com/goframe/goframe/pkg/observe"
)

// Test models

type TestUser struct {
	BaseModel
	Email  string `gorm:"uniqueIndex;not null" json:"email" validate:"required,email" admin:"list,search"`
	Name   string `gorm:"not null" json:"name" validate:"required" admin:"list,search"`
	Role   string `gorm:"default:'user'" json:"role" admin:"list,filter,choices:admin|Admin;user|User;moderator|Moderator"`
	Active bool   `gorm:"default:true" json:"active" admin:"list,filter"`
}

type TestProduct struct {
	BaseModel
	Name        string  `gorm:"not null" json:"name" admin:"list,search"`
	Description string  `json:"description" admin:"list"`
	Price       float64 `gorm:"not null" json:"price" admin:"list"`
	CategoryID  uint    `json:"category_id"`
	Category    *TestCategory
}

type TestCategory struct {
	BaseModel
	Name string `gorm:"not null" json:"name" admin:"list,search"`
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
		{"ID", "i_d"},
		{"UserID", "user_i_d"},
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

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := &app.Config{
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}
	d, err := db.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	// Auto-migrate test models
	d.GormDB().AutoMigrate(&TestUser{}, &TestProduct{}, &TestCategory{})
	return d
}

func TestCRUD_CreateAndFindByID(t *testing.T) {
	d := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(d.GormDB(), meta, nil)

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
	d := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 2, SearchFields: []string{"Email", "Name"}}
	// Re-apply search fields to meta.Fields
	for i := range meta.Fields {
		if meta.Fields[i].Name == "Email" || meta.Fields[i].Name == "Name" {
			meta.Fields[i].IsSearch = true
		}
	}

	crud := NewCRUD(d.GormDB(), meta, nil)

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
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if result.TotalPages != 3 {
		t.Errorf("expected 3 pages, got %d", result.TotalPages)
	}
}

func TestCRUD_Update(t *testing.T) {
	d := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(d.GormDB(), meta, nil)

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
	d := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(d.GormDB(), meta, nil)

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
	d := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(d.GormDB(), meta, nil)

	_, err := crud.FindByID(context.Background(), 999)
	if err == nil {
		t.Error("expected not found error")
	}
}

// Ensure time fields are set
func TestBaseModel_Timestamps(t *testing.T) {
	d := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(d.GormDB(), meta, nil)

	user := &TestUser{Email: "ts@test.com", Name: "Timestamps"}
	crud.Create(context.Background(), user)

	if user.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if user.UpdatedAt.Before(time.Now().Add(-5 * time.Second)) {
		t.Error("UpdatedAt should be recent")
	}
}
