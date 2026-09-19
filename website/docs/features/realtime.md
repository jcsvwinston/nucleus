---
sidebar_position: 7
title: Real-time channels
covers: []
config_keys:
  - request_timeout
  - timeout_exempt_paths[]
---

# Real-time channels

`pkg/realtime` pushes messages to connected clients: a hub that owns topics,
the two transports a browser understands, presence, and a relay for when the
application runs in more than one process.
[Storage & background tasks](./storage-and-tasks.md#real-time-pkgrealtime)
introduces it in one screen; this page is the detail.

One thing to know before building on it: this package is **not part of the API
freeze**. It is a transitional package, and no `pkg/realtime` symbol appears
in the frozen baseline, so the no-removal guarantee that covers `pkg/router`
or `pkg/signals` does not cover the names below. Everything described here is
what ships today.

## Hubs, topics and clients

A **hub** owns the connections this process holds and the topics they
subscribed to. A **topic** is a string you choose — `"orders"`, `"room:42"`,
`"user:17:notifications"`. There is no registration step: a topic exists while
at least one client is subscribed to it and disappears when the last one
leaves.

```go
import "github.com/jcsvwinston/nucleus/pkg/realtime"

hub := realtime.New(realtime.Config{Logger: logger})
defer func() { _ = hub.Close() }()
```

`Config` takes a `Logger` (`slog.Default()` when omitted), a `ClientBuffer`
(64 when omitted — see [A slow client](#a-slow-client)) and an optional
`Relay`. One hub per process is the usual arrangement: build it in your
bootstrap, capture it in the closures that serve the routes, close it on
shutdown. `Close` disconnects everybody and closes the relay if there is one.

A **client** is one connection. `Subscribe(id, user, topics...)` registers it:
the id must be non-empty and unique among the connections the hub currently
holds, and at least one topic is required (`ErrNoTopics` otherwise). The
transports below call it for you and unsubscribe when the connection ends —
you call it directly only when writing a transport of your own. The `user` is
whatever the handler decided; the package never derives it. `Topics()`,
`Count(topic)` and `Unsubscribe(id)` round out the surface, and all of them
answer for this process alone.

## Broadcasting

```go
payload, err := json.Marshal(order)
if err != nil {
    return err
}
hub.Broadcast(ctx, realtime.Message{
    Topic: "orders",
    Event: "created",
    ID:    order.ID,
    Data:  payload,
})
```

`Data` is already encoded — the package does not marshal for you. `Broadcast`
returns nothing: it delivers to this process's subscribers and then hands the
message to the relay, and a relay failure is logged rather than returned.

`Event` and `ID` reach an SSE client as the `event:` and `id:` fields. The
WebSocket transport writes `Data` alone, so a WebSocket client that needs to
know which kind of message it received needs that inside the payload. This is
the main place the two transports deliver different things; they also differ in
what each one says when the hub drops a client
([A slow client](#a-slow-client)) and in how each refuses a duplicate
`ClientID`. It is worth deciding once for the whole application.

## Serving a channel over server-sent events

```go
r.Get("/live", func(c *nucleus.Context) error {
    return realtime.ServeSSE(c.Writer, c.Request, realtime.SSEConfig{
        Hub:    hub,
        Topics: []string{"orders"},
        User:   currentUser(c),
        OnConnect: func(send func(realtime.Message)) {
            send(realtime.Message{Event: "snapshot", Data: currentOrders()})
        },
    })
})
```

`ServeSSE` sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
`Connection: keep-alive` and `X-Accel-Buffering: no` (nginx buffers proxied
responses by default, which turns a live stream into one lump at the end),
writes the 200 and flushes it, then holds the connection until the request
context is cancelled or the hub disconnects the client. It refuses early in
four cases: no hub is a 500, no topics a 400, a response writer that is not an
`http.Flusher` a 500, and a `ClientID` already connected to this hub a 409.
Leaving `ClientID` empty derives a fresh one.

`OnConnect` runs once the client is subscribed and before the first broadcast
reaches it. That ordering is what makes it the place to send current state: a
client that connects halfway through the day sees where things stand instead
of waiting for the next change.

A payload with newlines gets one `data:` prefix per line, so a multi-line JSON
document arrives whole. Every `KeepAlive` (25 seconds by default) the stream
writes a `: keep-alive` comment, which clients ignore and which stops a proxy
deciding the connection is dead. The timer runs on a fixed interval; a stream
that has just delivered a message writes the comment anyway.

`Message.ID` is written as the `id:` field and a browser sends it back as
`Last-Event-ID` when it reconnects. The framework does not read that header
and the hub keeps no history, so resuming from an id is something you build on
top — the usual answer is to send current state from `OnConnect` instead.
`StreamContext` runs the same handler with the request's context **replaced**
by the one you pass, so the stream ends when that context is done and no longer
when the client disconnects. Derive it from `r.Context()` — with
`context.WithCancel` or a deadline on top — when you want both.

## Serving a channel over WebSocket

```go
r.Get("/ws/rooms/{room}", func(c *nucleus.Context) error {
    room := c.Param("room")
    return realtime.ServeWS(c.Writer, c.Request, realtime.WSConfig{
        Hub:    hub,
        Topics: []string{"room:" + room},
        User:   currentUser(c),
        OnMessage: func(client *realtime.Client, data []byte) error {
            hub.Broadcast(c.Request.Context(), realtime.Message{
                Topic: "room:" + room,
                Data:  data,
            })
            return nil
        },
    })
})
```

`ServeWS` completes the handshake — including the `Sec-WebSocket-Accept` key
computed from the client's — then subscribes, reassembles fragmented inbound
messages, answers the peer's pings and writes every broadcast as one unmasked
text frame. The handshake is refused with 400 when the request is not an
upgrade or carries no `Sec-WebSocket-Key`, with 426 and a
`Sec-WebSocket-Version: 13` header when the client does not ask for version 13
— including when it sends no version header at all — with 403 when the origin
check says no, and with 500 when the response writer cannot be hijacked.

One refusal arrives after the handshake rather than instead of it. `ServeWS`
upgrades first and subscribes second, so a `ClientID` already connected to this
hub — the case `ServeSSE` answers with a 409 — gets a completed 101 followed
immediately by a close frame carrying 1000, the same code a normal close uses.
There is no status and no reason for the client to read. Leave `ClientID`
empty, or derive it per connection, not per user: two tabs of one person are
two connections.

`OnMessage` receives what the client sends, and returning an error from it
ends the connection. It receives the payload without the frame's opcode, so a
handler cannot tell a binary message from a text one; text and binary frames
both arrive as `[]byte`. Leaving it nil ignores inbound messages, which is the
right default for a one-way channel — the read loop still runs, because it is
what answers the peer's pings. `OnConnect` works as it does for SSE, except
that what it sends goes out as `Data` only.

Outbound, `ServeWS` writes every message — broadcasts and whatever `OnConnect`
sends — as a text frame. `Conn.WriteBinary` exists but nothing in `ServeWS`
reaches it, so broadcasting binary means calling `realtime.Upgrade` yourself
and driving the `*Conn` it returns.

The connection is pinged every `PingInterval` (30 seconds by default), on a
fixed timer that traffic does not reset. Note the limit: the server sends the
ping but does not enforce a deadline on the pong, so a peer that vanished
without closing the TCP connection is noticed when a write to it fails or when
the request context is cancelled, not one interval later.
`Conn.SetReadDeadline` is exported for applications that drive a connection
themselves and want that bound.

### Which transport

Reach for SSE unless something forces the other choice. It is plain HTTP, so
it crosses proxies that mangle upgrades; browsers reconnect on their own; and
it carries the event name and the id without you encoding them. Its limits are
that it is one-way and text-only. Take WebSocket when the client has to send
as well — a chat room, a collaborative cursor, an acknowledgement — or when it
sends binary frames: they reach `OnMessage`, as `[]byte` the handler cannot
tell from text. It does not buy you binary in the other direction: `ServeWS`
writes text. And you own the reconnection
behaviour on the client side, because a browser `WebSocket` does not reconnect
by itself the way an `EventSource` does.

Either way, **authorisation happens in the handler**, before the call, the way
it does for any other route: a channel is a route, and giving it a second
authorisation mechanism is how the two drift apart. What the handler decides,
it passes in as `User`.

## Presence

`Presence(topic)` reports who is connected, one entry per **connection** —
`{ID, User, Topics}`, sorted by id:

```go
for _, entry := range hub.Presence("room:42") {
    log.Printf("connection %s belongs to %s", entry.ID, entry.User)
}
```

Two browser tabs belonging to one person are two entries, which is what a
"3 devices" badge needs; count distinct `User` values when the question is
"who is here". The relay carries broadcasts between replicas and not presence,
so on more than one process `Presence` answers for one replica, not for the
cluster.

## The origin check

This is the security boundary of the WebSocket transport, so it is worth being
precise. `UpgradeConfig.CheckOrigin` decides whether a handshake is allowed.
Left nil — the default — the rule is:

- No `Origin` header at all: **allowed**. Browsers always send one; native
  clients and tests do not.
- An `Origin` header: **allowed only when it matches the request's `Host`**.
  Everything after `://` in the `Origin` is compared case-insensitively to
  `Host`. The port is part of both and therefore part of the comparison; the
  scheme is not compared.
- Anything else: **403**, before the connection is hijacked.

The default is same-origin because a browser sends the session cookie with a
WebSocket handshake and does not apply CORS to it. A handshake accepted from
another origin is a cross-site request carrying the user's session, and
whatever the socket then streams is readable by the page that opened it.

Setting `CheckOrigin` replaces that rule entirely — `func(*http.Request) bool
{ return true }` accepts every origin on the internet. If your front end
genuinely lives somewhere else, compare against the origins you wrote down:

```go
allowed := map[string]bool{"https://app.example.com": true}

cfg := realtime.WSConfig{
    Hub:    hub,
    Topics: []string{"orders"},
    Upgrade: realtime.UpgradeConfig{
        MaxMessageBytes: 64 << 10,
        CheckOrigin: func(r *http.Request) bool {
            return allowed[r.Header.Get("Origin")]
        },
    },
}
```

`ServeSSE` performs no origin check of its own: an SSE stream is an ordinary
HTTP response, covered by the authentication, the CORS policy and the
authorisation the route already carries.

## Message size and close codes

`MaxMessageBytes` bounds **inbound** messages; unset it is 1 MiB
(`DefaultMaxMessageBytes`). It is applied to the frame header before any
buffer is allocated, because that length is attacker-controlled: a client that
announces a gigabyte is refused, not allocated for.

- A frame whose declared length is over the limit — including one that only
  announces it and sends nothing — is answered with close code **1009**
  (`message too big`). Fragments that cross the limit while being reassembled
  get the same code.
- A client **data** frame — text, binary or continuation — that is not masked
  is answered with close code **1002**. RFC 6455 requires every frame from a
  client to be masked, and an unmasked one is either a broken client or
  something that is not a browser. Control frames are handled before that
  check, so an unmasked ping is still answered with a pong, an unmasked pong
  is still ignored, and an unmasked close still ends the connection normally.
- `Conn.Close`, the normal end of a connection, sends **1000**.

In each refusal the close frame is written and the socket closed immediately
after; `ServeWS` returns and the client is unsubscribed. Outbound messages are
not bounded by that limit — the server writes what you broadcast, in one
unmasked frame with no fragmentation. They are bounded in time instead: at
most five seconds waiting for the connection's write lock, then a ten-second
write deadline, so one peer that stopped reading cannot hold the connection
for ever.

## A slow client

Each client has a send buffer of `ClientBuffer` messages, 64 by default.
`Broadcast` never blocks on a client whose buffer is full: it increments that
client's dropped counter and disconnects it, logging a warning that names the
connection and the topic. Blocking the broadcaster would make one stalled
browser everybody's problem and an unbounded buffer would make it the
process's, whereas disconnecting makes it the client's. An `EventSource`
reconnects on its own and the new request resubscribes it; a browser
`WebSocket` does not, and nothing in this package resubscribes it, so on that
transport the reconnection is code you write.

What the client sees depends on the transport. An SSE stream writes one last
event before it ends:

```
event: nucleus.disconnected
data: {"reason":"too slow","dropped":1}
```

A WebSocket connection is closed with the normal 1000 code and the count is
not sent.

Read that count for what it is. `Client.Dropped()` counts the sends that
failed, and the first failure is what disconnects the client — so after an
eviction it is 1, whether the connection went on to miss ten messages or ten
thousand. Treat it as a flag that this connection fell behind, not as how many
messages it missed: the hub does not keep that number, and a badge built on it
would always read one. Because the hub keeps no history, a client that
reconnects receives what happens next and nothing from while it was away;
sending current state from `OnConnect` is what closes that gap.

## More than one process

A hub broadcasts to the clients **this process** holds, so with a second
replica running half your users stop seeing updates — the failure that looks
like a flake and is not. A `Relay` carries broadcasts between replicas, and
`RedisRelay` is the implementation that ships:

```go
relay, err := realtime.NewRedisRelay(realtime.RedisRelayConfig{
    URL:     cfg.RedisURL,
    Channel: "myapp:realtime",
})
if err != nil {
    return err
}

hub := realtime.New(realtime.Config{Logger: logger, Relay: relay})
if err := hub.StartRelay(ctx); err != nil {
    return err
}
defer func() { _ = hub.Close() }()
```

`StartRelay` starts **this** replica's subscriber in a goroutine and sleeps 10
milliseconds before returning. The pause is a fixed sleep, not a confirmation
that the subscription landed: it gives the subscription a moment before the
process starts work, and a broadcast another replica publishes inside that
window can still be missed here. It says nothing about the other direction
either — what this replica publishes goes straight to Redis from `Broadcast`,
and whether the other replicas receive it depends on their own `StartRelay`.
The subscription runs until `ctx` is done. If it fails earlier the error is
logged; if the pub/sub channel simply closes, the subscriber stops without an
error and without a line.

Neither call reports whether Redis is reachable, so neither one tells you the
relay works. The example checks both errors because both signatures return one,
but today `NewRedisRelay` fails only on a missing or unparseable URL and
`StartRelay` returns nil on every path it has. A wrong host, a wrong password
or a Redis that is down starts cleanly, and the first sign is the relay error
`Broadcast` logs. Register `health.NewRedisProbe` among your health checks if
you want that connection reported rather than discovered.

`Channel` defaults to `nucleus:realtime`, so two applications sharing one Redis
database must set distinct channels — or better, use distinct databases.

**What it does about its own messages.** `Broadcast` delivers locally first
and publishes second. Every published envelope carries the replica's `Origin`
— a generated identifier when you do not set one — and the subscriber skips
envelopes whose origin is its own, because delivering the relayed copy as well
would show every client every message twice. Messages arriving from another
replica are delivered to local subscribers and not published back out, so they
cannot loop.

The skip is a string comparison, which makes `Origin` a value that must differ
per replica. Set it to something stable and shared — an application name, a
deployment name, anything every pod reads from the same config — and each
replica discards every other replica's broadcasts: the relay goes quiet, with
no error anywhere, and you are back to half your users not seeing updates.
Leave it empty unless you have a per-instance identifier to hand.

Redis pub/sub rather than a stream, deliberately: nothing is persisted, so a
message published while a subscriber is briefly disconnected is gone. If
losing it is not acceptable then it is not a broadcast — use the
[transactional outbox](./storage-and-tasks.md#transactional-outbox-pkgoutbox)
or a background task, and broadcast from the consumer.

## Timeouts and routing

A channel is a route and gets the default middleware stack, where one default
matters: `request_timeout` (30 seconds) wraps handlers in
`http.TimeoutHandler`, which buffers the whole response and hides
`http.Flusher` — behind it a stream cannot flush a byte until it returns.

The router already exempts the two cases this page is about. A request whose
`Accept` header asks for `text/event-stream` is exempt, which is what a
browser's `EventSource` always sends, and so is a WebSocket upgrade; both work
with no configuration. A stream served to a client that does not send that
header — `curl`, a native client — needs its path prefix listed instead.
Entries are matched with `strings.HasPrefix`, not compared as whole paths, so
an entry of `/api` also exempts `/api-internal` and everything under it: list
the narrowest prefix that covers the stream, or the 30-second timeout quietly
comes off a whole subtree. An exempt prefix gets the raw response writer, with
`Flush` and `Hijack` working, and no deadline:

```yaml
request_timeout: 30s
timeout_exempt_paths:
  - /live
```

In tests, the kit opens a stream for you: `srv.Stream("/live")` returns an
open connection, `Next(limit)` reads the next event and skips keep-alive
comments, and `Quiet(limit)` asserts that nothing arrived — the assertion an
authorisation bug slips past when nobody writes it.
