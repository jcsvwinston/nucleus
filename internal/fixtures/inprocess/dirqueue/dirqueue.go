// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package dirqueue is the example in-process extension: a module that adds
// an outbox bridge written in Go, registered on the running application
// with no process in between. It delivers what the example plugin
// nucleus-plugin-relay delivers for queue.publish — each outbox message as
// one JSON file under <dir>/<topic>/new/, written to tmp/ and renamed,
// named by the message id so a redelivery replaces its own file — so the
// two can be read side by side: the same delivery out of process, through
// the plugin contract, and in process, through the outbox's Bridge
// interface.
//
// In process means one Go type, typed configuration and no process per
// message; it also means the extension is compiled into the application and
// shares its fate. An external plugin is the other trade.
//
// Mount it, and turn the outbox on:
//
//	nucleus.New().
//		FromConfigFile("nucleus.yml").
//		Mount(dirqueue.Module(dirqueue.Config{})).
//		Start()
//
//	# nucleus.yml
//	outbox:
//	  enabled: true
//	modules:
//	  dirqueue:
//	    dir: /var/spool/app-events
//	    pattern: "orders.*"
//
// Its test mounts it on an application and reaches it through the outbox
// the application runs, and holds it to nucleustest.CheckModuleIn.
package dirqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// Config is the module's configuration, bound from modules.dirqueue.* (the
// values a Config passed to Module sets are the baseline the file
// overrides).
type Config struct {
	// Dir is the directory queue the messages are written into.
	Dir string `koanf:"dir" validate:"required"`
	// Pattern is the outbox topic pattern routed to the bridge
	// ("orders.*", or "*" for every topic).
	Pattern string `koanf:"pattern" default:"*"`
}

// BridgeName is the name the module registers its bridge under, the one
// its route points at.
const BridgeName = "dirqueue"

// Module returns the module. It registers the bridge in OnStart, when the
// application's outbox exists and before its dispatcher starts, so the
// first pass already routes to it.
func Module(cfg Config) nucleus.ModuleSpec {
	return nucleus.Module[Config]{
		Name:   "dirqueue",
		Config: cfg,
		OnStart: func(_ context.Context, rt nucleus.Runtime, cfg Config) error {
			box := rt.Outbox()
			if box == nil {
				return errors.New("dirqueue: the application has no outbox — set outbox.enabled: true")
			}
			if err := box.RegisterBridge(&Bridge{Dir: cfg.Dir}); err != nil {
				return fmt.Errorf("dirqueue: %w", err)
			}
			box.AddRoute(cfg.Pattern, BridgeName)
			rt.Logger().Info("dirqueue: outbox bridge registered", "dir", cfg.Dir, "pattern", cfg.Pattern)
			return nil
		},
	}.Build()
}

// Bridge is the outbox.Bridge: the whole in-process extension contract is
// these four methods.
type Bridge struct {
	Dir string
}

// Name implements outbox.Bridge.
func (b *Bridge) Name() string { return BridgeName }

// Message is the file a delivered message becomes.
type Message struct {
	ID        string          `json:"id"`
	Topic     string          `json:"topic"`
	Payload   json.RawMessage `json:"payload"`
	Attempts  int             `json:"attempts"`
	CreatedAt time.Time       `json:"created_at"`
}

// Send implements outbox.Bridge. A message whose topic cannot name a
// directory is refused with outbox.Permanent — no attempt will change it,
// so it goes to the dead letter at once; a directory that cannot be
// written is an ordinary error, which the outbox retries with backoff.
func (b *Bridge) Send(_ context.Context, msg outbox.Message) error {
	if !safeSegment(msg.Topic) || !safeSegment(msg.ID) {
		return outbox.Permanent(fmt.Errorf("dirqueue: topic %q or id %q cannot name a file", msg.Topic, msg.ID))
	}
	payload := json.RawMessage(msg.Payload)
	if !json.Valid(payload) {
		return outbox.Permanent(fmt.Errorf("dirqueue: message %s: the payload is not JSON", msg.ID))
	}
	dir := filepath.Join(b.Dir, msg.Topic)
	for _, sub := range []string{"tmp", "new"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return fmt.Errorf("dirqueue: %w", err)
		}
	}
	content, err := json.Marshal(Message{ID: msg.ID, Topic: msg.Topic, Payload: payload, Attempts: msg.Attempts, CreatedAt: msg.CreatedAt})
	if err != nil {
		return outbox.Permanent(fmt.Errorf("dirqueue: message %s: %w", msg.ID, err))
	}
	tmp := filepath.Join(dir, "tmp", fmt.Sprintf("%s.%d", msg.ID, time.Now().UnixNano()))
	if err := os.WriteFile(tmp, append(content, '\n'), 0o600); err != nil {
		return fmt.Errorf("dirqueue: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "new", msg.ID+".json")); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("dirqueue: %w", err)
	}
	return nil
}

// Healthy implements outbox.Bridge: the directory is there or can be made.
func (b *Bridge) Healthy(context.Context) error {
	return os.MkdirAll(b.Dir, 0o700)
}

// Close implements outbox.Bridge; there is nothing to release.
func (b *Bridge) Close() error { return nil }

func safeSegment(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 200 {
		return false
	}
	for _, r := range s {
		ok := r == '.' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}
