package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedAgedTempObject writes one object under the cleanup prefix and one
// outside it, both older than any max-age the tests use. The object outside
// the prefix is the control: it isolates a failure to the prefix sweep and
// rules out the store or the mount being at fault.
func seedAgedTempObject(t *testing.T) (*LocalStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewLocalStore(LocalConfig{Path: dir})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	for _, key := range []string{"_tmp/borrable.txt", "importante.txt"} {
		if _, err := store.Put(ctx, key, strings.NewReader("x"), PutOptions{}); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	aged := time.Now().Add(-48 * time.Hour)
	for _, rel := range []string{filepath.Join("_tmp", "borrable.txt"), "importante.txt"} {
		if err := os.Chtimes(filepath.Join(dir, rel), aged, aged); err != nil {
			t.Fatalf("Chtimes %s: %v", rel, err)
		}
	}
	return store, dir
}

func mustExist(t *testing.T, store *LocalStore, key string, want bool) {
	t.Helper()
	got, err := store.Exists(context.Background(), key)
	if err != nil {
		t.Fatalf("Exists(%s): %v", key, err)
	}
	if got != want {
		t.Errorf("Exists(%s) = %v, want %v", key, got, want)
	}
}

// TestCleanerStart_DisabledDeletesNothing is the regression guard for a
// kill switch that did not kill anything.
//
// `storage.cleanup.enabled: false` built a Cleaner whose fields were
// identical to the enabled one — Cleaner carried no record of the flag at
// all — and Start() only ever checked the interval. Since run() sweeps once
// immediately, before the first tick, every boot deleted the user's aged
// objects under the prefix. Silently: nothing logged that the cleaner had
// started against an explicit `false`.
//
// The test enters through Start(), which is the door app.New uses
// (app.go: `cleaner.Start()`, unconditional). The pre-existing test entered
// through runCleanup() directly and therefore asserted deletion WITH
// Enabled:false — it encoded the defect as the expected behaviour.
func TestCleanerStart_DisabledDeletesNothing(t *testing.T) {
	store, _ := seedAgedTempObject(t)

	cleaner, err := NewCleaner(store, CleanupConfig{
		Enabled:  false,
		Prefix:   "_tmp/",
		MaxAge:   "24h",
		Interval: "20ms",
	}, nil)
	if err != nil {
		t.Fatalf("NewCleaner: %v", err)
	}
	cleaner.Start()
	defer cleaner.Stop()

	// Comfortably more than one interval: if the sweep runs at all — on the
	// immediate pass or on a tick — it has had its chance.
	time.Sleep(200 * time.Millisecond)

	mustExist(t, store, "_tmp/borrable.txt", true)
	mustExist(t, store, "importante.txt", true)
}

// TestCleanerStart_EnabledDeletesAgedTempObjects is the positive control:
// without it the test above could pass on a cleaner that never works at all.
func TestCleanerStart_EnabledDeletesAgedTempObjects(t *testing.T) {
	store, _ := seedAgedTempObject(t)

	cleaner, err := NewCleaner(store, CleanupConfig{
		Enabled:  true,
		Prefix:   "_tmp/",
		MaxAge:   "24h",
		Interval: "20ms",
	}, nil)
	if err != nil {
		t.Fatalf("NewCleaner: %v", err)
	}
	cleaner.Start()
	defer cleaner.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := store.Exists(context.Background(), "_tmp/borrable.txt"); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mustExist(t, store, "_tmp/borrable.txt", false)
	// Outside the prefix, same age: the sweep must not touch it.
	mustExist(t, store, "importante.txt", true)
}
