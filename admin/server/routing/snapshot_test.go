package routing

import (
	"errors"
	"testing"
	"time"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func TestSnapshotRouter_BeginResolveWait(t *testing.T) {
	r := NewSnapshotRouter(0)

	id, ch, cancel, err := r.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		r.Resolve(&adminv1.SnapshotResponse{RequestId: id, PayloadJson: []byte("ok")})
	}()

	resp, err := Wait(ch, cancel, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if string(resp.PayloadJson) != "ok" {
		t.Errorf("payload = %q", string(resp.PayloadJson))
	}
}

func TestSnapshotRouter_Timeout(t *testing.T) {
	r := NewSnapshotRouter(0)
	_, ch, cancel, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Wait(ch, cancel, 50*time.Millisecond)
	if !errors.Is(err, ErrSnapshotTimeout) {
		t.Errorf("err = %v, want ErrSnapshotTimeout", err)
	}
	if r.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0 (cancel ran)", r.PendingCount())
	}
}

func TestSnapshotRouter_ResolveAfterCancel_NoOp(t *testing.T) {
	r := NewSnapshotRouter(0)
	id, _, cancel, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	if r.Resolve(&adminv1.SnapshotResponse{RequestId: id}) {
		t.Error("Resolve should report false after Cancel")
	}
}

func TestSnapshotRouter_OverflowGate(t *testing.T) {
	r := NewSnapshotRouter(2)
	cancels := []func(){}
	for i := 0; i < 2; i++ {
		_, _, c, err := r.Begin()
		if err != nil {
			t.Fatal(err)
		}
		cancels = append(cancels, c)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	if _, _, _, err := r.Begin(); !errors.Is(err, ErrSnapshotInflightOverflow) {
		t.Errorf("err = %v, want overflow", err)
	}
}

func TestSnapshotRouter_RequestIDsUnique(t *testing.T) {
	r := NewSnapshotRouter(0)
	seen := map[string]bool{}
	cancels := []func(){}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	for i := 0; i < 50; i++ {
		id, _, c, err := r.Begin()
		if err != nil {
			t.Fatal(err)
		}
		cancels = append(cancels, c)
		if seen[id] {
			t.Fatalf("duplicate id at iter %d: %q", i, id)
		}
		seen[id] = true
	}
}
