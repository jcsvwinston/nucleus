package quark_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/quark"
	"github.com/jcsvwinston/GoFrame/pkg/quark/cache/memory"
	_ "github.com/mattn/go-sqlite3"
)

type CacheUser struct {
	ID    int64  `db:"id" pk:"true"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

type sqlCounter struct {
	count int
}

func (s *sqlCounter) ObserveQuery(event quark.QueryEvent) {
	if event.Operation == "SELECT" || event.Operation == "QUERY_ROW" {
		s.count++
	}
}

func TestCacheVerification(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", "file:cachetest?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	counter := &sqlCounter{}
	client, err := quark.New(db,
		quark.WithDialect(quark.SQLite()),
		quark.WithCacheStore(memory.New()),
		quark.WithQueryObserver(counter),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Setup table
	err = client.Sync(ctx, quark.SyncOptions{}, &CacheUser{})
	if err != nil {
		t.Fatal(err)
	}

	// Insert a record
	user := CacheUser{ID: 1, Name: "Quark User", Email: "quark@example.com"}
	err = quark.For[CacheUser](ctx, client).Create(&user)
	if err != nil {
		t.Fatal(err)
	}

	// Reset counter after setup
	counter.count = 0

	// 1st Read: Should hit DB (count = 1)
	_, err = quark.For[CacheUser](ctx, client).
		Where("id", "=", 1).
		Cache(1 * time.Minute).
		List()
	if err != nil {
		t.Fatal(err)
	}

	if counter.count != 1 {
		t.Errorf("Expected 1 query on first read, got %d", counter.count)
	}

	// 2nd Read: Should hit CACHE (count should still be 1)
	_, err = quark.For[CacheUser](ctx, client).
		Where("id", "=", 1).
		Cache(1 * time.Minute).
		List()
	if err != nil {
		t.Fatal(err)
	}

	if counter.count != 1 {
		t.Errorf("Expected cache hit (no new SQL), but query count increased to %d", counter.count)
	}

	// 3. Invalidation Test: Update the record
	user.Name = "Updated Name"
	_, err = quark.For[CacheUser](ctx, client).Update(&user)
	if err != nil {
		t.Fatal(err)
	}

	// 4th Read: Should hit DB again because of invalidation (count = 2)
	_, err = quark.For[CacheUser](ctx, client).
		Where("id", "=", 1).
		Cache(1 * time.Minute).
		List()
	if err != nil {
		t.Fatal(err)
	}

	if counter.count != 2 {
		t.Errorf("Expected cache invalidation after update, but query count is %d", counter.count)
	}
}
