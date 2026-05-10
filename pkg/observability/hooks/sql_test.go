package hooks

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// TestSQLObserver_NoSubscribers_NoEmit verifies the gate works.
func TestSQLObserver_NoSubscribers_NoEmit(t *testing.T) {
	bus := observability.NewBus(slog.New(slog.DiscardHandler))
	obs := NewSQLObserver(SQLObserverConfig{Bus: bus, NodeID: "n"})

	obs(context.Background(), model.SQLQueryEvent{
		ModelName: "Article",
		Operation: "select",
		Query:     "SELECT * FROM articles",
		Args:      []interface{}{1, "secret"},
		Duration:  time.Millisecond,
	})

	if got := bus.Stats(observability.KindSQLStatement); got.Emitted != 0 {
		t.Fatalf("emitted = %d, want 0", got.Emitted)
	}
}

// TestSQLObserver_Emits_AndSanitizes verifies the full sanitization pipeline.
func TestSQLObserver_Emits_AndSanitizes(t *testing.T) {
	bus := observability.NewBus(slog.New(slog.DiscardHandler))
	sub, cancel := bus.Subscribe(observability.Filter{Kinds: []observability.EventKind{observability.KindSQLStatement}}, nil)
	defer func() {
		cancel()
		for {
			select {
			case ev := <-sub.Ch():
				ev.Release()
			default:
				return
			}
		}
	}()

	obs := NewSQLObserver(SQLObserverConfig{Bus: bus, NodeID: "node-x"})

	obs(context.Background(), model.SQLQueryEvent{
		ModelName: "Article",
		Operation: "  insert  ",
		Query:     "INSERT INTO   articles   (id, title) VALUES (?, ?)",
		Args:      []interface{}{int64(7), "Hello world", []byte{0x01, 0x02}, nil, true},
		Duration:  3 * time.Millisecond,
		Error:     errors.New("constraint violation"),
	})

	select {
	case ev := <-sub.Ch():
		sql, ok := ev.(*observability.SQLStatementEvent)
		if !ok {
			t.Fatalf("got %T", ev)
		}
		defer sql.Release()

		if sql.ModelName != "Article" {
			t.Errorf("model = %q", sql.ModelName)
		}
		if sql.Operation != "insert" {
			t.Errorf("operation = %q", sql.Operation)
		}
		if sql.Query != "INSERT INTO articles (id, title) VALUES (?, ?)" {
			t.Errorf("query not compacted: %q", sql.Query)
		}
		if sql.Err != "constraint violation" {
			t.Errorf("err = %q", sql.Err)
		}
		if sql.NodeID() != "node-x" {
			t.Errorf("node = %q", sql.NodeID())
		}

		want := []string{"7", "string(11):***", "bytes(2):***", "null", "bool:true"}
		if len(sql.Args) != len(want) {
			t.Fatalf("args = %v, want %v", sql.Args, want)
		}
		for i, w := range want {
			if sql.Args[i] != w {
				t.Errorf("arg[%d] = %q, want %q", i, sql.Args[i], w)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// TestSQLObserver_NilBus_NoOp verifies the safety net.
func TestSQLObserver_NilBus_NoOp(t *testing.T) {
	obs := NewSQLObserver(SQLObserverConfig{Bus: nil})
	obs(context.Background(), model.SQLQueryEvent{}) // must not panic
}

// TestSQLObserver_TruncatesArgsList verifies the maxSQLArgs cap.
func TestSQLObserver_TruncatesArgsList(t *testing.T) {
	bus := observability.NewBus(slog.New(slog.DiscardHandler))
	sub, cancel := bus.Subscribe(observability.Filter{}, nil)
	defer func() {
		cancel()
		for {
			select {
			case ev := <-sub.Ch():
				ev.Release()
			default:
				return
			}
		}
	}()

	obs := NewSQLObserver(SQLObserverConfig{Bus: bus})

	args := make([]interface{}, 30)
	for i := range args {
		args[i] = i
	}
	obs(context.Background(), model.SQLQueryEvent{
		Query: "SELECT 1",
		Args:  args,
	})

	ev := <-sub.Ch()
	sql, _ := ev.(*observability.SQLStatementEvent)
	defer sql.Release()

	// 16 args + 1 trailing summary entry = 17.
	if len(sql.Args) != maxSQLArgs+1 {
		t.Fatalf("args len = %d, want %d", len(sql.Args), maxSQLArgs+1)
	}
	if sql.Args[len(sql.Args)-1] != "...(+14 more)" {
		t.Errorf("last arg = %q", sql.Args[len(sql.Args)-1])
	}
}
