// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"bytes"
	"encoding"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"reflect"

	"github.com/jcsvwinston/nucleus/internal/httpneg"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// negotiableTypes are the representations Negotiate can produce, in the
// order it prefers them when the client accepts several equally.
var negotiableTypes = []string{"application/json", "application/xml", "text/xml", "text/plain"}

// Negotiate answers v with status in the representation the client asked
// for in its Accept header — JSON, XML or plain text — so one handler
// serves every client instead of one handler per representation.
//
// The client's quality values decide (RFC 9110 §12.5.1: the most specific
// matching range gives a type its quality); between types it accepts
// equally the order is JSON, XML, plain text, so a client that sends */*
// or no Accept at all gets JSON. Plain text is offered only for a value
// that has one — a string, a []byte, a number or a bool, an error, a
// fmt.Stringer or an encoding.TextMarshaler — and XML only for a value
// encoding/xml can encode (a map cannot be); when the client's first choice
// cannot encode v, the next type it accepts answers. The response carries
// Vary: Accept, so a cache keeps the representations apart.
//
// When the client accepts none of them, nothing is written and Negotiate
// returns a 406 NOT_ACCEPTABLE DomainError naming the types it could have
// had; a handler returns it, and the router answers it in the framework's
// error shape like any other error.
func Negotiate(w http.ResponseWriter, r *http.Request, status int, v any) error {
	if w == nil {
		return ErrNilContextWriter
	}
	accept := httpneg.AcceptHeader(r)
	var producible []string
	for _, ct := range negotiableTypes {
		if ct == "text/plain" && !hasTextForm(v) {
			continue
		}
		producible = append(producible, ct)
	}
	for _, ct := range httpneg.Ranked(accept, producible...) {
		body, ok := encodeAs(ct, v)
		if !ok {
			continue
		}
		w.Header().Add("Vary", "Accept")
		w.Header().Set("Content-Type", ct+"; charset=utf-8")
		w.WriteHeader(status)
		_, err := w.Write(body)
		return err
	}
	var available []string
	for _, ct := range producible {
		if _, ok := encodeAs(ct, v); ok {
			available = append(available, ct)
		}
	}
	return (&gferrors.DomainError{
		Code:       "NOT_ACCEPTABLE",
		Message:    "none of the representations this resource has is acceptable",
		StatusCode: http.StatusNotAcceptable,
	}).WithDetails(map[string]any{"available": available})
}

// encodeAs renders v as ct; false when v has no such representation.
func encodeAs(ct string, v any) ([]byte, bool) {
	switch ct {
	case "application/json":
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(v); err != nil {
			return nil, false
		}
		return buf.Bytes(), true
	case "application/xml", "text/xml":
		if v == nil {
			return nil, false
		}
		b, err := xml.Marshal(v)
		if err != nil {
			return nil, false
		}
		return b, true
	case "text/plain":
		s, ok := textForm(v)
		return []byte(s), ok
	}
	return nil, false
}

func hasTextForm(v any) bool {
	_, ok := textForm(v)
	return ok
}

// textForm is v's plain-text representation, if it has one.
func textForm(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case []byte:
		return string(x), true
	case error:
		return x.Error(), true
	case encoding.TextMarshaler:
		b, err := x.MarshalText()
		if err != nil {
			return "", false
		}
		return string(b), true
	case fmt.Stringer:
		return x.String(), true
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String:
		return fmt.Sprint(v), true
	}
	return "", false
}
