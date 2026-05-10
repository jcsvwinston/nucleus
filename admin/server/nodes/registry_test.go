package nodes

import (
	"context"
	"testing"
	"time"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func TestRegistry_AddListLookup(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entry, deregister := r.Add(ctx, NodeInfo{
		NodeID: "node-a", Version: "v1",
	}, 8)
	defer deregister()

	if entry.NodeID != "node-a" {
		t.Errorf("NodeID = %q", entry.NodeID)
	}
	if l := r.List(); len(l) != 1 || l[0].NodeID != "node-a" {
		t.Errorf("List = %+v", l)
	}
	if _, ok := r.Lookup("node-a"); !ok {
		t.Error("Lookup failed")
	}
	if _, ok := r.Lookup("ghost"); ok {
		t.Error("Lookup ghost should fail")
	}
}

func TestRegistry_RemoveOnDeregister(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, deregister := r.Add(ctx, NodeInfo{NodeID: "node-a"}, 8)
	deregister()
	deregister() // idempotent

	if _, ok := r.Lookup("node-a"); ok {
		t.Error("Lookup after deregister should fail")
	}
}

func TestRegistry_TryEnqueue_NonBlocking(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entry, deregister := r.Add(ctx, NodeInfo{NodeID: "node-a"}, 1)
	defer deregister()

	frame := &adminv1.Frame{Body: &adminv1.Frame_Heartbeat{Heartbeat: &adminv1.Heartbeat{}}}
	if !TryEnqueue(entry, frame) {
		t.Error("first enqueue should succeed")
	}
	// Buffer is full; next enqueue must NOT block and must return false.
	if TryEnqueue(entry, frame) {
		t.Error("expected drop on full buffer")
	}
}

func TestRegistry_Touch(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, deregister := r.Add(ctx, NodeInfo{NodeID: "node-a", LastSeenAt: time.Now().UTC().Add(-time.Hour)}, 8)
	defer deregister()

	r.Touch("node-a", time.Now().UTC())
	e, _ := r.Lookup("node-a")
	if time.Since(e.Info.LastSeenAt) > time.Minute {
		t.Errorf("LastSeenAt not updated: %v", e.Info.LastSeenAt)
	}
}

func TestRegistry_Watch(t *testing.T) {
	r := New()
	ch, cancel := r.Watch()
	defer cancel()

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()
	_, dereg := r.Add(ctx, NodeInfo{NodeID: "node-a"}, 8)

	select {
	case change := <-ch:
		if !change.Connected || change.NodeID != "node-a" {
			t.Errorf("change = %+v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive connect notification")
	}

	dereg()

	select {
	case change := <-ch:
		if change.Connected || change.NodeID != "node-a" {
			t.Errorf("disconnect change = %+v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive disconnect notification")
	}
}
