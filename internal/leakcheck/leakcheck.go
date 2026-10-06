// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package leakcheck fails a test binary that leaves goroutines running after
// its tests pass.
//
// It does what go.uber.org/goleak's VerifyTestMain did for the two packages
// that used it, with the same defaults — the goroutines the testing package,
// os/signal and the runtime tracer keep are expected; anything else still
// alive after twenty retries with backoff is a leak. It exists because a
// module's go.mod lists every module its own tests import, and the
// framework's go.mod is the one every application inherits (NU-106). Nothing
// outside a _test.go file imports it.
package leakcheck

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// VerifyTestMain runs the tests and, when they pass, exits non-zero if
// goroutines other than the expected ones outlive them. Call it from
// TestMain.
func VerifyTestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		if leaked := Find(); len(leaked) > 0 {
			fmt.Fprintf(os.Stderr, "leakcheck: found unexpected goroutines after a successful run:\n\n%s\n",
				strings.Join(leaked, "\n\n"))
			code = 1
		}
	}
	os.Exit(code)
}

const (
	retries  = 20
	maxSleep = 100 * time.Millisecond
)

// Find returns the stacks of the goroutines other than the caller's that are
// not expected, retrying with a backoff so a goroutine on its way out is not
// reported: 20 retries, sleeping from a microsecond doubling up to 100ms.
func Find() []string {
	for i := 0; ; i++ {
		leaked := unexpected()
		if len(leaked) == 0 || i >= retries {
			return leaked
		}
		d := time.Microsecond << uint(i)
		if d > maxSleep {
			d = maxSleep
		}
		time.Sleep(d)
	}
}

func unexpected() []string {
	var leaked []string
	for i, g := range goroutines() {
		if i == 0 {
			continue // the caller: runtime.Stack lists it first
		}
		if !expected(g) {
			leaked = append(leaked, g.raw)
		}
	}
	return leaked
}

type goroutine struct {
	state string   // "running", "chan receive", ...
	funcs []string // the functions on its stack, innermost first
	raw   string
}

func (g goroutine) first() string {
	if len(g.funcs) == 0 {
		return ""
	}
	return g.funcs[0]
}

func (g goroutine) has(fn string) bool {
	for _, f := range g.funcs {
		if f == fn {
			return true
		}
	}
	return false
}

// expected holds goleak's default filters.
func expected(g goroutine) bool {
	switch g.first() {
	case "testing.RunTests", "testing.(*T).Run", "testing.(*T).Parallel", "testing.runFuzzing", "testing.runFuzzTests":
		// The testing package's own goroutines, parked on a channel.
		if strings.HasPrefix(g.state, "chan receive") {
			return true
		}
	case "os/signal.signal_recv", "os/signal.loop":
		// Importing os/signal starts one.
		return true
	}
	switch {
	case g.has("runtime.goexit") && strings.HasPrefix(g.state, "syscall"):
		return true // cgo's background goroutine
	case g.has("runtime.ensureSigM"):
		return true // signal.Notify's
	case g.has("runtime.ReadTrace"):
		return true // the execution tracer's
	}
	return false
}

// goroutines parses runtime.Stack(all=true): one block per goroutine, a
// "goroutine N [state]:" header, then a function line and a file line per
// frame, and a "created by" line that names the parent, not a frame.
func goroutines() []goroutine {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var out []goroutine
	for _, block := range strings.Split(strings.TrimSpace(string(buf)), "\n\n") {
		lines := strings.Split(block, "\n")
		header := lines[0]
		if !strings.HasPrefix(header, "goroutine ") {
			continue
		}
		g := goroutine{raw: block}
		if open, end := strings.IndexByte(header, '['), strings.LastIndexByte(header, ']'); open >= 0 && end > open {
			g.state, _, _ = strings.Cut(header[open+1:end], ",")
		}
		for _, line := range lines[1:] {
			if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "created by ") || strings.HasPrefix(line, "...") {
				continue
			}
			if i := strings.LastIndexByte(line, '('); i > 0 {
				line = line[:i]
			}
			g.funcs = append(g.funcs, line)
		}
		out = append(out, g)
	}
	return out
}
