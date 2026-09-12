// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// A handler in a really-booted application can take the connection over.
//
// The unit test next to the middleware (pkg/auth) proves the wrapper
// implements http.Hijacker. This one proves what an application author
// actually gets: the DEFAULT stack, every middleware the framework mounts by
// itself, and a route that upgrades. The gap it closes was found from the
// outside — Orbit's live feed served its snapshot and never opened its
// stream — because no test on either side booted an application and asked.
package contracts

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

func TestDefaultStackLetsARouteHijackTheConnection(t *testing.T) {
	hijackable := make(chan bool, 1)

	mod := nucleus.Module[struct{}]{
		Name: "upgrade",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/upgrade", func(c *nucleus.Context) error {
				hj, ok := c.Writer.(http.Hijacker)
				hijackable <- ok
				if !ok {
					return nil
				}
				conn, buf, err := hj.Hijack()
				if err != nil {
					return err
				}
				defer func() { _ = conn.Close() }()
				if _, err := buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\nupgraded"); err != nil {
					return err
				}
				return buf.Flush()
			})
		},
	}.Build()

	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("upgrade-contract-secret", 2)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{mod.Name(): mod},
		Options: []app.Option{app.WithOpenAuthz()},
	})

	address := strings.TrimPrefix(srv.URL("/"), "http://")
	address = strings.TrimSuffix(address, "/")
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial %s: %v", address, err)
	}
	defer func() { _ = conn.Close() }()
	// The upgrade headers are not decoration: the timeout middleware buffers
	// a normal response (and hides Hijack with it) and exempts a request that
	// declares itself an upgrade. A probe without them measures the timeout
	// handler, not the session wrapper.
	if _, err := conn.Write([]byte("GET /upgrade HTTP/1.1\r\nHost: contract\r\n" +
		"Connection: Upgrade\r\nUpgrade: websocket\r\n\r\n")); err != nil {
		t.Fatalf("request: %v", err)
	}

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if ok := <-hijackable; !ok {
		t.Fatal("the writer the route received is not an http.Hijacker: " +
			"no websocket library can upgrade through the default stack")
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("status line = %q, want the 101 the route wrote on the raw connection", strings.TrimSpace(status))
	}
	rest := make([]byte, 64)
	n, _ := reader.Read(rest)
	if body := string(rest[:n]); !strings.Contains(body, "upgraded") {
		t.Fatalf("body = %q, want what the route wrote after taking the connection over", body)
	}
}
