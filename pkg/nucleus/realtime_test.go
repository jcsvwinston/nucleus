// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/realtime"
)

// A module takes the application's hub in OnStart and publishes from a
// handler; a client of /realtime/<topic> receives it. The hub is the
// runtime's: nothing in main.go builds or threads it.
func TestRealtimeFrom_AModulePublishesToTheChannel(t *testing.T) {
	var hub *realtime.Hub
	orders := nucleus.Module[struct{}]{
		Name: "orders",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			h, ok := nucleus.RealtimeFrom(rt)
			if !ok {
				return errors.New("orders: build the application WithRealtime()")
			}
			hub = h
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/orders", func(c *nucleus.Context) error {
				hub.Broadcast(c.Request.Context(), realtime.Message{Topic: "orders", Event: "created", Data: []byte(`{"id":7}`)})
				return c.NoContent()
			})
		},
	}.Build()
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(starterConfig(t, "")).
		WithoutDefaults().
		WithRealtime().
		Mount(orders))

	req, _ := http.NewRequest(http.MethodGet, srv.URL("/realtime/orders"), nil)
	req.Header.Set("Accept", "text/event-stream")
	stream, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = stream.Body.Close() }()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("subscribe: %d", stream.StatusCode)
	}

	resp, err := srv.Client().Post(srv.URL("/orders"), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("publish: %d", resp.StatusCode)
	}

	got := make(chan string, 1)
	go func() {
		lines := bufio.NewScanner(stream.Body)
		for lines.Scan() {
			if strings.HasPrefix(lines.Text(), "data:") {
				got <- lines.Text()
				return
			}
		}
	}()
	select {
	case line := <-got:
		if line != `data: {"id":7}` {
			t.Fatalf("received %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived on /realtime/orders")
	}
}

// Without the option there is no hub to hand out.
func TestRealtimeFrom_NoneWithoutTheOption(t *testing.T) {
	var found bool
	probe := nucleus.Module[struct{}]{
		Name: "probe",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			_, found = nucleus.RealtimeFrom(rt)
			return nil
		},
	}.Build()
	nucleustest.Start(t, nucleus.New().FromConfigFile(starterConfig(t, "")).WithoutDefaults().Mount(probe))
	if found {
		t.Fatal("RealtimeFrom found a hub on an application built without WithRealtime")
	}
}
