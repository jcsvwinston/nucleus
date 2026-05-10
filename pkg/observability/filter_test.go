package observability

import (
	"testing"
	"time"
)

func TestFilter_Empty_AllowsAll(t *testing.T) {
	f := Filter{}
	cases := []Event{
		AcquireHTTPRequestEvent(time.Now(), "node-a"),
		AcquireSQLStatementEvent(time.Now(), "node-b"),
		AcquireSessionChangeEvent(time.Now(), "node-c"),
		AcquireCustomEvent(time.Now(), "node-d"),
	}
	defer func() {
		for _, e := range cases {
			e.Release()
		}
	}()

	for _, e := range cases {
		if !f.Matches(e) {
			t.Errorf("empty filter should match %s/%s, but did not", e.Kind(), e.NodeID())
		}
	}
}

func TestFilter_Kinds(t *testing.T) {
	f := Filter{Kinds: []EventKind{KindSQLStatement}}

	httpEv := AcquireHTTPRequestEvent(time.Now(), "n")
	defer httpEv.Release()
	sqlEv := AcquireSQLStatementEvent(time.Now(), "n")
	defer sqlEv.Release()

	if f.Matches(httpEv) {
		t.Error("filter Kinds=[sql] should not match http")
	}
	if !f.Matches(sqlEv) {
		t.Error("filter Kinds=[sql] should match sql")
	}
}

func TestFilter_Nodes(t *testing.T) {
	f := Filter{NodeIDs: []string{"node-a", "node-b"}}

	a := AcquireHTTPRequestEvent(time.Now(), "node-a")
	defer a.Release()
	b := AcquireHTTPRequestEvent(time.Now(), "node-B")
	defer b.Release()
	c := AcquireHTTPRequestEvent(time.Now(), "node-c")
	defer c.Release()

	if !f.Matches(a) {
		t.Error("expected node-a to match")
	}
	if !f.Matches(b) {
		t.Error("expected node-B to match (case-insensitive)")
	}
	if f.Matches(c) {
		t.Error("did not expect node-c to match")
	}
}

func TestFilter_NilEvent(t *testing.T) {
	f := Filter{}
	if f.Matches(nil) {
		t.Error("nil event must not match")
	}
}
