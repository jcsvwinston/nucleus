// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// NU-87 has two gates, and they measure different things on purpose.
//
// The claim used to serialise on the head of the queue. Every worker selected
// the same first rows and raced a conditional UPDATE for them, so adding
// workers added collisions, not throughput — at sixteen workers on PostgreSQL
// nine claims in ten came back empty, and the manager then slept a whole poll
// interval with the work still waiting. Measured on 2026-10-04, the real
// manager ran 1217 jobs/s with one worker and 1667 with sixteen.
//
// TestSQLMatrix_JobsClaimsDoNotCollide measures the cause, and it does not
// depend on the host: with free work in the queue, a claim must not come back
// empty. TestSQLMatrix_JobsQueueScalesWithWorkers measures the effect an
// application sees — sixteen workers drain the queue at least twice as fast as
// one — and that one DOES depend on the host, which is why its floor is two
// and not the four a workstation shows. On a twelve-core machine sixteen
// workers ran 4.4 to 6 times as fast as one; on the four-vCPU CI runner the
// host is saturated by four workers already (PostgreSQL 2938 jobs/s at four,
// 3697 at sixteen) and the ratio tops out at 2.9 on PostgreSQL and 2.4 on
// MySQL. The claim this replaced stays at 1.0 to 1.4 whatever the host.

// TestSQLMatrix_JobsQueueScalesWithWorkers: on an engine with SKIP LOCKED,
// sixteen workers drain the queue at least twice as fast as one.
//
// What it measures is a RATIO, never a rate. A rate measures the machine —
// NU-95 was four tests that did, and all four went red on a loaded CI host
// with nothing wrong. One worker and sixteen run on the same engine, in the
// same job, seconds apart; whatever the host costs, it costs both. The rounds
// are interleaved and each worker count keeps its best, so one slow moment
// cannot decide the verdict. And there is a ceiling on every round, so a queue
// that stops making progress fails here, naming what it had done, instead of
// running into the package timeout.
//
// The handler does nothing on purpose. With real work in the handler, workers
// spend their time there and collide less, and even a claim that serialises
// looks like it scales; with none, the claim is all there is to measure.
//
// It also checks the claim's contract under contention, which the throughput
// is worthless without: every job runs, and none runs twice. Nothing crashes
// here, so the at-least-once allowance does not apply — a duplicate would mean
// two workers were handed the same row.
func TestSQLMatrix_JobsQueueScalesWithWorkers(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping the live queue throughput test")
	}
	if testing.Short() {
		t.Skip("the throughput test drains the queue twelve times; skipped under -short")
	}
	if raceEnabled {
		t.Skip("throughput under the race detector measures the detector")
	}
	lower := strings.ToLower(rawURL)
	var flavor Flavor
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		flavor = FlavorPostgres
	case strings.HasPrefix(lower, "mysql://"):
		flavor = FlavorMySQL
	default:
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}

	db := openMatrixDB(t, rawURL)
	// One connection per worker, kept. database/sql keeps two idle by
	// default, and sixteen workers on two idle connections spend their time
	// opening and closing the rest — which measures the pool, not the claim.
	db.SetMaxIdleConns(32)
	table := fmt.Sprintf("jobs_scale_%d", time.Now().UnixNano())
	store, err := NewStore(db, Config{TableName: table, Flavor: flavor})
	if err != nil {
		t.Fatalf("open the queue against %s: %v", flavor, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + store.quotedTable())
		_, _ = db.Exec("DROP TABLE IF EXISTS " + store.leaderTable())
	})
	// Both engines in the matrix have SKIP LOCKED (PostgreSQL 16, MySQL 8.4).
	// A store that did not detect it would run the portable claim, and this
	// test would then fail on a ratio, far from the cause.
	if !store.skipLocked {
		t.Fatalf("the store did not detect SKIP LOCKED on %s; this test needs an engine that has it (PostgreSQL, MySQL 8, MariaDB 10.6 or later)", flavor)
	}

	const (
		jobs = 3000
		// The ceiling: far above what any round takes on any host this has
		// run on, so it only trips on a queue that has stopped draining.
		roundCeiling = 3 * time.Minute
	)
	best := map[int]float64{}
	for pass := 0; pass < 2; pass++ {
		for _, workers := range []int{1, 4, 16} {
			rate := drainRound(t, store, flavor, workers, jobs, roundCeiling)
			t.Logf("%s: %2d worker(s) drained %d jobs at %.0f jobs/s (pass %d)", flavor, workers, jobs, rate, pass+1)
			if rate > best[workers] {
				best[workers] = rate
			}
		}
	}

	ratio := best[16] / best[1]
	t.Logf("%s: best of two — 1 worker %.0f jobs/s, 4 workers %.0f, 16 workers %.0f; 16 against 1 = %.1fx",
		flavor, best[1], best[4], best[16], ratio)
	const floor = 2.0
	if ratio < floor {
		t.Fatalf("on %s, 16 workers drained the queue %.1fx as fast as 1 (%.0f against %.0f jobs/s), want at least %.0fx: the claim is serialising the workers again (NU-87)",
			flavor, ratio, best[16], best[1], floor)
	}
}

// TestSQLMatrix_JobsClaimsDoNotCollide is the half of NU-87's gate that the
// host cannot move: sixteen workers claim from a queue that has plenty of free
// work, and the claims that come back EMPTY while it does are counted.
//
// An empty claim with free work waiting is exactly the collision NU-87 was:
// every worker went for the same head row, one won, and the rest got nothing.
// With SKIP LOCKED a worker steps over the rows others hold, so with at least
// twice as many jobs unclaimed as there are workers it always finds one — on a
// fast host or a slow one, which is why this is a share of claims and not a
// rate. The claim it replaced lost 75 % of its claims at four workers and 90 %
// at sixteen. It drives the store directly, without the manager, so it
// measures the claim and nothing else.
func TestSQLMatrix_JobsClaimsDoNotCollide(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping the live claim collision test")
	}
	if testing.Short() {
		t.Skip("the claim collision test drains 3000 jobs; skipped under -short")
	}
	lower := strings.ToLower(rawURL)
	var flavor Flavor
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		flavor = FlavorPostgres
	case strings.HasPrefix(lower, "mysql://"):
		flavor = FlavorMySQL
	default:
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}
	db := openMatrixDB(t, rawURL)
	db.SetMaxIdleConns(32)
	store, err := NewStore(db, Config{TableName: fmt.Sprintf("jobs_collide_%d", time.Now().UnixNano()), Flavor: flavor})
	if err != nil {
		t.Fatalf("open the queue against %s: %v", flavor, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + store.quotedTable())
		_, _ = db.Exec("DROP TABLE IF EXISTS " + store.leaderTable())
	})

	const (
		workers = 16
		jobs    = 3000
		// Free work, for certain: no more than workers-1 rows can be held by
		// the other claims in flight, and twice that leaves room for a worker
		// that read the count and was then descheduled for a while.
		freeWork = 2 * workers
	)
	bulkEnqueue(t, store, flavor, "collide", jobs)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var (
		taken, claims, emptyWithWork atomic.Int64
		wg                           sync.WaitGroup
		failed                       atomic.Value
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			for ctx.Err() == nil {
				before := taken.Load()
				if before >= jobs {
					return
				}
				got, err := store.Claim(ctx, owner, []string{"default"}, 1, 30*time.Second, time.Now())
				if err != nil {
					if ctx.Err() == nil {
						failed.Store(err)
					}
					return
				}
				claims.Add(1)
				if len(got) == 0 {
					if jobs-before >= freeWork {
						emptyWithWork.Add(1)
					}
					continue
				}
				if _, err := store.Succeed(ctx, owner, got[0].ID, time.Now()); err != nil {
					failed.Store(err)
					return
				}
				taken.Add(1)
			}
		}(fmt.Sprintf("collide-%d", i))
	}
	wg.Wait()
	if err, ok := failed.Load().(error); ok {
		t.Fatalf("on %s: %v", flavor, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("on %s: %d of %d jobs claimed in 3m; the queue stopped draining", flavor, taken.Load(), jobs)
	}

	share := float64(emptyWithWork.Load()) / float64(claims.Load())
	t.Logf("%s: %d workers made %d claims for %d jobs; %d came back empty with free work waiting (%.1f%%)",
		flavor, workers, claims.Load(), jobs, emptyWithWork.Load(), 100*share)
	if share > 0.05 {
		t.Fatalf("on %s, %.0f%% of the claims came back empty while the queue had free work, want at most 5%%: the workers are colliding on the same rows again (NU-87)",
			flavor, 100*share)
	}
}

// drainRound enqueues n jobs, runs a manager with the given number of workers
// until every one of them has run, and returns the rate. It fails the test if
// a job runs twice, if any job is left undone, or if the round passes the
// ceiling.
func drainRound(t *testing.T, store *Store, flavor Flavor, workers, n int, ceiling time.Duration) float64 {
	t.Helper()
	bulkEnqueue(t, store, flavor, fmt.Sprintf("scale-%d", workers), n)
	rate, runs := drain(t, store, flavor, workers, n, ceiling)

	duplicates := 0
	for _, count := range runs {
		if count > 1 {
			duplicates += count - 1
		}
	}
	if duplicates > 0 {
		t.Fatalf("on %s with %d worker(s): %d job(s) ran more than once with nothing crashing — two workers were handed the same row",
			flavor, workers, duplicates)
	}
	if len(runs) != n {
		t.Fatalf("on %s with %d worker(s): %d distinct jobs ran, want %d", flavor, workers, len(runs), n)
	}
	// Close waits for each worker's last Succeed, so the table is final.
	var done int
	if err := store.db.QueryRow(rebindFor(flavor, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE status = ?`, store.quotedTable())), string(StatusDone)).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if done != n {
		t.Fatalf("on %s with %d worker(s): %d jobs recorded done, want %d", flavor, workers, done, n)
	}
	if _, err := store.db.Exec("DELETE FROM " + store.quotedTable()); err != nil {
		t.Fatal(err)
	}
	return rate
}

// TestSQLMatrix_JobsClaimCostDoesNotGrowWithTheBacklog guards how the claim
// finds its rows: by reading the claim index in order and stopping at the
// first free one, not by collecting every due job and sorting them.
//
// The first SKIP LOCKED claim did the second. On PostgreSQL that passed the
// scaling test above and still cost a sort of the whole backlog per claim —
// 37 ms at 200 000 pending jobs, spilling to disk, where the index-ordered
// claim takes a tenth of a millisecond. On MySQL it is worse than slow: InnoDB
// locks every row the sort reads, so one claimer holds the queue. Nothing but
// a backlog shows the difference, so this test makes one.
//
// Same rules as the scaling test: a RATIO between two runs on the same host,
// the best of two each, and a ceiling. One worker drains a thousand jobs from
// a queue holding a thousand, and a thousand from a queue holding twenty-one
// thousand; the second must not be slower than half the first. A claim that
// sorts the backlog is several times slower there; one that reads the index is
// not slower at all.
func TestSQLMatrix_JobsClaimCostDoesNotGrowWithTheBacklog(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping the live claim cost test")
	}
	if testing.Short() {
		t.Skip("the claim cost test builds a backlog of 21 000 jobs; skipped under -short")
	}
	if raceEnabled {
		t.Skip("throughput under the race detector measures the detector")
	}
	lower := strings.ToLower(rawURL)
	var flavor Flavor
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		flavor = FlavorPostgres
	case strings.HasPrefix(lower, "mysql://"):
		flavor = FlavorMySQL
	default:
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}
	db := openMatrixDB(t, rawURL)
	db.SetMaxIdleConns(32)

	// Two tables, not two queues in one: a claim that scans the table would
	// pay for the big backlog on the small queue too, and the ratio between
	// them would say nothing.
	open := func(name string) *Store {
		store, err := NewStore(db, Config{TableName: name, Flavor: flavor})
		if err != nil {
			t.Fatalf("open the queue against %s: %v", flavor, err)
		}
		t.Cleanup(func() {
			_, _ = db.Exec("DROP TABLE IF EXISTS " + store.quotedTable())
			_, _ = db.Exec("DROP TABLE IF EXISTS " + store.leaderTable())
		})
		return store
	}
	stamp := time.Now().UnixNano()
	small := open(fmt.Sprintf("jobs_short_%d", stamp))
	big := open(fmt.Sprintf("jobs_long_%d", stamp))

	const (
		batch        = 1000
		backlog      = 20000
		roundCeiling = 3 * time.Minute
	)
	bulkEnqueue(t, big, flavor, "backlog", backlog)

	var bestShort, bestLong float64
	for pass := 0; pass < 2; pass++ {
		bulkEnqueue(t, small, flavor, fmt.Sprintf("short-%d", pass), batch)
		shortRate, _ := drain(t, small, flavor, 1, batch, roundCeiling)

		// The batch goes in AHEAD of the backlog — due earlier — so the jobs
		// drained are exactly these, and the backlog stays behind them.
		bulkEnqueueAt(t, big, flavor, fmt.Sprintf("long-%d", pass), batch, time.Now().UTC().Add(-2*time.Hour))
		longRate, _ := drain(t, big, flavor, 1, batch, roundCeiling)
		t.Logf("%s: %.0f jobs/s from a queue of %d, %.0f jobs/s from a queue of %d (pass %d)",
			flavor, shortRate, batch, longRate, backlog+batch, pass+1)
		if shortRate > bestShort {
			bestShort = shortRate
		}
		if longRate > bestLong {
			bestLong = longRate
		}
	}
	ratio := bestLong / bestShort
	t.Logf("%s: best of two — %.0f jobs/s with a backlog of %d against %.0f without; %.2fx", flavor, bestLong, backlog, bestShort, ratio)
	if ratio < 0.5 {
		t.Fatalf("on %s a backlog of %d jobs slowed every claim to %.2fx (%.0f against %.0f jobs/s), want at least 0.5x: the claim is sorting the backlog instead of reading the index in order",
			flavor, backlog, ratio, bestLong, bestShort)
	}
}

// drain runs a manager with the given number of workers until want jobs have
// run, and returns the rate and how many times each payload ran.
func drain(t *testing.T, store *Store, flavor Flavor, workers, want int, ceiling time.Duration) (float64, map[string]int) {
	t.Helper()
	m, err := NewManager(ManagerConfig{
		Store: store, Concurrency: workers, LeaseDuration: 30 * time.Second,
		ShutdownGrace: 5 * time.Second, Owner: fmt.Sprintf("drain-%d", workers),
		// PollInterval stays at its default of one second: it is what an
		// application runs with, and what a worker that lost a claim used to
		// sleep through.
	}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu   sync.Mutex
		runs = make(map[string]int, want)
		ran  atomic.Int64
		all  = make(chan struct{})
	)
	if err := m.HandleFunc("scale.noop", func(_ context.Context, task tasks.Task) error {
		mu.Lock()
		runs[string(task.Payload())]++
		mu.Unlock()
		if ran.Add(1) == int64(want) {
			close(all)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	runCtx, stop := context.WithCancel(context.Background())
	started := time.Now()
	go func() { _ = m.Run(runCtx) }()
	var elapsed time.Duration
	select {
	case <-all:
		elapsed = time.Since(started)
	case <-time.After(ceiling):
		stop()
		_ = m.Close()
		t.Fatalf("on %s with %d worker(s): %d of %d jobs ran in %s; the queue stopped draining",
			flavor, workers, ran.Load(), want, ceiling)
	}
	stop()
	_ = m.Close()
	mu.Lock()
	defer mu.Unlock()
	return float64(want) / elapsed.Seconds(), runs
}

// bulkEnqueue writes n due jobs, each due a microsecond after the last, in
// multi-row INSERTs: enqueueing one row per statement would make building a
// backlog the slowest part of the test.
func bulkEnqueue(t *testing.T, store *Store, flavor Flavor, prefix string, n int) {
	t.Helper()
	bulkEnqueueAt(t, store, flavor, prefix, n, time.Now().UTC().Add(-time.Hour))
}

func bulkEnqueueAt(t *testing.T, store *Store, flavor Flavor, prefix string, n int, from time.Time) {
	t.Helper()
	const chunk = 500
	for start := 0; start < n; start += chunk {
		end := start + chunk
		if end > n {
			end = n
		}
		rows := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*7)
		for i := start; i < end; i++ {
			rows = append(rows, "(?, 'default', 'scale.noop', ?, ?, 0, 3, 0, 0, 0, ?, ?)")
			at := from.Add(time.Duration(i) * time.Microsecond)
			args = append(args, fmt.Sprintf("%s-%06d", prefix, i), fmt.Sprintf(`{"n":"%s-%d"}`, prefix, i),
				string(StatusPending), at, at)
		}
		query := rebindFor(flavor, fmt.Sprintf(
			`INSERT INTO %s (id, queue, task_type, payload, status, attempts, max_attempts,
				timeout_ms, backoff_base_ms, backoff_max_ms, available_at, created_at)
			VALUES %s`, store.quotedTable(), strings.Join(rows, ", ")))
		if _, err := store.db.Exec(query, args...); err != nil {
			t.Fatalf("enqueue on %s: %v", flavor, err)
		}
	}
}
