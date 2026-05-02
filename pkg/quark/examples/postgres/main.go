package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/quark"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Product represents a multi-tenant product model using RLS
type Product struct {
	ID        int64     `db:"id" pk:"true"`
	TenantID  string    `db:"tenant_id"` // Used for RLS
	Name      string    `db:"name"`
	Price     float64   `db:"price"`
	CreatedAt time.Time `db:"created_at"`
}

func main() {
	ctx := context.Background()

	// 1. Initialize Postgres connection
	// Set QUARK_EXAMPLE_POSTGRES_DSN="postgres://user:pass@localhost:5432/db?sslmode=disable"
	dsn := os.Getenv("QUARK_EXAMPLE_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://quark_user:quark_pass@localhost:5432/quark_test?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 2. Initialize Quark with Multi-Tenant RLS
	client, err := quark.New(db, 
		quark.WithDialect(quark.PostgreSQL()),
		quark.WithTenantStrategy(quark.RowLevelSecurity, "tenant_id"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 3. Migrate
	fmt.Println("🚀 Migrating Postgres schema...")
	if err := client.Migrate(ctx, &Product{}); err != nil {
		log.Fatal(err)
	}

	// 4. Create products for different tenants
	fmt.Println("📝 Creating multi-tenant products...")
	
	// Create for Tenant A
	ctxA := context.WithValue(ctx, "tenant_id", "tenant-a")
	prodA := &Product{Name: "Laptop", Price: 1200.0}
	if err := quark.For[Product](ctxA, client).Create(prodA); err != nil {
		log.Fatal(err)
	}

	// Create for Tenant B
	ctxB := context.WithValue(ctx, "tenant_id", "tenant-b")
	prodB := &Product{Name: "Smartphone", Price: 800.0}
	if err := quark.For[Product](ctxB, client).Create(prodB); err != nil {
		log.Fatal(err)
	}

	// 5. Verify Isolation
	fmt.Println("🔍 Verifying Tenant Isolation...")
	
	itemsA, _ := quark.For[Product](ctxA, client).List()
	fmt.Printf("Tenant A sees %d products: %v\n", len(itemsA), itemsA[0].Name)

	itemsB, _ := quark.For[Product](ctxB, client).List()
	fmt.Printf("Tenant B sees %d products: %v\n", len(itemsB), itemsB[0].Name)
}
