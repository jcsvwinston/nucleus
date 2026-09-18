// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"errors"
	"testing"
)

// NU-76: a screen that shows one kind of message has to be able to count that
// kind. Showing the whole outbox and naming its scope is what the panel had to
// do instead — an application that also queues webhooks read "4 pending" on its
// mail screen as four unsent mails.
func TestInspectRuntime_CountsPerTopic(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "per_topic_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{"mail", "mail", "mail", "webhooks"} {
		if _, err := store.Enqueue(context.Background(), Entry{Topic: topic, Payload: map[string]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	snap := InspectRuntime(db, cfg)
	if snap.Pending != 4 {
		t.Fatalf("the whole outbox has %d pending, want 4", snap.Pending)
	}
	mail, ok := snap.TopicSnapshotFor("mail")
	if !ok {
		t.Fatal("no snapshot for the mail topic")
	}
	if mail.Pending != 3 || mail.Total != 3 {
		t.Errorf("mail: pending=%d total=%d, want 3 and 3", mail.Pending, mail.Total)
	}
	if hooks, _ := snap.TopicSnapshotFor("webhooks"); hooks.Pending != 1 {
		t.Errorf("webhooks: pending=%d, want 1", hooks.Pending)
	}
	if _, ok := snap.TopicSnapshotFor("nothing-here"); ok {
		t.Error("a topic the outbox has never seen reported a snapshot")
	}
}

// The other half: a topic that is struggling shows WHY, without having to
// reach the dead letter first.
func TestInspectRuntime_CarriesTheLastErrorPerTopic(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "last_error_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(context.Background(), Entry{Topic: "webhooks", Payload: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(context.Background(), Entry{Topic: "mail", Payload: map[string]string{}}); err != nil {
		t.Fatal(err)
	}

	dcfg := DefaultDispatcherConfig()
	dcfg.LeaseOwner = "test"
	d, err := NewDispatcher(store, func(_ context.Context, msg Message) error {
		if msg.Topic == "webhooks" {
			return errors.New("the endpoint refused it")
		}
		return nil
	}, dcfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	snap := InspectRuntime(db, cfg)
	hooks, ok := snap.TopicSnapshotFor("webhooks")
	if !ok {
		t.Fatal("no snapshot for webhooks")
	}
	if hooks.LastError == "" {
		t.Fatal("the failing topic carries no reason: a count without a cause is what an operator cannot act on")
	}
	if hooks.LastErrorAt == "" {
		t.Error("the failure has no timestamp")
	}
	// The healthy topic stays clean: the error belongs to the topic that had
	// it, not to the outbox as a whole.
	if mail, _ := snap.TopicSnapshotFor("mail"); mail.LastError != "" {
		t.Errorf("the delivered topic carries an error it never had: %q", mail.LastError)
	}
}
