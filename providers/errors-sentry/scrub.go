// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sentry

import (
	"net/url"
	"sort"
	"strings"

	sentrygo "github.com/getsentry/sentry-go"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// The redaction an event goes through is the one a log line goes through:
// the framework's list of keys (pkg/observe, ADR-007), matched on the whole
// name in any case, the value replaced with the same placeholder. Exact
// matching is the framework's choice — `page_token` is not `token` — and an
// event that redacted differently from the log line next to it would be a
// second policy nobody wrote down.

func (rep *reporter) redacted(key string) bool {
	_, ok := rep.redact[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// redactMap replaces the value of every key on the list.
func (rep *reporter) redactMap(m map[string]string) map[string]string {
	for k := range m {
		if rep.redacted(k) {
			m[k] = observe.RedactionPlaceholder
		}
	}
	return m
}

// redactQuery replaces the value of every query parameter on the list. The
// result is encoded with sorted keys, so the same query reads the same in
// every event.
func (rep *reporter) redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		// A query that does not parse cannot be redacted key by key, and
		// sending it as it came would send whatever it holds.
		return observe.RedactionPlaceholder
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		for _, v := range values[k] {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			if rep.redacted(k) {
				v = observe.RedactionPlaceholder
			}
			b.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(v))
		}
	}
	return b.String()
}

// scrub runs on every event before it is sent (BeforeSend): whatever built
// the event — this package, or an SDK integration — a header, a query
// parameter, a tag or a context field named on the list leaves as the placeholder,
// and cookies, bodies and the client's address do not leave at all.
func (rep *reporter) scrub(event *sentrygo.Event) {
	if event == nil {
		return
	}
	if req := event.Request; req != nil {
		req.Cookies = ""
		req.Data = ""
		req.Env = nil
		req.Headers = rep.redactMap(req.Headers)
		req.QueryString = rep.redactQuery(req.QueryString)
	}
	event.Tags = rep.redactMap(event.Tags)
	for _, c := range event.Contexts {
		for k := range c {
			if rep.redacted(k) {
				c[k] = observe.RedactionPlaceholder
			}
		}
	}
	event.User.IPAddress = ""
}
