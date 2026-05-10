package routing

import (
	"testing"
	"time"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func httpEvent(node, method, path string, status int) *adminv1.Event {
	return &adminv1.Event{
		NodeId: node,
		Body: &adminv1.Event_HttpRequest{
			HttpRequest: &adminv1.HttpRequestEvent{
				Method: method, Path: path, Status: uint32(status),
			},
		},
	}
}

func TestEventBus_PublishAndDeliver(t *testing.T) {
	b := NewEventBus()
	sub, cancel := b.Subscribe(nil, nil, 4)
	defer cancel()

	if got := b.SubscriberCount(); got != 1 {
		t.Errorf("count = %d", got)
	}

	delivered := b.Publish(httpEvent("n", "GET", "/", 200))
	if delivered != 1 {
		t.Errorf("delivered = %d, want 1", delivered)
	}

	select {
	case ev := <-sub.Ch():
		if ev.NodeId != "n" {
			t.Errorf("nodeId = %q", ev.NodeId)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBus_FilterKinds(t *testing.T) {
	b := NewEventBus()
	_, cancel := b.Subscribe(&adminv1.Filter{
		Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_SQL_STATEMENT},
	}, nil, 4)
	defer cancel()

	if got := b.Publish(httpEvent("n", "GET", "/", 200)); got != 0 {
		t.Errorf("HTTP delivered to SQL-only sub: %d", got)
	}

	sql := &adminv1.Event{
		NodeId: "n",
		Body: &adminv1.Event_SqlStatement{
			SqlStatement: &adminv1.SqlStatementEvent{ModelName: "User"},
		},
	}
	if got := b.Publish(sql); got != 1 {
		t.Errorf("SQL delivered = %d, want 1", got)
	}
}

func TestEventBus_FilterNodes(t *testing.T) {
	b := NewEventBus()
	sub, cancel := b.Subscribe(&adminv1.Filter{NodeIds: []string{"node-a"}}, nil, 4)
	defer cancel()

	if got := b.Publish(httpEvent("node-b", "GET", "/", 200)); got != 0 {
		t.Errorf("delivered cross-node: %d", got)
	}
	if got := b.Publish(httpEvent("node-a", "GET", "/", 200)); got != 1 {
		t.Errorf("delivered own-node = %d, want 1", got)
	}

	select {
	case ev := <-sub.Ch():
		if ev.NodeId != "node-a" {
			t.Fatalf("nodeId = %q", ev.NodeId)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBus_DropOnFullChannel(t *testing.T) {
	b := NewEventBus()
	_, cancel := b.Subscribe(nil, nil, 1)
	defer cancel()

	for i := 0; i < 5; i++ {
		b.Publish(httpEvent("n", "GET", "/", 200))
	}
	if got := b.Stats(); got.Dropped == 0 {
		t.Errorf("expected drops, got %+v", got)
	}
}

func TestEventBus_HasDemand(t *testing.T) {
	b := NewEventBus()

	if b.HasDemand(adminv1.EventType_EVENT_TYPE_HTTP_REQUEST) {
		t.Error("idle bus should report no demand")
	}

	_, cancel := b.Subscribe(&adminv1.Filter{
		Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST},
	}, nil, 4)
	defer cancel()

	if !b.HasDemand(adminv1.EventType_EVENT_TYPE_HTTP_REQUEST) {
		t.Error("HTTP demand expected")
	}
	if b.HasDemand(adminv1.EventType_EVENT_TYPE_SQL_STATEMENT) {
		t.Error("no SQL sub, demand should be false")
	}
}

func TestEventBus_AggregateFilter(t *testing.T) {
	b := NewEventBus()

	if got := b.AggregateFilter(); got != nil {
		t.Errorf("empty bus filter = %+v, want nil", got)
	}

	_, cancel1 := b.Subscribe(&adminv1.Filter{
		Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST},
	}, nil, 4)
	defer cancel1()
	_, cancel2 := b.Subscribe(&adminv1.Filter{
		Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_SQL_STATEMENT},
	}, nil, 4)
	defer cancel2()

	agg := b.AggregateFilter()
	if len(agg.Types) != 2 {
		t.Fatalf("aggregate types = %v", agg.Types)
	}

	// One open subscription forces the aggregate to be "all kinds".
	_, cancelOpen := b.Subscribe(nil, nil, 4)
	defer cancelOpen()
	agg = b.AggregateFilter()
	if len(agg.Types) != 0 {
		t.Errorf("with open sub, Types should be empty, got %v", agg.Types)
	}
}

func TestEventBus_HTTPMethodFilter(t *testing.T) {
	b := NewEventBus()
	sub, cancel := b.Subscribe(&adminv1.Filter{
		HttpMethods: []string{"POST"},
	}, nil, 4)
	defer cancel()

	if got := b.Publish(httpEvent("n", "GET", "/", 200)); got != 0 {
		t.Error("GET should be filtered out")
	}
	if got := b.Publish(httpEvent("n", "POST", "/", 200)); got != 1 {
		t.Error("POST should pass")
	}

	select {
	case ev := <-sub.Ch():
		if ev.GetHttpRequest().Method != "POST" {
			t.Errorf("method = %q", ev.GetHttpRequest().Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
