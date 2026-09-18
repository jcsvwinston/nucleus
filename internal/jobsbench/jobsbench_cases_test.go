// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package jobsbench

// controls is the bench: every capability the jobs, events and real-time
// surface is measured on, with the verdict this repository RECORDS for it.
//
// The list is a policy choice, not a derivation of what happens to exist. It
// comes from what an application needs from background work, taken from the
// products that already answer it — Sidekiq and Oban and Solid Queue for the
// queue, Phoenix Channels and Action Cable and Django Channels for real time,
// Rails' ActiveSupport::Notifications and Elixir's PubSub for the bus — which
// is the same comparison the maturity audit of 2026-09-03 scored this
// repository against.
//
// What is deliberately NOT here: the panel that displays all of this. A
// dashboard of queues, retries and a dead letter queue is Orbit's surface and
// it is measured where it lives, in orbit/internal/adminbench (family
// "operations"). Measuring it from here would assert a claim about another
// repository's build.
//
// A verdict is what the probe MEASURED on the day it was recorded. Moving one
// is a deliberate edit in the change that moves the code.
func controls() []control {
	return []control{
		// ---- queue -------------------------------------------------------
		{id: "JOB-01", family: "queue", title: "a job runs with no external service to operate",
			want: present, probe: probeNoExternalService},
		{id: "JOB-02", family: "queue", title: "a job accepted before a restart still runs after it",
			want: present, note: "measured against the SQL provider, which an application selects with jobs_provider: sql — the same criterion the auth and admin benches use for an opt-in the framework ships. The DEFAULT provider still holds jobs in memory and loses them on restart, by design; JOB-01 measures what it does give you",
			probe: probeSurvivesRestart},
		{id: "JOB-03", family: "queue", title: "a durable queue backed by the database the application already has",
			want: present, probe: probeSQLProvider},
		{id: "JOB-04", family: "queue", title: "named queues, so slow work cannot starve fast work",
			want: present, note: "measured against the SQL provider: the order queues are configured in is the priority, and a worker does not touch a queue it does not serve. The in-process provider still refuses any queue but the default one",
			probe: probeNamedQueues},
		{id: "JOB-05", family: "queue", title: "a failing job is retried with a growing wait",
			want: present, probe: probeRetryBackoff},
		{id: "JOB-06", family: "queue", title: "the retry curve is the application's to choose",
			want: present, note: "EnqueuePolicy carries BackoffBase and BackoffMax since S2, and the SQL provider honours them per job. The other two providers ignore them and say so: the memory one keeps its fixed curve, and asynq's retry delay is server-wide, not per task",
			probe: probeRetryPolicy},
		{id: "JOB-07", family: "queue", title: "a job that exhausts its retries is kept, not dropped",
			want: present, probe: probeDeadLetter},
		{id: "JOB-08", family: "queue", title: "a dead job can be put back",
			want: present, probe: probeRequeueDead},
		{id: "JOB-09", family: "queue", title: "what is pending, running and failed is visible",
			want: present, probe: probeQueueInspection},
		{id: "JOB-10", family: "queue", title: "a job whose type no worker handles is not lost",
			want: present, probe: probeUnhandledType},
		{id: "JOB-11", family: "queue", title: "a job can be scheduled for later",
			want: present, probe: probeDelayedJob},
		{id: "JOB-12", family: "queue", title: "a job can be bounded by its own deadline",
			want: present, probe: probeJobTimeout},
		{id: "JOB-13", family: "queue", title: "the same job enqueued twice runs once",
			want: present, note: "measured against the SQL provider: EnqueuePolicy.UniqueKey reserves the work while it is queued or running, and the second enqueue is told which job it collapsed into. The in-process provider has nowhere durable to hold the reservation and ignores the key",
			probe: probeUniqueness},

		// ---- events ------------------------------------------------------
		{id: "EVT-01", family: "events", title: "an emitted event reaches a handler in the same process",
			want: present, probe: probeInProcessBus},
		{id: "EVT-02", family: "events", title: "a handler receives its payload typed",
			want: present, note: "signals.Define/Subscribe/Publish carry the payload type, delivered ALONGSIDE the untyped API — Event.Payload is still `any` for the handlers that predate it, and QADR-0010 holds that until the major at the close of A12",
			probe: probeTypedPayload},
		{id: "EVT-03", family: "events", title: "a panicking handler costs its own event, not the process",
			want: present, probe: probeHandlerPanic},
		{id: "EVT-04", family: "events", title: "a burst of asynchronous emissions is bounded",
			want: present, probe: probeAsyncBounded},
		{id: "EVT-12", family: "events", title: "emitting asynchronously returns to the caller",
			want: present, probe: probeAsyncNonBlocking},
		{id: "EVT-05", family: "events", title: "an event can reach another replica",
			want: present, probe: probeCrossReplicaRelay},
		{id: "EVT-06", family: "events", title: "a message is enqueued inside the caller's transaction",
			want: present, probe: probeOutboxTransactional},
		{id: "EVT-07", family: "events", title: "the transactional path and the in-process bus are one bus",
			want: present, note: "outbox.NewBusBridge delivers onto the event bus, so a message written inside a transaction reaches the same handlers as one published directly. pkg/observability remains a separate path — it carries SQL and HTTP traces, not application events",
			probe: probeOutboxIsBusTransport},
		{id: "EVT-08", family: "events", title: "a failed delivery is retried with the reason kept",
			want: present, probe: probeOutboxRetries},
		{id: "EVT-09", family: "events", title: "a failed message can be put back",
			want: present, probe: probeOutboxRequeue},
		{id: "EVT-10", family: "events", title: "the state of ONE topic can be asked for",
			want: absent, note: "NU-76: InspectRuntime counts every topic at once, so a panel that wants to say `three mails pending` has to write the dialect-quoted SQL itself",
			probe: probeOutboxPerTopic},
		{id: "EVT-11", family: "events", title: "the reason a delivery failed survives where an operator looks",
			want: absent, note: "NU-76: the per-message LastError is stored but the runtime snapshot does not carry it, so a stuck topic shows a count and no cause",
			probe: probeOutboxLastError},

		// ---- realtime ----------------------------------------------------
		{id: "RT-01", family: "realtime", title: "an application can complete a WebSocket handshake",
			want: partial, note: "the framework contributes a predicate (IsWebSocketUpgrade) and a writer that can be hijacked; the handshake, framing and ping/pong are the application's to bring",
			probe: probeWebSocketUpgrade},
		{id: "RT-02", family: "realtime", title: "an application can stream server-sent events",
			want: partial, note: "it streams, but every line is hand-written: headers, Flush loop, client tracking. There is no SSE helper and no channel behind it",
			probe: probeSSEStream},
		{id: "RT-03", family: "realtime", title: "a long stream survives the default timeouts",
			want: present, probe: probeStreamSurvivesTimeout},
		{id: "RT-04", family: "realtime", title: "one message can be pushed to everyone on a topic",
			want: absent, note: "there is no channel in the framework: a module's runtime offers no broadcast, so fan-out is the application's own map of connections",
			probe: probeChannelBroadcast},
		{id: "RT-05", family: "realtime", title: "joining a topic is authenticated like a route",
			want: absent, note: "with no channel there is no join to authorise: an application that hand-rolls WebSockets also hand-rolls the session check on the upgrade",
			probe: probeChannelAuth},
		{id: "RT-06", family: "realtime", title: "who is connected to a topic can be asked",
			want: absent, note: "no presence: Phoenix Presence and Action Cable's stream identifiers have no counterpart",
			probe: probeChannelPresence},
		{id: "RT-07", family: "realtime", title: "a broadcast reaches clients attached to another replica",
			want: absent, note: "the signals relay carries events between processes, but nothing carries a channel message to the sockets another replica holds",
			probe: probeChannelRelay},
		{id: "RT-08", family: "realtime", title: "a test can drive a long-lived connection",
			want: absent, note: "nucleustest drives requests; there is no helper that opens a stream, reads events and closes it, so the tests an application writes for real time are its own",
			probe: probeChannelTestKit},

		// ---- ops ---------------------------------------------------------
		{id: "OPS-01", family: "ops", title: "liveness has an endpoint of its own",
			want: absent, note: "/livez is not served: an orchestrator restarts on the same answer it drains on, so a slow dependency reads as a dead process",
			probe: probeLiveness},
		{id: "OPS-02", family: "ops", title: "readiness has an endpoint of its own",
			want: absent, note: "/readyz is not served: there is no way to say `alive but not ready` while a worker drains or a dependency recovers",
			probe: probeReadiness},
		{id: "OPS-03", family: "ops", title: "health answers per dependency",
			want: present, probe: probeHealthPerDependency},
		{id: "OPS-04", family: "ops", title: "a profiler is reachable and not public",
			want: absent, note: "/debug/pprof is not served at all: an application that wants a heap profile of a stuck worker mounts net/http/pprof itself, and then owns protecting it",
			probe: probeProfilerProtected},
		{id: "OPS-05", family: "ops", title: "the queue's work is instrumented",
			want: present, probe: probeJobMetrics},
		{id: "OPS-06", family: "ops", title: "a sqlite application waits instead of failing under contention",
			want: absent, note: "NU-77: the framework passes the sqlite DSN through bare, so busy_timeout stays 0 and a second writer fails immediately instead of waiting — the outbox dispatcher and a migrating module are exactly two writers",
			probe: probeSQLiteBusyTimeout},
		{id: "OPS-07", family: "ops", title: "a module can create its schema before the outbox starts reading",
			want: absent, note: "NU-77, measured from inside a module's OnStart: the outbox table is ALREADY there, so the dispatcher — first pass immediate, 1s poll — reached the database before any module could migrate. Two writers on one sqlite file, which is why OPS-06 costs a failed boot rather than a wait",
			probe: probeOutboxBootOrder},
	}
}
