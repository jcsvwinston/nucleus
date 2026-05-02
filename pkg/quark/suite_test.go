package quark

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// SharedSuite runs a comprehensive set of tests against a given client.
func SharedSuite(t *testing.T, client *Client) {
	ctx := context.Background()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(ctx, t, client)
	})

	t.Run("QueryBuilder", func(t *testing.T) {
		testQueryBuilder(ctx, t, client)
	})

	t.Run("Transactions", func(t *testing.T) {
		testTransactions(ctx, t, client)
	})

	t.Run("Relationships", func(t *testing.T) {
		testRelationships(ctx, t, client)
	})

	t.Run("Hooks", func(t *testing.T) {
		testHooks(ctx, t, client)
	})

	t.Run("Validation", func(t *testing.T) {
		testValidation(ctx, t, client)
	})

	t.Run("SoftDelete", func(t *testing.T) {
		testSoftDelete(ctx, t, client)
	})

	t.Run("Pagination", func(t *testing.T) {
		testPagination(ctx, t, client)
	})

	t.Run("MultiTenant", func(t *testing.T) {
		testMultiTenant(ctx, t, client)
	})

	t.Run("Events", func(t *testing.T) {
		testEvents(ctx, t, client)
	})

	t.Run("Middleware", func(t *testing.T) {
		testMiddleware(ctx, t, client)
	})

	t.Run("Raw", func(t *testing.T) {
		testRaw(ctx, t, client)
	})

	t.Run("DatabasePerTenant", func(t *testing.T) {
		testDatabasePerTenant(ctx, t, client)
	})

	t.Run("Stress", func(t *testing.T) {
		testStress(ctx, t, client)
	})
}

func testCRUD(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS suite_users")
	type SuiteUser struct {
		ID    int64  `db:"id" pk:"true"`
		Name  string `db:"name"`
		Email string `db:"email"`
	}

	// Setup table for the engine
	err := client.Migrate(ctx, &SuiteUser{})
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	// Create
	u := SuiteUser{Name: "Suite User", Email: "suite@test.com"}
	if err := For[SuiteUser](ctx, client).Create(&u); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if u.ID == 0 {
		t.Error("expected ID to be set")
	}

	// Find
	found, err := For[SuiteUser](ctx, client).Find(u.ID)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found.Name != u.Name {
		t.Errorf("expected name %s, got %s", u.Name, found.Name)
	}

	// Update
	found.Name = "Updated Name"
	if _, err := For[SuiteUser](ctx, client).Update(&found); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Verify Update
	verify, _ := For[SuiteUser](ctx, client).Find(u.ID)
	if verify.Name != "Updated Name" {
		t.Errorf("expected updated name, got %s", verify.Name)
	}

	// Delete
	if _, err := For[SuiteUser](ctx, client).HardDelete(&verify); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify Delete
	_, err = For[SuiteUser](ctx, client).Find(u.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func testQueryBuilder(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS q_b_users")
	type QBUser struct {
		ID   int64  `db:"id" pk:"true"`
		Name string `db:"name"`
		Age  int    `db:"age"`
		City string `db:"city"`
	}

	client.Migrate(ctx, &QBUser{})
	
	users := []QBUser{
		{Name: "Alice", Age: 20, City: "Madrid"},
		{Name: "Charlie", Age: 30, City: "Madrid"},
		{Name: "Bob", Age: 40, City: "Barcelona"},
	}
	for i := range users {
		For[QBUser](ctx, client).Create(&users[i])
	}

	// Test Simple Where
	madrid, _ := For[QBUser](ctx, client).Where("city", "=", "Madrid").List()
	if len(madrid) != 2 {
		t.Errorf("expected 2 users in Madrid, got %d", len(madrid))
	}

	// Test And
	oldMadrid, _ := For[QBUser](ctx, client).Where("city", "=", "Madrid").Where("age", ">", 25).List()
	if len(oldMadrid) != 1 {
		t.Errorf("expected 1 old user in Madrid, got %d", len(oldMadrid))
	}

	// Test Or
	orResult, _ := For[QBUser](ctx, client).Where("city", "=", "Barcelona").Or(func(q *Query[QBUser]) *Query[QBUser] {
		return q.Where("age", "<", 25)
	}).List()
	if len(orResult) != 2 {
		t.Errorf("expected 2 users for OR condition, got %d", len(orResult) )
	}

	// Test In
	inResult, _ := For[QBUser](ctx, client).WhereIn("age", []any{20, 40}).List()
	if len(inResult) != 2 {
		t.Errorf("expected 2 users for IN condition, got %d", len(inResult))
	}

	// Test Between
	betweenResult, _ := For[QBUser](ctx, client).WhereBetween("age", 25, 35).List()
	if len(betweenResult) != 1 {
		t.Errorf("expected 1 user for BETWEEN condition, got %d", len(betweenResult))
	}

	// Test Select
	selResult, _ := For[QBUser](ctx, client).Select("name", "city").Where("age", "=", 30).List()
	if len(selResult) != 1 {
		t.Errorf("expected 1 user for Select, got %d", len(selResult))
	}
	if selResult[0].Name != "Charlie" || selResult[0].Age != 0 {
		if selResult[0].Age != 0 {
			t.Errorf("expected Age to be zero (not selected), got %d", selResult[0].Age)
		}
	}
}

func testTransactions(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS tx_users")
	type TxUser struct {
		ID   int64  `db:"id" pk:"true"`
		Name string `db:"name"`
	}
	client.Migrate(ctx, &TxUser{})

	// Successful Tx
	err := client.Tx(ctx, func(tx *Tx) error {
		return ForTx[TxUser](ctx, tx).Create(&TxUser{Name: "Tx User"})
	})
	if err != nil {
		t.Fatalf("tx failed: %v", err)
	}

	// Rollback Tx
	err = client.Tx(ctx, func(tx *Tx) error {
		ForTx[TxUser](ctx, tx).Create(&TxUser{Name: "Rollback User"})
		return fmt.Errorf("intentional rollback")
	})
	if err == nil {
		t.Error("expected error from tx, got nil")
	}

	// Verify results
	count, _ := For[TxUser](ctx, client).Count()
	if count != 1 {
		t.Errorf("expected 1 user after tx and rollback, got %d", count)
	}
}

func testRelationships(ctx context.Context, t *testing.T, client *Client) {
	// Already mostly covered in quark_test.go, but integrated here for all dialects
	// Implement Preload tests for HasMany and BelongsTo
}

func testHooks(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS hook_users")
	type HookUser struct {
		ID        int64      `db:"id" pk:"true"`
		Title     string     `db:"title"`
		DeletedAt *time.Time `db:"deleted_at"`
	}

	client.Migrate(ctx, &HookUser{})
	// Basic test for hooks could be more complex, but we mainly want to ensure they run across dialects
	u := HookUser{Title: "Hook Test"}
	For[HookUser](ctx, client).Create(&u)
	
	// Just verify creation worked
	if u.ID == 0 {
		t.Error("hook user ID not set")
	}
}

func testValidation(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS validateds")
	type Validated struct {
		ID    int64  `db:"id" pk:"true"`
		Email string `db:"email" validate:"required,email"`
	}
	client.Migrate(ctx, &Validated{})

	err := For[Validated](ctx, client).Create(&Validated{Email: "invalid"})
	if err == nil {
		t.Error("expected validation error, got nil")
	}
	client.Raw().Exec("DROP TABLE IF EXISTS validateds")
}

func testSoftDelete(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS posts")
	type Post struct {
		ID        int64      `db:"id" pk:"true"`
		Title     string     `db:"title"`
		DeletedAt *time.Time `db:"deleted_at"`
	}

	client.Migrate(ctx, &Post{})
	p := Post{Title: "Soft Delete Post"}
	For[Post](ctx, client).Create(&p)

	// Soft delete
	rows, err := For[Post](ctx, client).Delete(&p)
	if err != nil || rows != 1 {
		t.Fatalf("soft delete failed: %v", err)
	}

	// Should not find by default
	_, err = For[Post](ctx, client).Find(p.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for soft deleted record, got %v", err)
	}

	// Should find with Unscoped
	found, err := For[Post](ctx, client).Unscoped().Find(p.ID)
	if err != nil {
		t.Fatalf("unscoped find failed: %v", err)
	}
	if found.DeletedAt == nil {
		t.Error("expected DeletedAt to be set")
	}
}

func testPagination(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS logs")
	type Log struct {
		ID  int64  `db:"id" pk:"true"`
		Msg string `db:"msg"`
	}
	client.Migrate(ctx, &Log{})
	for i := 0; i < 50; i++ {
		if err := For[Log](ctx, client).Create(&Log{Msg: "test"}); err != nil {
			t.Fatalf("failed to create log %d: %v", i, err)
		}
	}

	res, err := For[Log](ctx, client).Paginate(10, 1) // Page 1 (offset 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 10 {
		t.Errorf("expected 10 items, got %d", len(res.Items))
	}
	if res.Total != 50 {
		t.Errorf("expected total 50, got %d", res.Total)
	}
}

func testMultiTenant(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS tenant_data")
	type TenantData struct {
		ID       int64  `db:"id" pk:"true"`
		TenantID string `db:"tenant_id"`
		Value    string `db:"value"`
	}
	client.Migrate(ctx, &TenantData{})

	cfg := DefaultTenantConfig()
	cfg.Strategy = RowLevelSecurity
	cfg.BaseClient = client
	
	resolver := func(ctx context.Context) string {
		if tid, ok := ctx.Value("tenant_id").(string); ok {
			return tid
		}
		return ""
	}
	
	router := NewTenantRouter(cfg, resolver, nil)

	client.Raw().Exec("DROP TABLE IF EXISTS tenant_datas")
	client.Migrate(ctx, &TenantData{})

	ctx1 := context.WithValue(context.Background(), "tenant_id", "t1")
	ctx2 := context.WithValue(context.Background(), "tenant_id", "t2")

	For[TenantData](ctx1, router).Create(&TenantData{Value: "V1"})
	For[TenantData](ctx2, router).Create(&TenantData{Value: "V2"})

	// Verify isolation
	v1, _ := For[TenantData](ctx1, router).List()
	if len(v1) != 1 || v1[0].Value != "V1" {
		t.Errorf("tenant 1 isolation failed: %v", v1)
	}

	v2, _ := For[TenantData](ctx2, router).List()
	if len(v2) != 1 || v2[0].Value != "V2" {
		t.Errorf("tenant 2 isolation failed: %v", v2)
	}
}

type mockObserver struct {
	events []QueryEvent
	mu     sync.Mutex
}

func (o *mockObserver) ObserveQuery(e QueryEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
}

func testEvents(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS events_users")
	obs := &mockObserver{}
	// Since client options are applied at New(), we can't easily add an observer to an existing client 
	// unless we use a middleware or the client supports it.
	// Quark Client has an 'observers' slice. Let's see if we can append to it.
	// Actually, it's unexported. But we can create a NEW client with the SAME DB for this test.
	
	c2, _ := New(client.Raw(), WithDialect(client.Dialect()), WithQueryObserver(obs))
	
	type EventUser struct {
		ID   int64  `db:"id" pk:"true"`
		Name string `db:"name"`
	}
	if err := c2.Migrate(ctx, &EventUser{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if err := For[EventUser](ctx, c2).Create(&EventUser{Name: "Event"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := For[EventUser](ctx, c2).List(); err != nil {
		t.Fatalf("list failed: %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(obs.events))
	}
}

type suiteMockMiddleware struct {
	called bool
}

func (m *suiteMockMiddleware) WrapQuery(next QueryFunc) QueryFunc {
	return func(ctx context.Context, exec Executor, sql string, args []any) (*sql.Rows, error) {
		m.called = true
		return next(ctx, exec, sql, args)
	}
}

func (m *suiteMockMiddleware) WrapQueryRow(next QueryRowFunc) QueryRowFunc {
	return next
}

func (m *suiteMockMiddleware) WrapExec(next ExecFunc) ExecFunc {
	return next
}

func testMiddleware(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS mid_users")
	mid := &suiteMockMiddleware{}
	c2, _ := New(client.Raw(), WithDialect(client.Dialect()), WithMiddleware(mid))
	
	type MidUser struct {
		ID int64 `db:"id" pk:"true"`
	}
	c2.Migrate(ctx, &MidUser{})
	For[MidUser](ctx, c2).List()

	if !mid.called {
		t.Error("middleware was not called")
	}
}

func testRaw(ctx context.Context, t *testing.T, client *Client) {
	client.Raw().Exec("DROP TABLE IF EXISTS raw_test")
	// Enable raw queries for this test
	c2, _ := New(client.Raw(), WithDialect(client.Dialect()), WithLimits(Limits{AllowRawQueries: true, MaxResults: 1000, QueryTimeout: time.Second}))
	
	if err := c2.Exec(ctx, "CREATE TABLE raw_test (id INTEGER, name TEXT)"); err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	query := fmt.Sprintf("SELECT * FROM raw_test WHERE id = %s", strings.Join(c2.Dialect().Placeholders(1), ", "))
	if _, err := c2.RawQuery(ctx, query, 1); err != nil {
		t.Fatalf("raw query failed: %v", err)
	}
}

func testDatabasePerTenant(ctx context.Context, t *testing.T, client *Client) {
	factory := func(tenantID string) (*Client, error) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			return nil, err
		}
		return New(db, WithDialect(SQLite()))
	}

	cfg := DefaultTenantConfig()
	cfg.Strategy = DatabasePerTenant
	cfg.MaxCachedPools = 2 // Small limit for eviction test
	
	resolver := func(ctx context.Context) string {
		if tid, ok := ctx.Value("tenant_id").(string); ok {
			return tid
		}
		return ""
	}

	router := NewTenantRouter(cfg, resolver, factory)

	ctx1 := context.WithValue(ctx, "tenant_id", "t1")
	ctx2 := context.WithValue(ctx, "tenant_id", "t2")
	ctx3 := context.WithValue(ctx, "tenant_id", "t3")

	// Trigger cache population
	router.GetClient(ctx1)
	router.GetClient(ctx2)
	
	active := router.ActiveTenants()
	if len(active) != 2 {
		t.Errorf("expected 2 active tenants, got %d", len(active))
	}

	// Trigger eviction
	router.GetClient(ctx3)
	
	activeAfter := router.ActiveTenants()
	if len(activeAfter) != 2 {
		t.Errorf("expected 2 active tenants after eviction, got %d", len(activeAfter))
	}
}
