// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"testing"
	"time"
)

// A cron entry has to mean the same instant whichever provider serves it. The
// in-process and asynq schedulers read it in UTC; this one read it in the
// host's local zone, so moving an application from `jobs_provider: memory` to
// `jobs_provider: sql` moved every `0 3 * * *` by the offset — at night, where
// nobody is looking, and with no error anywhere.
func TestSchedulerDefaultsToUTC(t *testing.T) {
	sch, err := NewScheduler(SchedulerConfig{Manager: &Manager{}, Store: &Store{}})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	defer func() { _ = sch.Close() }()

	got := sch.cron.Location()
	if got != time.UTC {
		t.Fatalf("cron location is %v; want UTC, so an entry means the same instant on every provider", got)
	}
}

// And an application that asks for a zone still gets it.
func TestSchedulerHonoursAnExplicitLocation(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skipf("no tzdata for Europe/Madrid: %v", err)
	}
	sch, err := NewScheduler(SchedulerConfig{Manager: &Manager{}, Store: &Store{}, Location: madrid})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	defer func() { _ = sch.Close() }()

	if got := sch.cron.Location(); got != madrid {
		t.Fatalf("cron location is %v; want %v", got, madrid)
	}
}
