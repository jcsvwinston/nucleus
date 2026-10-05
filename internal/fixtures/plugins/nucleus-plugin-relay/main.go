// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Command nucleus-plugin-relay is the example plugin for the outbox's plugin
// bridge: it serves the two capabilities an outbox bridge of type plugin
// sends.
//
//   - queue.publish: each message is written into a directory queue — one
//     JSON file per message under <queue>/<topic>/new/, written to tmp/ and
//     renamed, the way a Maildir delivers — named by the message key. The
//     outbox's key is the message id, the same on every redelivery, so a
//     redelivered message replaces its own file instead of adding a second.
//   - webhook.deliver: the request the outbox hands over (method, URL,
//     headers, body, all of it built and signed by the outbox) is sent over
//     HTTP. A 2xx is a delivery; 408, 425, 429 and 5xx are transient (exit
//     20), which the outbox retries with backoff; any other answer is a
//     rejection (exit 30), which sends the message to the dead letter at
//     once.
//
// It is built and run by its test through the real runtime — an
// application whose outbox routes messages to it — and by `nucleus plugin
// test --execute`, so it is the starting point to copy for a bridge to a
// broker of your own: replace publish with the broker's client.
//
// Install and select it:
//
//	go build -o "$(go env GOPATH)/bin/nucleus-plugin-relay" ./internal/fixtures/plugins/nucleus-plugin-relay
//	# nucleus.yml
//	outbox:
//	  enabled: true
//	  bridges:
//	    - name: events
//	      type: plugin
//	      config:
//	        provider: relay
//	        capability: queue.publish
//	        pattern: "orders.*"
//	plugins:
//	  allowed:
//	    - provider: relay
//	      capabilities: [queue.publish, webhook.deliver]
//
// Configuration, read from the environment as a plugin reads its
// credentials: RELAY_QUEUE_DIR is the directory queue, $HOME/relay-queue
// when it is unset; it is created when missing.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

func main() {
	plugins.Serve(plugins.Plugin{
		QueuePublish:   publish,
		WebhookDeliver: deliver,
	})
}

// queued is the file a published message becomes.
type queued struct {
	Topic       string            `json:"topic"`
	Key         string            `json:"key"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        json.RawMessage   `json:"body"`
	RequestID   string            `json:"request_id"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	PublishedAt time.Time         `json:"published_at"`
}

// publish writes one message into the directory queue. A topic or key that
// cannot name a file is a validation failure (exit 10); a queue that cannot
// be written is transient (exit 20).
func publish(_ context.Context, req plugins.RequestEnvelope, m plugins.QueuePublishPayload) (plugins.QueuePublishOutput, error) {
	topic, err := fileName("topic", m.Topic)
	if err != nil {
		return plugins.QueuePublishOutput{}, err
	}
	keySource := m.Key
	if keySource == "" {
		keySource = req.RequestID
	}
	key, err := fileName("key", keySource)
	if err != nil {
		return plugins.QueuePublishOutput{}, err
	}
	root, err := queueDir()
	if err != nil {
		return plugins.QueuePublishOutput{}, err
	}
	dir := filepath.Join(root, topic)
	for _, sub := range []string{"tmp", "new"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return plugins.QueuePublishOutput{}, plugins.Fail(plugins.ExitCodeTransient, "QUEUE_UNWRITABLE", "create %s: %v", filepath.Join(dir, sub), err)
		}
	}
	content, err := json.MarshalIndent(queued{
		Topic: m.Topic, Key: keySource, Headers: m.Headers, Body: m.Body,
		RequestID: req.RequestID, Metadata: req.Metadata, PublishedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return plugins.QueuePublishOutput{}, plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "the body is not JSON: %v", err)
	}
	// The temporary name is unique to this delivery; the final one is the
	// key, so a redelivery replaces the file it delivered before.
	tmp := filepath.Join(dir, "tmp", fmt.Sprintf("%s.%d.%d", key, os.Getpid(), time.Now().UnixNano()))
	if err := writeSynced(tmp, append(content, '\n')); err != nil {
		_ = os.Remove(tmp)
		return plugins.QueuePublishOutput{}, plugins.Fail(plugins.ExitCodeTransient, "QUEUE_UNWRITABLE", "write %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "new", key+".json")); err != nil {
		_ = os.Remove(tmp)
		return plugins.QueuePublishOutput{}, plugins.Fail(plugins.ExitCodeTransient, "QUEUE_UNWRITABLE", "publish into %s: %v", filepath.Join(dir, "new"), err)
	}
	return plugins.QueuePublishOutput{MessageID: keySource}, nil
}

// deliver sends the webhook request the outbox built.
func deliver(ctx context.Context, _ plugins.RequestEnvelope, w plugins.WebhookDeliverPayload) (plugins.WebhookDeliverOutput, error) {
	target, err := url.Parse(w.URL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return plugins.WebhookDeliverOutput{}, plugins.Fail(plugins.ExitCodeValidation, "INVALID_URL", "%q is not an http(s) URL", w.URL)
	}
	method := strings.ToUpper(strings.TrimSpace(w.Method))
	if method == "" {
		method = http.MethodPost
	}
	if w.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewBufferString(w.Body))
	if err != nil {
		return plugins.WebhookDeliverOutput{}, plugins.Fail(plugins.ExitCodeValidation, "INVALID_REQUEST", "%v", err)
	}
	for k, v := range w.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return plugins.WebhookDeliverOutput{}, plugins.Fail(plugins.ExitCodeTimeout, "DEADLINE_EXCEEDED", "%s %s: %v", method, target.Redacted(), err)
		}
		return plugins.WebhookDeliverOutput{}, plugins.Fail(plugins.ExitCodeTransient, "UNREACHABLE", "%s %s: %v", method, target.Redacted(), err)
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		return plugins.WebhookDeliverOutput{StatusCode: code}, nil
	case code == http.StatusRequestTimeout, code == http.StatusTooEarly, code == http.StatusTooManyRequests, code >= 500:
		return plugins.WebhookDeliverOutput{}, plugins.Fail(plugins.ExitCodeTransient, fmt.Sprintf("HTTP_%d", code), "%s answered %d: %s", target.Redacted(), code, strings.TrimSpace(string(snippet)))
	default:
		return plugins.WebhookDeliverOutput{}, plugins.Fail(plugins.ExitCodeRejected, fmt.Sprintf("HTTP_%d", code), "%s answered %d: %s", target.Redacted(), code, strings.TrimSpace(string(snippet)))
	}
}

// fileName turns a topic or a key into one path segment, refusing what
// cannot become one rather than rewriting it into a name another message
// might also get.
func fileName(what, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "the message has no %s", what)
	}
	if value == "." || value == ".." || len(value) > 200 {
		return "", plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "%s %q cannot name a file", what, value)
	}
	for _, r := range value {
		ok := r == '.' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return "", plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "%s %q has %q, which cannot be part of a file name (letters, digits, '.', '-' and '_')", what, value, r)
		}
	}
	return value, nil
}

func queueDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("RELAY_QUEUE_DIR")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", plugins.Fail(plugins.ExitCodeValidation, "QUEUE_UNSET", "RELAY_QUEUE_DIR is not set and there is no home directory to default to: %v", err)
	}
	return filepath.Join(home, "relay-queue"), nil
}

// writeSynced writes and syncs the file before it is renamed into new/, so
// a consumer never reads a partial message.
func writeSynced(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
