// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package routedump is the wire format between a Nucleus application that
// prints its route table at boot (NUCLEUS_PRINT_ROUTES=1, read by
// pkg/nucleus.RunContext) and the `nucleus routes` command that runs the
// application to read it. Both sides import this package so the document
// shape, the environment variable and the line marker cannot drift apart.
//
// The document travels on the application's stdout, where the structured
// logger also writes, so it is emitted as ONE line carrying a schema marker
// that no log line can start with; Parse scans stdout for that line and
// ignores everything else.
package routedump

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// EnvVar is the environment variable the application reads at boot. Any
// value other than empty, "0" or "false" turns the dump on.
const EnvVar = "NUCLEUS_PRINT_ROUTES"

// Schema identifies the document version; it doubles as the line marker
// Parse looks for.
const Schema = "nucleus.routes/v1"

// Enabled reports whether an environment value asks for the dump.
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// Route is one entry of the application's route table.
type Route struct {
	// Method is the HTTP method, or "*" when the entry answers every method
	// (a mounted subtree or a method-less Handle).
	Method string `json:"method"`
	// Pattern is the full request path pattern, module prefix applied.
	Pattern string `json:"pattern"`
	// Module names the module that registered the route; empty for the
	// framework's own routes.
	Module string `json:"module,omitempty"`
	// Middlewares counts the middleware wrapped around the entry at
	// registration time.
	Middlewares int `json:"middlewares"`
}

// Document is what the application prints.
type Document struct {
	Schema string  `json:"schema"`
	Env    string  `json:"env"`
	Routes []Route `json:"routes"`
}

// Encode writes doc as a single line ending in "\n". The schema field is
// forced to Schema and placed first, so the line starts with the marker
// Parse searches for regardless of what the caller filled in.
func Encode(w io.Writer, doc Document) error {
	doc.Schema = Schema
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("routedump: encode: %w", err)
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("routedump: write: %w", err)
	}
	return nil
}

// linePrefix is what the Encode line starts with: encoding/json keeps struct
// field order, so the schema is always the first key.
var linePrefix = []byte(`{"schema":"` + Schema + `"`)

// Parse finds the document line in an application's captured stdout. It
// returns found=false when no line carries the marker (the binary did not
// honour the variable — an older framework, or a main that never reaches
// nucleus.Run), and an error when the marker line does not decode.
func Parse(stdout []byte) (doc Document, found bool, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, linePrefix) {
			continue
		}
		if err := json.Unmarshal(line, &doc); err != nil {
			return Document{}, true, fmt.Errorf("routedump: decode: %w", err)
		}
		if doc.Schema != Schema {
			return Document{}, true, fmt.Errorf("routedump: unexpected schema %q", doc.Schema)
		}
		return doc, true, nil
	}
	if err := scanner.Err(); err != nil {
		return Document{}, false, fmt.Errorf("routedump: scan stdout: %w", err)
	}
	return Document{}, false, nil
}
