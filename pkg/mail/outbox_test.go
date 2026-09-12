package mail

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/outbox"

	_ "modernc.org/sqlite"
)

type capturingSender struct {
	mu   sync.Mutex
	sent []Message
	err  error
}

func (s *capturingSender) Send(_ context.Context, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, msg)
	return nil
}

func (s *capturingSender) messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.sent...)
}

func mailTestStore(t *testing.T) (*outbox.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:mailoutbox_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := outbox.NewStore(db, outbox.Config{Flavor: outbox.FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, db
}

// The point of queueing mail: the message and the row it announces commit
// together. A rolled-back transaction leaves nothing to deliver.
func TestEnqueueTx_RollbackQueuesNothing(t *testing.T) {
	store, db := mailTestStore(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := EnqueueTx(ctx, store, tx, verificationMessage()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if snap := store.Snapshot(ctx); snap.Total != 0 {
		t.Fatalf("a rolled-back transaction left %d queued messages", snap.Total)
	}
}

func TestEnqueueTx_CommitQueuesTheMessage(t *testing.T) {
	store, db := mailTestStore(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := EnqueueTx(ctx, store, tx, verificationMessage()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if snap := store.Snapshot(ctx); snap.Pending != 1 {
		t.Fatalf("expected one pending message, got %#v", snap)
	}
}

// The bridge is what turns a queued payload back into a sent message, with
// both representations intact.
func TestOutboxBridge_DeliversBothRepresentations(t *testing.T) {
	sender := &capturingSender{}
	bridge := NewOutboxBridge(sender)

	store, _ := mailTestStore(t)
	queued, err := Enqueue(context.Background(), store, verificationMessage())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := bridge.Send(context.Background(), queued); err != nil {
		t.Fatalf("bridge send: %v", err)
	}

	sent := sender.messages()
	if len(sent) != 1 {
		t.Fatalf("expected one delivery, got %d", len(sent))
	}
	if sent[0].HTML == "" || sent[0].Body == "" || sent[0].Subject != "Confirm your address" {
		t.Fatalf("the message did not survive the queue: %+v", sent[0])
	}
	if len(sent[0].Attachments) != 1 || string(sent[0].Attachments[0].Content) != "hello" {
		t.Fatalf("the attachment did not survive the queue: %+v", sent[0].Attachments)
	}
}

// A payload that cannot be decoded is an error, not a silent drop: the
// outbox records it as failed with its reason.
func TestOutboxBridge_UndecodablePayloadFails(t *testing.T) {
	bridge := NewOutboxBridge(&capturingSender{})
	err := bridge.Send(context.Background(), outbox.Message{ID: "m1", Payload: []byte("{not json")})
	if err == nil || !strings.Contains(err.Error(), "m1") {
		t.Fatalf("expected a decode error naming the message, got %v", err)
	}
}

// An invalid message is refused at ENQUEUE time, where the caller can still
// see it, rather than at delivery time in a background dispatcher.
func TestEnqueue_RejectsInvalidMessage(t *testing.T) {
	store, _ := mailTestStore(t)
	if _, err := Enqueue(context.Background(), store, Message{From: "no-reply@example.test"}); err == nil {
		t.Fatal("a message with no recipient was queued")
	}
}

func verificationMessage() Message {
	return Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "Confirm your address",
		Body:    "Open https://app.example/verify?t=abc",
		HTML:    `<p>Open <a href="https://app.example/verify?t=abc">the link</a></p>`,
		Attachments: []Attachment{{
			Filename: "terms.txt", ContentType: "text/plain", Content: []byte("hello"),
		}},
	}
}
