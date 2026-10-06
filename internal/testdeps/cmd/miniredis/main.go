// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Command miniredis serves an in-memory Redis for the framework's tests, in a
// process of its own.
//
// The tests that need Redis used to start miniredis in-process, which put it
// — and the Lua interpreter it embeds — into the go.mod every application
// inherits, since a module's go.mod lists what its own tests import (NU-106).
// internal/testredis builds this command from this module, whose go.mod is
// never published, and starts one process per server a test asks for.
//
// Protocol: the process listens on 127.0.0.1 at a free port, writes
// "ADDR <host:port>" on its standard output, and serves until its standard
// input closes — so a test process that dies takes its servers with it. On
// top of Redis it answers FASTFORWARD <milliseconds>, which advances
// miniredis's clock the way (*miniredis.Miniredis).FastForward did in-process.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
)

func main() {
	m := miniredis.NewMiniRedis()
	if err := m.StartAddr("127.0.0.1:0"); err != nil {
		fmt.Fprintln(os.Stderr, "miniredis:", err)
		os.Exit(1)
	}
	err := m.Server().Register("FASTFORWARD", func(c *server.Peer, _ string, args []string) {
		if len(args) != 1 {
			c.WriteError("ERR usage: FASTFORWARD <milliseconds>")
			return
		}
		ms, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || ms < 0 {
			c.WriteError("ERR FASTFORWARD takes a non-negative number of milliseconds")
			return
		}
		m.FastForward(time.Duration(ms) * time.Millisecond)
		c.WriteOK()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "miniredis:", err)
		os.Exit(1)
	}
	fmt.Printf("ADDR %s\n", m.Addr())

	// Serve until the test that started this process lets go of it.
	_, _ = io.Copy(io.Discard, os.Stdin)
	m.Close()
}
