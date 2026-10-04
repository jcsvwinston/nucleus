// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Change is one difference between two versions of a document that a
// client written against the old one would notice.
type Change struct {
	// Operation is "METHOD /path", or "" for a document-wide change.
	Operation string `json:"operation,omitempty"`
	// Where names the part of the operation: "request body /title",
	// "parameter query.page", "response 200 /author", "security".
	Where   string `json:"where,omitempty"`
	Message string `json:"message"`
}

func (c Change) String() string {
	var b strings.Builder
	if c.Operation != "" {
		b.WriteString(c.Operation)
	}
	if c.Where != "" {
		if b.Len() > 0 {
			b.WriteString(" — ")
		}
		b.WriteString(c.Where)
	}
	if b.Len() > 0 {
		b.WriteString(": ")
	}
	b.WriteString(c.Message)
	return b.String()
}

// BreakingChanges lists what in next breaks a client written against prev.
// The rules are the ones a generated client actually trips on, read from
// the side of whoever sends the request and reads the response:
//
//   - an operation that disappears;
//   - a request that must say more: a new required parameter or body
//     field, a parameter or body that becomes required, a narrower type, a
//     tighter bound, an enum value removed;
//   - a response that says less: a field removed or no longer required, a
//     changed type, a success status removed, an enum value added (a
//     client switching on it meets a case it does not know);
//   - an operation that was public and now asks for credentials.
//
// Additions a client can ignore — a new operation, an optional parameter,
// a new response field, a wider request bound — are not reported.
func BreakingChanges(prev, next *Document) []Change {
	if prev == nil {
		return nil
	}
	if next == nil {
		next = NewDocument("", "")
	}
	var out []Change
	paths := make([]string, 0, len(prev.Paths))
	for p := range prev.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			oldOp := prev.Paths[p].Operation(m)
			if oldOp == nil {
				continue
			}
			name := m + " " + p
			newItem, ok := next.Paths[p]
			newOp := newItem.Operation(m)
			if !ok || newOp == nil {
				out = append(out, Change{Operation: name, Message: "the operation was removed"})
				continue
			}
			d := differ{prev: prev, next: next, op: name}
			d.operation(oldOp, newOp)
			out = append(out, d.changes...)
		}
	}
	return out
}

type differ struct {
	prev, next *Document
	op         string
	changes    []Change
	seen       map[string]bool
}

func (d *differ) add(where, format string, args ...any) {
	d.changes = append(d.changes, Change{Operation: d.op, Where: where, Message: fmt.Sprintf(format, args...)})
}

func (d *differ) operation(o, n *Operation) {
	// Security: public before, credentials now.
	if o.Security != nil && len(*o.Security) == 0 && (n.Security == nil || len(*n.Security) > 0) {
		if n.Security != nil || len(d.next.Security) > 0 {
			d.add("security", "the operation was public and now requires credentials")
		}
	}

	// Parameters, keyed by location and name.
	oldParams := map[string]Parameter{}
	for _, p := range o.Parameters {
		oldParams[p.In+"."+p.Name] = p
	}
	for _, np := range n.Parameters {
		key := np.In + "." + np.Name
		op, existed := oldParams[key]
		where := "parameter " + key
		switch {
		case !existed && np.Required:
			d.add(where, "a new required parameter")
		case existed && np.Required && !op.Required:
			d.add(where, "the parameter became required")
		}
		if existed {
			d.schema(where, op.Schema, np.Schema, true)
		}
	}

	// Request body.
	switch {
	case o.RequestBody == nil && n.RequestBody != nil && n.RequestBody.Required:
		d.add("request body", "a required body was added")
	case o.RequestBody != nil && n.RequestBody != nil:
		if n.RequestBody.Required && !o.RequestBody.Required {
			d.add("request body", "the body became required")
		}
		for mt, om := range o.RequestBody.Content {
			nm, ok := n.RequestBody.Content[mt]
			if !ok {
				d.add("request body", "the operation no longer accepts %s", mt)
				continue
			}
			d.schema("request body", om.Schema, nm.Schema, true)
		}
	}

	// Responses: what a client reads.
	for code, or := range o.Responses {
		nr, ok := n.Responses[code]
		if !ok {
			if strings.HasPrefix(code, "2") {
				d.add("response "+code, "the response was removed")
			}
			continue
		}
		for mt, om := range or.Content {
			nm, ok := nr.Content[mt]
			if !ok {
				d.add("response "+code, "no longer answers %s", mt)
				continue
			}
			d.schema("response "+code, om.Schema, nm.Schema, false)
		}
	}
}

// schema compares two schemas from the side the client is on: request
// (the client writes the value; tightening breaks it) or response (the
// client reads it; removing breaks it).
func (d *differ) schema(where string, o, n Schema, request bool) {
	// A named schema compared once per direction: a recursive type (a tree,
	// an article with its parent) would otherwise recurse forever.
	if o.Ref != "" && n.Ref != "" {
		key := fmt.Sprintf("%s|%s|%v", o.Ref, n.Ref, request)
		if d.seen == nil {
			d.seen = map[string]bool{}
		}
		if d.seen[key] {
			return
		}
		d.seen[key] = true
	}
	o, n = d.resolve(d.prev, o), d.resolve(d.next, n)

	if o.Type != "" && n.Type != "" && o.Type != n.Type {
		d.add(where, "the type changed from %s to %s", o.Type, n.Type)
		return
	}
	if request {
		if !o.Nullable && n.Nullable {
			// Accepting null as well is a widening for a writer.
		} else if o.Nullable && !n.Nullable {
			d.add(where, "null is no longer accepted")
		}
		d.tighter(where, o, n)
		if removed := enumRemoved(o.Enum, n.Enum); len(removed) > 0 {
			d.add(where, "the values %s are no longer accepted", enumList(removed))
		}
		oldReq := toSet(o.Required)
		for _, r := range n.Required {
			if !oldReq[r] {
				d.add(where+"/"+r, "a new required field")
			}
		}
	} else {
		if !o.Nullable && n.Nullable {
			d.add(where, "the value may now be null")
		}
		if added := enumRemoved(n.Enum, o.Enum); len(added) > 0 && len(o.Enum) > 0 {
			d.add(where, "new values %s may be returned", enumList(added))
		}
		newReq := toSet(n.Required)
		for _, r := range o.Required {
			if !newReq[r] {
				if _, still := n.Properties[r]; still {
					d.add(where+"/"+r, "the field is no longer always present")
				}
			}
		}
		for name := range o.Properties {
			if _, ok := n.Properties[name]; !ok {
				d.add(where+"/"+name, "the field was removed")
			}
		}
	}
	names := make([]string, 0, len(o.Properties))
	for name := range o.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		np, ok := n.Properties[name]
		if !ok {
			continue
		}
		d.schema(where+"/"+name, o.Properties[name], np, request)
	}
	if o.Items != nil && n.Items != nil {
		d.schema(where+"/items", *o.Items, *n.Items, request)
	}
	if o.AdditionalProperties != nil && n.AdditionalProperties != nil {
		d.schema(where+"/*", *o.AdditionalProperties, *n.AdditionalProperties, request)
	}
}

// tighter reports the bounds a request value used to meet and may not now.
func (d *differ) tighter(where string, o, n Schema) {
	gtF := func(a, b *float64) bool { return b != nil && (a == nil || *b > *a) }
	ltF := func(a, b *float64) bool { return b != nil && (a == nil || *b < *a) }
	gtI := func(a, b *int) bool { return b != nil && (a == nil || *b > *a) }
	ltI := func(a, b *int) bool { return b != nil && (a == nil || *b < *a) }
	switch {
	case gtF(o.Minimum, n.Minimum), gtF(o.ExclusiveMinimum, n.ExclusiveMinimum):
		d.add(where, "the minimum was raised")
	}
	switch {
	case ltF(o.Maximum, n.Maximum), ltF(o.ExclusiveMaximum, n.ExclusiveMaximum):
		d.add(where, "the maximum was lowered")
	}
	if gtI(o.MinLength, n.MinLength) {
		d.add(where, "the minimum length was raised")
	}
	if ltI(o.MaxLength, n.MaxLength) {
		d.add(where, "the maximum length was lowered")
	}
	if gtI(o.MinItems, n.MinItems) {
		d.add(where, "the minimum number of items was raised")
	}
	if ltI(o.MaxItems, n.MaxItems) {
		d.add(where, "the maximum number of items was lowered")
	}
	if n.Pattern != "" && n.Pattern != o.Pattern {
		d.add(where, "a new pattern %s", n.Pattern)
	}
	if n.Format != "" && n.Format != o.Format && o.Type == "string" {
		d.add(where, "a new format %s", n.Format)
	}
	if len(o.Enum) == 0 && len(n.Enum) > 0 {
		d.add(where, "the value is now restricted to %s", enumList(n.Enum))
	}
}

func (d *differ) resolve(doc *Document, s Schema) Schema {
	for i := 0; s.Ref != "" && i < 32; i++ {
		name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/")
		if !ok {
			return s
		}
		target, ok := doc.Components.Schemas[name]
		if !ok {
			return Schema{}
		}
		s = target
	}
	return s
}

func enumRemoved(old, next []any) []any {
	if len(next) == 0 {
		return nil
	}
	var out []any
	for _, v := range old {
		if !enumContains(next, v) {
			out = append(out, v)
		}
	}
	return out
}

func toSet(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, s := range list {
		m[s] = true
	}
	return m
}
