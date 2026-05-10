package buffer

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func mkEvent(id string) *adminv1.Event {
	return &adminv1.Event{NodeId: id}
}

func TestBuffer_PushDrain_FIFO(t *testing.T) {
	b := New(5)
	for i := 0; i < 3; i++ {
		b.Push(mkEvent(string(rune('a' + i))))
	}
	if got := b.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}
	out := b.Drain(0)
	if len(out) != 3 {
		t.Fatalf("Drain returned %d entries, want 3", len(out))
	}
	if out[0].NodeId != "a" || out[2].NodeId != "c" {
		t.Errorf("FIFO order broken: %v", []string{out[0].NodeId, out[1].NodeId, out[2].NodeId})
	}
	if b.Len() != 0 {
		t.Errorf("Len after drain = %d", b.Len())
	}
}

func TestBuffer_DropOldest_Overflow(t *testing.T) {
	b := New(3)
	b.Push(mkEvent("a"))
	b.Push(mkEvent("b"))
	b.Push(mkEvent("c"))
	if got := b.Push(mkEvent("d")); !got {
		t.Error("expected eviction on overflow")
	}
	if got := b.Push(mkEvent("e")); !got {
		t.Error("expected eviction on overflow")
	}
	if b.Dropped() != 2 {
		t.Errorf("Dropped = %d, want 2", b.Dropped())
	}

	out := b.Drain(0)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	if out[0].NodeId != "c" || out[2].NodeId != "e" {
		t.Errorf("post-overflow order = [%s,%s,%s], want [c,d,e]", out[0].NodeId, out[1].NodeId, out[2].NodeId)
	}
}

func TestBuffer_DrainLimit(t *testing.T) {
	b := New(5)
	for _, c := range "abcde" {
		b.Push(mkEvent(string(c)))
	}
	out := b.Drain(2)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].NodeId != "a" || out[1].NodeId != "b" {
		t.Errorf("partial drain order: %v %v", out[0].NodeId, out[1].NodeId)
	}
	if b.Len() != 3 {
		t.Errorf("remaining = %d, want 3", b.Len())
	}

	rest := b.Drain(0)
	if len(rest) != 3 || rest[0].NodeId != "c" {
		t.Errorf("remaining drain mismatched: %v", rest)
	}
}

func TestBuffer_NilSafe(t *testing.T) {
	var b *Buffer
	b.Push(mkEvent("x")) // must not panic
	if got := b.Drain(0); got != nil {
		t.Errorf("nil drain = %v, want nil", got)
	}
	if b.Len() != 0 || b.Capacity() != 0 || b.Dropped() != 0 {
		t.Error("nil buffer accessors should be zero")
	}
}

func TestPerKind_Routing(t *testing.T) {
	pk := NewPerKind(map[observability.EventKind]int{
		observability.KindHTTPRequest:   2,
		observability.KindSQLStatement:  3,
		observability.KindSessionChange: 1,
		observability.KindCustom:        4,
	})

	pk.For(observability.KindHTTPRequest).Push(mkEvent("http-1"))
	pk.For(observability.KindSQLStatement).Push(mkEvent("sql-1"))
	pk.For(observability.KindSessionChange).Push(mkEvent("sess-1"))
	pk.For(observability.KindCustom).Push(mkEvent("cust-1"))

	snap := pk.LenSnapshot()
	if snap[observability.KindHTTPRequest] != 1 {
		t.Errorf("http len = %d", snap[observability.KindHTTPRequest])
	}
	if snap[observability.KindCustom] != 1 {
		t.Errorf("custom len = %d", snap[observability.KindCustom])
	}

	all := pk.DrainAll()
	if len(all) != 4 {
		t.Fatalf("DrainAll returned %d, want 4", len(all))
	}
}

func TestPerKind_For_UnknownKind(t *testing.T) {
	pk := NewPerKind(nil)
	if got := pk.For(observability.KindUnknown); got != nil {
		t.Error("KindUnknown should yield nil buffer")
	}
}
