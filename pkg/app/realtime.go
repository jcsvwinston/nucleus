// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/realtime"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// RealtimeRoute is the route WithRealtime serves: one channel per topic.
// A request that asks for the WebSocket upgrade gets a WebSocket; one that
// accepts text/event-stream (an EventSource) gets server-sent events.
const RealtimeRoute = "/realtime/{topic}"

// RealtimeChannelPath is the path of one topic's channel, "/realtime/<topic>"
// — what a client connects to and what a policy row names.
func RealtimeChannelPath(topic string) string { return "/realtime/" + topic }

// realtimeTopic is what a topic may be named: a path segment a policy row
// can spell, with no room for anything a URL would have to escape.
var realtimeTopic = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// WithRealtime gives the application a realtime hub of its own (App.Realtime,
// and nucleus.RealtimeFrom for a module) and serves its channels at
// RealtimeRoute: GET /realtime/<topic> streams what is broadcast to the topic,
// over a WebSocket or as server-sent events, whichever the client asks for.
// A handler publishes with hub.Broadcast(ctx, realtime.Message{Topic: …}).
//
// It is what `nucleus add websockets` writes into main.go. Before it, the
// hub and the route were the application's to build: realtime.New in main,
// a handle threaded to every module that publishes, and a route calling
// realtime.ServeWS.
//
// A channel is a route, and it is authorised like one — there is no second
// mechanism to drift from the first. On the default stack the default-deny
// layer decides who may subscribe to a topic by its path: `p, anonymous,
// /realtime/news, read, allow`, or `p, member, /realtime/*, read, allow`. An
// application built WithoutDefaults() has no such layer, and every topic
// is open to whoever reaches the route. The channel is one-way: what a
// client sends is ignored. A browser may open the WebSocket only from the
// application's own origin (realtime.UpgradeConfig), because a browser
// sends the session cookie with the handshake and applies no CORS to it.
// The identity a channel reports for presence (realtime.Client.User) is the
// request's: the token's user, the key's owner, the signed-in account.
//
// The hub reaches the subscribers of this process; a deployment of several
// replicas builds its own hub with a realtime.Relay instead.
func WithRealtime() Option {
	return func(o *appOptions) { o.realtime = true }
}

// attachRealtime builds the hub, serves its channels and closes it at
// shutdown.
func (a *App) attachRealtime() {
	hub := realtime.New(realtime.Config{Logger: a.Logger})
	a.Realtime = hub
	a.Router.Get(RealtimeRoute, a.serveRealtime)
	a.OnShutdown(func(context.Context) error { return hub.Close() })
	a.Logger.Info("nucleus: realtime channels at GET /realtime/{topic} (WebSocket, or server-sent events for Accept: text/event-stream)")
}

// serveRealtime is one subscription: the topic from the path, the
// transport from the request.
func (a *App) serveRealtime(c *router.Context) error {
	topic := strings.TrimSpace(c.Request.PathValue("topic"))
	if !realtimeTopic.MatchString(topic) {
		return gferrors.BadRequest("a realtime topic is 1 to 128 letters, digits and . _ : - starting with a letter or a digit")
	}
	user := requestIdentity(c.Request, a.Session)
	switch {
	case realtime.IsWebSocketUpgrade(c.Request):
		if err := realtime.ServeWS(c.Writer, c.Request, realtime.WSConfig{Hub: a.Realtime, Topics: []string{topic}, User: user}); err != nil {
			// The refusal (origin, version, a hijack the writer cannot do)
			// has already been written; past the handshake the connection
			// belongs to the channel and there is nothing left to answer.
			a.Logger.Debug("realtime: websocket channel ended", "topic", topic, "error", err)
		}
		return nil
	case acceptsEventStream(c.Request):
		if err := realtime.ServeSSE(c.Writer, c.Request, realtime.SSEConfig{Hub: a.Realtime, Topics: []string{topic}, User: user}); err != nil {
			a.Logger.Debug("realtime: event stream ended", "topic", topic, "error", err)
		}
		return nil
	}
	return &gferrors.DomainError{
		Code:       "NOT_ACCEPTABLE",
		Message:    "this is a realtime channel: open it as a WebSocket, or as an EventSource (Accept: text/event-stream)",
		StatusCode: http.StatusNotAcceptable,
	}
}

// acceptsEventStream reports a client asking for server-sent events.
func acceptsEventStream(r *http.Request) bool {
	for _, v := range r.Header.Values("Accept") {
		if strings.Contains(strings.ToLower(v), "text/event-stream") {
			return true
		}
	}
	return false
}
