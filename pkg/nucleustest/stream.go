// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Stream is an open server-sent-events connection in a test.
//
// It exists because a live view was the one thing this kit could not drive: it
// could make a request and read a response, and a stream is neither — it stays
// open, arrives in pieces, and has to be closed. So every application testing
// a channel wrote the bufio loop, the event parser and the timeout again.
type Stream struct {
	tb     testing.TB
	resp   *http.Response
	reader *bufio.Reader
	cancel context.CancelFunc
}

// StreamEvent is one event off a stream.
type StreamEvent struct {
	// Event is the event name, empty for an unnamed one.
	Event string
	// Data is the payload, with the data: prefixes removed and multi-line
	// payloads rejoined.
	Data string
	// ID is the event id, when the server sent one.
	ID string
}

// JSON decodes the event's payload into v, failing the test when it will not.
func (e StreamEvent) JSON(tb testing.TB, v any) {
	tb.Helper()
	if err := json.Unmarshal([]byte(e.Data), v); err != nil {
		tb.Fatalf("nucleustest: event %q is not the JSON expected: %v (payload %q)", e.Event, err, e.Data)
	}
}

// Stream opens an SSE connection to path and returns it. The connection is
// closed when the test ends.
//
//	stream := srv.Stream("/live")
//	hub.Broadcast(ctx, realtime.Message{Topic: "orders", Event: "created", Data: body})
//	event := stream.Next(time.Second)
func (s *Server) Stream(path string, headers ...map[string]string) *Stream {
	s.tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL(path), nil)
	if err != nil {
		cancel()
		s.tb.Fatalf("nucleustest: build stream request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for _, set := range headers {
		for k, v := range set {
			req.Header.Set(k, v)
		}
	}
	resp, err := s.Client().Do(req)
	if err != nil {
		cancel()
		s.tb.Fatalf("nucleustest: open stream %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		s.tb.Fatalf("nucleustest: stream %s answered %d, want 200", path, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		_ = resp.Body.Close()
		cancel()
		s.tb.Fatalf("nucleustest: stream %s answered content-type %q, want text/event-stream", path, ct)
	}

	stream := &Stream{tb: s.tb, resp: resp, reader: bufio.NewReader(resp.Body), cancel: cancel}
	s.tb.Cleanup(stream.Close)
	return stream
}

// Next returns the next event, failing the test if none arrives within limit.
// Keep-alive comments are skipped: they are the transport's business.
func (s *Stream) Next(limit time.Duration) StreamEvent {
	s.tb.Helper()
	event, ok := s.next(limit)
	if !ok {
		s.tb.Fatalf("nucleustest: no event arrived within %v", limit)
	}
	return event
}

// Quiet fails the test if any event arrives within limit. It is how a test
// asserts that somebody did NOT receive a broadcast — the assertion an
// authorisation bug slips past when nobody writes it.
func (s *Stream) Quiet(limit time.Duration) {
	s.tb.Helper()
	if event, ok := s.next(limit); ok {
		s.tb.Fatalf("nucleustest: expected no events, got %q with %q", event.Event, event.Data)
	}
}

func (s *Stream) next(limit time.Duration) (StreamEvent, bool) {
	deadline := time.Now().Add(limit)
	var event StreamEvent
	var data []string
	for time.Now().Before(deadline) {
		line, err := s.readLineBefore(deadline)
		if err != nil {
			return StreamEvent{}, false
		}
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(trimmed, ":"):
			// Keep-alive comment.
			continue
		case trimmed == "":
			if len(data) > 0 || event.Event != "" {
				event.Data = strings.Join(data, "\n")
				return event, true
			}
			continue
		case strings.HasPrefix(trimmed, "event:"):
			event.Event = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		case strings.HasPrefix(trimmed, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
		case strings.HasPrefix(trimmed, "id:"):
			event.ID = strings.TrimSpace(strings.TrimPrefix(trimmed, "id:"))
		}
	}
	return StreamEvent{}, false
}

// readLineBefore reads one line, giving up at the deadline. The read itself is
// what blocks, so the deadline goes on the connection.
func (s *Stream) readLineBefore(deadline time.Time) (string, error) {
	type deadliner interface{ SetReadDeadline(time.Time) error }
	if conn, ok := s.resp.Body.(deadliner); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	done := make(chan struct{})
	var line string
	var err error
	go func() {
		defer close(done)
		line, err = s.reader.ReadString('\n')
	}()
	select {
	case <-done:
		return line, err
	case <-time.After(time.Until(deadline)):
		return "", fmt.Errorf("nucleustest: stream read timed out")
	}
}

// Close ends the stream.
func (s *Stream) Close() {
	if s == nil {
		return
	}
	s.cancel()
	_ = s.resp.Body.Close()
}
