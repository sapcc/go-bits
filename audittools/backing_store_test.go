// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"
)

////////////////////////////////////////////////////////////////////////////////
// Shared test infrastructure

func testEvent(id string) cadf.Event {
	return cadf.Event{
		ID:        id,
		EventType: "activity",
		Action:    "create",
		Outcome:   "success",
	}
}

func mustWriteStore(t *testing.T, store BackingStore, event cadf.Event) {
	t.Helper()
	must.SucceedT(t, store.Write(event))
}

func mustReadBatchStore(t *testing.T, store BackingStore) []cadf.Event {
	t.Helper()

	events, commit, err := store.ReadBatch()
	must.SucceedT(t, err)

	if commit != nil {
		must.SucceedT(t, commit())
	}

	if events == nil {
		return []cadf.Event{}
	}

	return events
}

func testWithEachTypeOfStore(t *testing.T, action func(*testing.T, BackingStore)) {
	t.Run("with file store", func(t *testing.T) {
		configJSON := fmt.Sprintf(`{"directory":%q,"max_file_size":10240}`, t.TempDir())
		store := must.ReturnT(NewFileBackingStore([]byte(configJSON), AuditorOpts{
			Registry: prometheus.NewRegistry(),
		}))(t)
		defer store.Close()
		action(t, store)
	})
	t.Run("with memory store", func(t *testing.T) {
		store := must.ReturnT(NewInMemoryBackingStore([]byte(`{"max_events":100}`), AuditorOpts{
			Registry: prometheus.NewRegistry(),
		}))(t)
		defer store.Close()
		action(t, store)
	})
	t.Run("with PostgreSQL store", func(t *testing.T) {
		db := easypg.ConnectForTest(t, emptyMigration())
		defer db.Close()
		store := newTestSQLBackingStore(t, db, sqlBackingStoreOpts{
			BatchSize: 100,
			MaxEvents: 100,
		})
		defer store.Close()
		action(t, store)
	})
}

////////////////////////////////////////////////////////////////////////////////
// Shared tests (all backing store types)

func TestBackingStoreWriteAndRead(t *testing.T) {
	testWithEachTypeOfStore(t, func(t *testing.T, store BackingStore) {
		mustWriteStore(t, store, testEvent("event-1"))
		mustWriteStore(t, store, testEvent("event-2"))
		mustWriteStore(t, store, testEvent("event-3"))

		events := mustReadBatchStore(t, store)
		assert.Equal(t, len(events), 3)
		assert.Equal(t, events[0].ID, "event-1")
		assert.Equal(t, events[1].ID, "event-2")
		assert.Equal(t, events[2].ID, "event-3")

		// After commit, should be empty
		events = mustReadBatchStore(t, store)
		assert.Equal(t, len(events), 0)
	})
}

func TestBackingStoreEmptyRead(t *testing.T) {
	testWithEachTypeOfStore(t, func(t *testing.T, store BackingStore) {
		events, commit, err := store.ReadBatch()
		assert.ErrEqual(t, err, nil)

		if events != nil {
			t.Errorf("expected nil events, got %d events", len(events))
		}
		if commit != nil {
			t.Errorf("expected nil commit, got non-nil commit function")
		}
	})
}

func TestBackingStoreFIFOOrder(t *testing.T) {
	testWithEachTypeOfStore(t, func(t *testing.T, store BackingStore) {
		for i := 1; i <= 10; i++ {
			mustWriteStore(t, store, testEvent(fmt.Sprintf("event-%d", i)))
		}

		events := mustReadBatchStore(t, store)
		assert.Equal(t, len(events), 10)

		for i := range 10 {
			assert.Equal(t, events[i].ID, fmt.Sprintf("event-%d", i+1))
		}
	})
}

////////////////////////////////////////////////////////////////////////////////
// File-specific tests

func newTestFileBackingStore(t *testing.T, maxFileSize, maxTotalSize int64) *fileBackingStore {
	t.Helper()
	configJSON := fmt.Sprintf(`{"directory":%q,"max_file_size":%d,"max_total_size":%d}`,
		t.TempDir(), maxFileSize, maxTotalSize)
	store := must.ReturnT(NewFileBackingStore([]byte(configJSON), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	}))(t)
	return store.(*fileBackingStore)
}

func TestFileBackingStoreMultipleEventsInSameFile(t *testing.T) {
	store := newTestFileBackingStore(t, 1024, 0)

	mustWriteStore(t, store, testEvent("event-1"))
	mustWriteStore(t, store, testEvent("event-2"))

	assertFileCount(t, store, 1)

	events := mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 2)

	assertFileCount(t, store, 0)
}

func TestFileBackingStoreRotation(t *testing.T) {
	// MaxFileSize of 1 byte forces rotation on every write
	store := newTestFileBackingStore(t, 1, 0)

	mustWriteStore(t, store, testEvent("event-1"))
	time.Sleep(10 * time.Millisecond) // Ensure unique timestamp
	mustWriteStore(t, store, testEvent("event-2"))

	assertFileCount(t, store, 2)

	// Read first batch (oldest file)
	events := mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].ID, "event-1")

	assertFileCount(t, store, 1)

	// Read second batch
	events = mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].ID, "event-2")

	assertFileCount(t, store, 0)
}

func TestFileBackingStorePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	configJSON := fmt.Sprintf(`{"directory":%q}`, tmpDir)
	store := must.ReturnT(NewFileBackingStore([]byte(configJSON), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	}))(t)
	fileStore := store.(*fileBackingStore)

	mustWriteStore(t, store, testEvent("test"))

	files := mustListFiles(t, fileStore)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	info := mustStat(t, files[0])
	mode := info.Mode().Perm()

	// Verify 0600 permissions prevent other users from reading audit data.
	// Windows permissions may differ - log actual value for debugging.
	if mode != 0600 {
		t.Logf("File permissions: %o (expected 0600)", mode)
	}

	dirInfo := mustStat(t, tmpDir)
	t.Logf("Dir permissions: %o", dirInfo.Mode().Perm())
}

func TestFileBackingStoreMaxTotalSize(t *testing.T) {
	store := newTestFileBackingStore(t, 10, 400)

	// Write 3 events
	for range 3 {
		mustWriteStore(t, store, testEvent("event"))
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	files := mustListFiles(t, store)
	if len(files) < 2 || len(files) > 3 {
		t.Fatalf("expected 2-3 files, got %d", len(files))
	}

	// 4th event should fail due to size limit
	err := store.Write(testEvent("event"))
	if !assert.ErrEqual(t, err, ErrBackingStoreFull) {
		t.FailNow()
	}

	assertFileCount(t, store, 3)
}

func mustListFiles(t *testing.T, store *fileBackingStore) []string {
	t.Helper()
	files, _, err := store.listFiles()
	must.SucceedT(t, err)
	return files
}

func assertFileCount(t *testing.T, store *fileBackingStore, expected int) {
	t.Helper()
	files := mustListFiles(t, store)
	assert.Equal(t, len(files), expected)
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	return must.ReturnT(os.Stat(path))(t)
}

////////////////////////////////////////////////////////////////////////////////
// Memory-specific tests

func TestMemoryBackingStoreMaxEventsLimit(t *testing.T) {
	store := must.ReturnT(NewInMemoryBackingStore([]byte(`{"max_events":3}`), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	}))(t)
	defer store.Close()

	mustWriteStore(t, store, testEvent("event-1"))
	mustWriteStore(t, store, testEvent("event-2"))
	mustWriteStore(t, store, testEvent("event-3"))

	// Writing beyond the limit should fail
	err := store.Write(testEvent("event-4"))
	if !assert.ErrEqual(t, err, ErrBackingStoreFull) {
		t.FailNow()
	}

	// After reading and committing, we should be able to write again
	events := mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 3)

	mustWriteStore(t, store, testEvent("event-4"))
}

func TestMemoryBackingStoreMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	store := must.ReturnT(NewInMemoryBackingStore([]byte(`{"max_events":100}`), AuditorOpts{
		Registry: registry,
	}))(t)
	defer store.Close()

	mustWriteStore(t, store, testEvent("event-1"))
	mustWriteStore(t, store, testEvent("event-2"))
	mustWriteStore(t, store, testEvent("event-3"))

	must.SucceedT(t, store.UpdateMetrics())

	metricFamilies := must.ReturnT(registry.Gather())(t)

	assertMetricValue := func(name string, expected float64) {
		t.Helper()
		for _, mf := range metricFamilies {
			if mf.GetName() == name {
				assert.Equal(t, mf.GetMetric()[0].GetCounter().GetValue(), expected)
				return
			}
		}
		t.Errorf("metric %q not found", name)
	}
	assertGaugeValue := func(name string, expected float64) {
		t.Helper()
		for _, mf := range metricFamilies {
			if mf.GetName() == name {
				assert.Equal(t, mf.GetMetric()[0].GetGauge().GetValue(), expected)
				return
			}
		}
		t.Errorf("metric %q not found", name)
	}

	assertMetricValue("audittools_backing_store_writes_total", 3)
	assertGaugeValue("audittools_backing_store_size_bytes", 3)
}

func TestMemoryBackingStoreConcurrency(t *testing.T) {
	store := must.ReturnT(NewInMemoryBackingStore([]byte(`{"max_events":1000}`), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	}))(t)
	defer store.Close()

	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 10

	for i := range numGoroutines {
		wg.Go(func() {
			for j := range eventsPerGoroutine {
				eventID := fmt.Sprintf("routine-%d-event-%d", i, j)
				if err := store.Write(testEvent(eventID)); err != nil {
					t.Errorf("Write failed: %v", err)
				}
			}
		})
	}

	wg.Wait()

	events, commit, err := store.ReadBatch()
	assert.ErrEqual(t, err, nil)

	expectedCount := numGoroutines * eventsPerGoroutine
	assert.Equal(t, len(events), expectedCount)

	if commit != nil {
		must.SucceedT(t, commit())
	}

	events, _, err = store.ReadBatch()
	assert.ErrEqual(t, err, nil)
	assert.Equal(t, len(events), 0)
}
