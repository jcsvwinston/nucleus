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
			want: absent, note: "the default provider holds jobs in memory: a restart loses every job that had not run. Durability today means running asynq, which means operating a Redis",
			probe: probeSurvivesRestart},
		{id: "JOB-03", family: "queue", title: "a durable queue backed by the database the application already has",
			want: absent, note: "jobs_provider accepts memory and asynq; there is no SQL-backed provider, which is what Solid Queue and Oban are",
			probe: probeSQLProvider},
		{id: "JOB-04", family: "queue", title: "named queues, so slow work cannot starve fast work",
			want: absent, note: "the default provider refuses any queue but the default one; named queues and weights exist only under asynq",
			probe: probeNamedQueues},
		{id: "JOB-05", family: "queue", title: "a failing job is retried with a growing wait",
			want: present, probe: probeRetryBackoff},
		{id: "JOB-06", family: "queue", title: "the retry curve is the application's to choose",
			want: partial, note: "EnqueuePolicy carries MaxRetry — how many — but no field for the backoff, so every application takes the curve the provider hard-codes",
			probe: probeRetryPolicy},
		{id: "JOB-07", family: "queue", title: "a job that exhausts its retries is kept, not dropped",
			want: absent, note: "the default provider counts the failure and discards the job: nothing holds a dead job, so there is no dead letter queue to inspect",
			probe: probeDeadLetter},
		{id: "JOB-08", family: "queue", title: "a dead job can be put back",
			want: absent, note: "the API names six queue actions (pause, retry, archive…) and the default provider implements none: OperateQueue always errors",
			probe: probeRequeueDead},
		{id: "JOB-09", family: "queue", title: "what is pending, running and failed is visible",
			want: partial, note: "the default provider's snapshot carries two counters (processed, failed) and no queue rows: pending and active are always zero, whatever the queue holds",
			probe: probeQueueInspection},
		{id: "JOB-10", family: "queue", title: "a job whose type no worker handles is not lost",
			want: absent, note: "the default provider logs it, counts it failed and drops it — a producer deployed ahead of its consumer loses everything it enqueued in between",
			probe: probeUnhandledType},
		{id: "JOB-11", family: "queue", title: "a job can be scheduled for later",
			want: present, probe: probeDelayedJob},
		{id: "JOB-12", family: "queue", title: "a job can be bounded by its own deadline",
			want: present, probe: probeJobTimeout},
		{id: "JOB-13", family: "queue", title: "the same job enqueued twice runs once",
			want: absent, note: "there is no uniqueness key: Oban's unique jobs and Sidekiq's unique extension have no counterpart, so a double click is double work",
			probe: probeUniqueness},

		// ---- events ------------------------------------------------------
		{id: "EVT-01", family: "events", title: "an emitted event reaches a handler in the same process",
			want: present, probe: probeInProcessBus},
		{id: "EVT-02", family: "events", title: "a handler receives its payload typed",
			want: absent, note: "signals.Event.Payload is `any`: every handler asserts the type it hopes for, and a producer that changes it breaks consumers at run time",
			probe: probeTypedPayload},
		{id: "EVT-03", family: "events", title: "a panicking handler costs its own event, not the process",
			want: absent, note: "a panic in a synchronous handler unwinds through Emit into whatever triggered the event — a model save, a request",
			probe: probeHandlerPanic},
		{id: "EVT-04", family: "events", title: "a burst of asynchronous emissions is bounded",
			want: present, probe: probeAsyncBounded},
		{id: "EVT-12", family: "events", title: "emitting asynchronously returns to the caller",
			want: absent, note: "EmitAsync takes the concurrency slot on the CALLER's goroutine, so once 64 handlers are in flight the emitter waits for one to finish: a slow subscriber becomes latency in the request or the model save that emitted",
			probe: probeAsyncNonBlocking},
		{id: "EVT-05", family: "events", title: "an event can reach another replica",
			want: present, probe: probeCrossReplicaRelay},
		{id: "EVT-06", family: "events", title: "a message is enqueued inside the caller's transaction",
			want: present, probe: probeOutboxTransactional},
		{id: "EVT-07", family: "events", title: "the transactional path and the in-process bus are one bus",
			want: absent, note: "an outbox topic and a signal of the same name are unrelated: pkg/outbox delivers to bridges, pkg/signals to in-process handlers, and pkg/observability is a third path. An application chooses a transport, not a subscription",
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
			want: absent, note: "a job running on the default provider records NO series at all: the seven jobs.* instruments live inside the asynq provider, so an application without Redis has nothing to alert on. The outbox records none either",
			probe: probeJobMetrics},
		{id: "OPS-06", family: "ops", title: "a sqlite application waits instead of failing under contention",
			want: absent, note: "NU-77: the framework passes the sqlite DSN through bare, so busy_timeout stays 0 and a second writer fails immediately instead of waiting — the outbox dispatcher and a migrating module are exactly two writers",
			probe: probeSQLiteBusyTimeout},
		{id: "OPS-07", family: "ops", title: "a module can create its schema before the outbox starts reading",
			want: absent, note: "NU-77, measured from inside a module's OnStart: the outbox table is ALREADY there, so the dispatcher — first pass immediate, 1s poll — reached the database before any module could migrate. Two writers on one sqlite file, which is why OPS-06 costs a failed boot rather than a wait",
			probe: probeOutboxBootOrder},
	}
}
