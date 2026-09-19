// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package jobstelemetry holds the metric instruments every job provider
// records to.
//
// It exists because the instruments used to live INSIDE the asynq provider, so
// an application that did not operate a Redis recorded nothing at all: the
// queue could be filling up, failing and dying with no series to alert on
// (NU-81). Instrumentation belongs to the subsystem, not to one of its
// backends.
//
// It is internal on purpose: the names are a contract with whoever scrapes
// them, but the Go surface is not, and pkg/tasks is frozen.
package jobstelemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	mu sync.Mutex
	// boundTo is the meter provider the instruments below were built from.
	// They are rebuilt when it changes.
	//
	// The first provider an application installs is covered without this:
	// OTel's global provider delegates instruments created before the first
	// otel.SetMeterProvider. It delegates ONCE, so what this guards against is
	// the second provider — instruments built before it never reach it, and
	// everything recorded from then on is scraped by nobody. That is every test
	// suite that stands an application up twice, and it is why the bench
	// measured zero series depending on which probe ran first.
	boundTo metric.MeterProvider

	enqueued  metric.Int64Counter
	started   metric.Int64Counter
	succeeded metric.Int64Counter
	retried   metric.Int64Counter
	failed    metric.Int64Counter
	held      metric.Int64Counter
	duration  metric.Float64Histogram
)

// The instrument names keep the jobs.* prefix the asynq provider already
// published, so a dashboard built against that one keeps working when an
// application moves to another provider — which is the point of the metrics
// belonging to the subsystem rather than to one backend.
func bind() {
	mu.Lock()
	defer mu.Unlock()
	current := otel.GetMeterProvider()
	if current == boundTo && enqueued != nil {
		return
	}
	meter := current.Meter("nucleus/tasks")
	enqueued, _ = meter.Int64Counter("jobs.enqueue.total")
	started, _ = meter.Int64Counter("jobs.process.started")
	succeeded, _ = meter.Int64Counter("jobs.process.succeeded")
	retried, _ = meter.Int64Counter("jobs.process.retried")
	failed, _ = meter.Int64Counter("jobs.process.failed")
	held, _ = meter.Int64Counter("jobs.process.held")
	duration, _ = meter.Float64Histogram("jobs.process.duration.ms")
	boundTo = current
}

// read returns the instruments under the lock, so a rebuild cannot be observed
// half-done.
func read() (e, st, su, r, f, h metric.Int64Counter, d metric.Float64Histogram) {
	mu.Lock()
	defer mu.Unlock()
	return enqueued, started, succeeded, retried, failed, held, duration
}

func attrs(provider, queue, taskType string) metric.MeasurementOption {
	return metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("queue", queue),
		attribute.String("type", taskType),
	)
}

// Enqueued records a job accepted into the queue.
func Enqueued(ctx context.Context, provider, queue, taskType string) {
	bind()
	if e, _, _, _, _, _, _ := read(); e != nil {
		e.Add(ctx, 1, attrs(provider, queue, taskType))
	}
}

// Started records a job picked up by a worker.
func Started(ctx context.Context, provider, queue, taskType string) {
	bind()
	if _, st, _, _, _, _, _ := read(); st != nil {
		st.Add(ctx, 1, attrs(provider, queue, taskType))
	}
}

// Succeeded records a handler that returned without an error, and how long it
// took.
func Succeeded(ctx context.Context, provider, queue, taskType string, took time.Duration) {
	bind()
	_, _, su, _, _, _, d := read()
	if su != nil {
		su.Add(ctx, 1, attrs(provider, queue, taskType))
	}
	if d != nil {
		d.Record(ctx, float64(took.Milliseconds()), attrs(provider, queue, taskType))
	}
}

// Retried records a failure that will be attempted again.
func Retried(ctx context.Context, provider, queue, taskType string, took time.Duration) {
	bind()
	_, _, _, r, _, _, d := read()
	if r != nil {
		r.Add(ctx, 1, attrs(provider, queue, taskType))
	}
	if d != nil {
		d.Record(ctx, float64(took.Milliseconds()), attrs(provider, queue, taskType))
	}
}

// Failed records a job that has run out of attempts.
func Failed(ctx context.Context, provider, queue, taskType string) {
	bind()
	if _, _, _, _, f, _, _ := read(); f != nil {
		f.Add(ctx, 1, attrs(provider, queue, taskType))
	}
}

// Held records a job kept instead of run — waiting for a handler that is not
// registered here, or put aside by a shutdown. It is the series that tells an
// operator a producer was deployed ahead of its worker.
func Held(ctx context.Context, provider, queue, taskType, reason string) {
	bind()
	if _, _, _, _, _, h, _ := read(); h != nil {
		h.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("queue", queue),
			attribute.String("type", taskType),
			attribute.String("reason", reason),
		))
	}
}
