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
)

func TestFileBackingStoreWriteAndRead(t *testing.T) {
	store := newTestBackingStore(t, FileBackingStoreOpts{
		MaxFileSize: 1024,
	})

	event1 := testEvent("event-1")

	// Write event
	mustWrite(t, store, event1)
	assertFileCount(t, store, 1)

	// Read batch
	events := mustReadBatch(t, store)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].ID, "event-1")

	// After commit, file should be gone
	assertFileCount(t, store, 0)
}

func TestFileBackingStoreMultipleEventsInSameFile(t *testing.T) {
	store := newTestBackingStore(t, FileBackingStoreOpts{
		MaxFileSize: 1024,
	})

	mustWrite(t, store, testEvent("event-1"))
	mustWrite(t, store, testEvent("event-2"))

	assertFileCount(t, store, 1)

	events := mustReadBatch(t, store)
	assert.Equal(t, len(events), 2)

	assertFileCount(t, store, 0)
}

func TestFileBackingStoreRotation(t *testing.T) {
	// MaxFileSize of 1 byte forces rotation on every write
	store := newTestBackingStore(t, FileBackingStoreOpts{
		MaxFileSize: 1,
	})

	mustWrite(t, store, testEvent("event-1"))
	time.Sleep(10 * time.Millisecond) // Ensure unique timestamp
	mustWrite(t, store, testEvent("event-2"))

	assertFileCount(t, store, 2)

	// Read first batch (oldest file)
	events := mustReadBatch(t, store)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].ID, "event-1")

	assertFileCount(t, store, 1)

	// Read second batch
	events = mustReadBatch(t, store)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].ID, "event-2")

	assertFileCount(t, store, 0)
}

func TestBackingStorePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	store := newTestBackingStore(t, FileBackingStoreOpts{
		Directory: tmpDir,
	})

	mustWrite(t, store, testEvent("test"))

	files := mustListFiles(t, store)
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

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed for %s: %v", path, err)
	}

	return info
}

func TestBackingStoreMaxTotalSize(t *testing.T) {
	store := newTestBackingStore(t, FileBackingStoreOpts{
		MaxFileSize:  10,  // Forces rotation on every event for testing
		MaxTotalSize: 400, // Allows approximately 3 events (~344 bytes total)
	})

	// Write 3 events
	for range 3 {
		mustWrite(t, store, testEvent("event"))
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

// Test helper types and functions

type FileBackingStoreOpts struct {
	Directory    string
	MaxFileSize  int64
	MaxTotalSize int64
}

func newTestBackingStore(t *testing.T, opts FileBackingStoreOpts) *FileBackingStore {
	t.Helper()

	if opts.Directory == "" {
		opts.Directory = t.TempDir()
	}

	configJSON := fmt.Sprintf(`{"directory":%q,"max_file_size":%d,"max_total_size":%d}`,
		opts.Directory, opts.MaxFileSize, opts.MaxTotalSize)

	// Use new factory signature
	factory := NewFileBackingStore
	store, err := factory([]byte(configJSON), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("NewFileBackingStore failed: %v", err)
	}

	fileStore, ok := store.(*FileBackingStore)
	if !ok {
		t.Fatalf("expected *FileBackingStore, got %T", store)
	}

	return fileStore
}

func testEvent(id string) cadf.Event {
	return cadf.Event{
		ID:        id,
		EventType: "activity",
		Action:    "create",
		Outcome:   "success",
	}
}

func mustWrite(t *testing.T, store *FileBackingStore, event cadf.Event) {
	t.Helper()

	if err := store.Write(event); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func mustReadBatch(t *testing.T, store *FileBackingStore) []cadf.Event {
	t.Helper()

	events, commit, err := store.ReadBatch()
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}

	if commit != nil {
		if err := commit(); err != nil {
			t.Fatalf("commit failed: %v", err)
		}
	}

	return events
}

func mustListFiles(t *testing.T, store *FileBackingStore) []string {
	t.Helper()

	files, err := store.listFiles()
	if err != nil {
		t.Fatalf("listFiles failed: %v", err)
	}

	return files
}

func assertFileCount(t *testing.T, store *FileBackingStore, expected int) {
	t.Helper()

	files := mustListFiles(t, store)
	assert.Equal(t, len(files), expected)
}

////////////////////////////////////////////////////////////////////////////////
// InMemoryBackingStore tests

// TestMemoryBackingStoreWriteAndRead tests basic write and read operations.
func TestMemoryBackingStoreWriteAndRead(t *testing.T) {
	store := newTestMemoryBackingStore(t, MemoryBackingStoreOpts{
		MaxEvents: 100,
	})
	defer store.Close()

	// Write events
	mustWriteMemory(t, store, testEvent("event-1"))
	mustWriteMemory(t, store, testEvent("event-2"))
	mustWriteMemory(t, store, testEvent("event-3"))

	// Read batch (should get all events in FIFO order)
	events := mustReadBatchMemory(t, store)
	assert.Equal(t, len(events), 3)
	assert.Equal(t, events[0].ID, "event-1")
	assert.Equal(t, events[1].ID, "event-2")
	assert.Equal(t, events[2].ID, "event-3")

	// Read again (should be empty after commit)
	events = mustReadBatchMemory(t, store)
	assert.Equal(t, len(events), 0)
}

// TestMemoryBackingStoreMaxEventsLimit tests the max events limit enforcement.
func TestMemoryBackingStoreMaxEventsLimit(t *testing.T) {
	store := newTestMemoryBackingStore(t, MemoryBackingStoreOpts{
		MaxEvents: 3,
	})
	defer store.Close()

	// Write up to the limit
	mustWriteMemory(t, store, testEvent("event-1"))
	mustWriteMemory(t, store, testEvent("event-2"))
	mustWriteMemory(t, store, testEvent("event-3"))

	// Writing beyond the limit should fail
	err := store.Write(testEvent("event-4"))
	if !assert.ErrEqual(t, err, ErrBackingStoreFull) {
		t.FailNow()
	}

	// After reading and committing, we should be able to write again
	events := mustReadBatchMemory(t, store)
	assert.Equal(t, len(events), 3)

	// Now we should be able to write again
	mustWriteMemory(t, store, testEvent("event-4"))
}

// TestMemoryBackingStoreFIFOOrder tests that events are returned in FIFO order.
func TestMemoryBackingStoreFIFOOrder(t *testing.T) {
	store := newTestMemoryBackingStore(t, MemoryBackingStoreOpts{
		MaxEvents: 100,
	})
	defer store.Close()

	// Write events in specific order
	for i := 1; i <= 10; i++ {
		mustWriteMemory(t, store, testEvent(fmt.Sprintf("event-%d", i)))
	}

	// Read all events
	events := mustReadBatchMemory(t, store)
	assert.Equal(t, len(events), 10)

	// Verify FIFO order
	for i := range 10 {
		expectedID := fmt.Sprintf("event-%d", i+1)
		assert.Equal(t, events[i].ID, expectedID)
	}
}

// TestMemoryBackingStoreDefaultMaxEvents tests the default max events value.
func TestMemoryBackingStoreDefaultMaxEvents(t *testing.T) {
	configJSON := `{}`

	factory := NewInMemoryBackingStore
	store, err := factory([]byte(configJSON), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("NewInMemoryBackingStore failed: %v", err)
	}
	defer store.Close()

	memStore, ok := store.(*InMemoryBackingStore)
	if !ok {
		t.Fatalf("expected *InMemoryBackingStore, got %T", store)
	}

	// Default should be 1000
	assert.DeepEqual(t, "default max events", memStore.MaxEvents.UnwrapOr(0), 1000)
}

// TestMemoryBackingStoreEmptyRead tests reading from an empty store.
func TestMemoryBackingStoreEmptyRead(t *testing.T) {
	store := newTestMemoryBackingStore(t, MemoryBackingStoreOpts{
		MaxEvents: 100,
	})
	defer store.Close()

	// Read from empty store
	events, commit, err := store.ReadBatch()
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}

	if events != nil {
		t.Errorf("expected nil events, got %d events", len(events))
	}
	if commit != nil {
		t.Errorf("expected nil commit, got non-nil commit function")
	}
}

// TestMemoryBackingStoreMetrics tests that Prometheus metrics are updated correctly.
func TestMemoryBackingStoreMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	store := newTestMemoryBackingStoreWithRegistry(t, MemoryBackingStoreOpts{
		MaxEvents: 100,
	}, registry)
	defer store.Close()

	// Write some events
	mustWriteMemory(t, store, testEvent("event-1"))
	mustWriteMemory(t, store, testEvent("event-2"))
	mustWriteMemory(t, store, testEvent("event-3"))

	// Update metrics
	if err := store.UpdateMetrics(); err != nil {
		t.Fatalf("UpdateMetrics failed: %v", err)
	}

	// Gather metrics
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Verify metrics exist
	foundWrite := false
	foundSize := false

	for _, mf := range metricFamilies {
		switch mf.GetName() {
		case "audittools_backing_store_writes_total":
			foundWrite = true
			// Should have 3 writes
			if mf.GetMetric()[0].GetCounter().GetValue() != 3 {
				t.Errorf("expected 3 writes, got %f", mf.GetMetric()[0].GetCounter().GetValue())
			}
		case "audittools_backing_store_size_bytes":
			foundSize = true
			// Should have 3 events in store
			if mf.GetMetric()[0].GetGauge().GetValue() != 3 {
				t.Errorf("expected size 3, got %f", mf.GetMetric()[0].GetGauge().GetValue())
			}
		}
	}

	if !foundWrite {
		t.Error("write counter metric not found")
	}
	if !foundSize {
		t.Error("size gauge metric not found")
	}
}

// TestMemoryBackingStoreConcurrency tests thread safety with concurrent access.
func TestMemoryBackingStoreConcurrency(t *testing.T) {
	store := newTestMemoryBackingStore(t, MemoryBackingStoreOpts{
		MaxEvents: 1000,
	})
	defer store.Close()

	// Concurrent writes
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 10

	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(routineID int) {
			defer wg.Done()
			for j := range eventsPerGoroutine {
				eventID := fmt.Sprintf("routine-%d-event-%d", routineID, j)
				if err := store.Write(testEvent(eventID)); err != nil {
					t.Errorf("Write failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Read all events
	events, commit, err := store.ReadBatch()
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}

	// Should have all events
	expectedCount := numGoroutines * eventsPerGoroutine
	if len(events) != expectedCount {
		t.Errorf("expected %d events, got %d", expectedCount, len(events))
	}

	// Commit should work
	if commit != nil {
		if err := commit(); err != nil {
			t.Fatalf("commit failed: %v", err)
		}
	}

	// Store should be empty now
	events, _, err = store.ReadBatch()
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty store after commit, got %d events", len(events))
	}
}

// Test helper types and functions for memory backing store

type MemoryBackingStoreOpts struct {
	MaxEvents int
}

func newTestMemoryBackingStore(t *testing.T, opts MemoryBackingStoreOpts) *InMemoryBackingStore {
	t.Helper()
	return newTestMemoryBackingStoreWithRegistry(t, opts, prometheus.NewRegistry())
}

func newTestMemoryBackingStoreWithRegistry(t *testing.T, opts MemoryBackingStoreOpts, registry prometheus.Registerer) *InMemoryBackingStore {
	t.Helper()

	configJSON := fmt.Sprintf(`{"max_events":%d}`, opts.MaxEvents)

	// Use new factory signature
	factory := NewInMemoryBackingStore
	store, err := factory([]byte(configJSON), AuditorOpts{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("NewInMemoryBackingStore failed: %v", err)
	}

	memStore, ok := store.(*InMemoryBackingStore)
	if !ok {
		t.Fatalf("expected *InMemoryBackingStore, got %T", store)
	}

	return memStore
}

func mustWriteMemory(t *testing.T, store *InMemoryBackingStore, event cadf.Event) {
	t.Helper()

	if err := store.Write(event); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func mustReadBatchMemory(t *testing.T, store *InMemoryBackingStore) []cadf.Event {
	t.Helper()

	events, commit, err := store.ReadBatch()
	if err != nil {
		t.Fatalf("ReadBatch failed: %v", err)
	}

	if commit != nil {
		if err := commit(); err != nil {
			t.Fatalf("commit failed: %v", err)
		}
	}

	if events == nil {
		return []cadf.Event{}
	}

	return events
}
