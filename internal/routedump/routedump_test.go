// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package routedump

import (
	"bytes"
	"strings"
	"testing"
)

func TestEnabled(t *testing.T) {
	for _, off := range []string{"", "0", "false", " FALSE ", "no", "off"} {
		if Enabled(off) {
			t.Errorf("Enabled(%q) = true, want false", off)
		}
	}
	for _, on := range []string{"1", "true", "yes", "anything"} {
		if !Enabled(on) {
			t.Errorf("Enabled(%q) = false, want true", on)
		}
	}
}

// The document rides on the same stdout as the application's structured
// logger, in text or JSON format: Parse must pick the marker line out of
// either and ignore the rest.
func TestParseFindsTheLineAmongLogs(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("time=2026-09-06T10:00:00Z level=INFO msg=\"RBAC enforcer initialized\"\n")
	buf.WriteString(`{"time":"2026-09-06T10:00:00Z","level":"WARN","msg":"jwt: no signing material configured"}` + "\n")
	if err := Encode(&buf, Document{Env: "development", Routes: []Route{
		{Method: "GET", Pattern: "/healthz"},
		{Method: "POST", Pattern: "/notes", Module: "notes", Middlewares: 2},
	}}); err != nil {
		t.Fatal(err)
	}
	buf.WriteString("time=2026-09-06T10:00:01Z level=INFO msg=\"shutdown\"\n")

	doc, found, err := Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("document line not found")
	}
	if doc.Env != "development" || len(doc.Routes) != 2 {
		t.Fatalf("unexpected document: %+v", doc)
	}
	if doc.Routes[1].Module != "notes" || doc.Routes[1].Middlewares != 2 {
		t.Fatalf("module route lost attribution: %+v", doc.Routes[1])
	}
}

func TestParseReportsAbsence(t *testing.T) {
	_, found, err := Parse([]byte("level=INFO msg=\"server listening\"\n"))
	if err != nil || found {
		t.Fatalf("want found=false, err=nil; got found=%v err=%v", found, err)
	}
}

func TestParseRejectsBrokenMarkerLine(t *testing.T) {
	line := strings.TrimSuffix(`{"schema":"`+Schema+`","routes":[`, "\n")
	_, found, err := Parse([]byte(line + "\n"))
	if err == nil || !found {
		t.Fatalf("a marker line that does not decode must be an error (found=%v err=%v)", found, err)
	}
}
