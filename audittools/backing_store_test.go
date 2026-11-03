// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/assert"

	"github.com/sapcc/go-bits/must"
)

////////////////////////////////////////////////////////////////////////////////
// shared test infrastructure

func testEvent(id string) cadf.Event {
	return cadf.Event{
		ID:        id,
		EventType: "activity",
		Action:    "create",
		Outcome:   "success",
	}
}

func mustWrite(t *testing.T, store BackingStore, ids ...string) {
	t.Helper()
	for _, id := range ids {
		must.SucceedT(t, store.Write(testEvent(id)))
	}
}

// mustReadBatch reads one batch, and then reports the first `published` events in it as published.
// If `published` is negative, all events are reported as published.
// Returns the IDs of all events in the batch.
func mustReadBatch(t *testing.T, store BackingStore, published int) []string {
	t.Helper()
	events, commit, err := store.ReadBatch()
	must.SucceedT(t, err)

	ids := []string{}
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	if commit != nil {
		if published < 0 {
			published = len(events)
		}
		must.SucceedT(t, commit(published))
	}
	return ids
}

func newTestMemoryBackingStore(t *testing.T, params string) *inMemoryBackingStore {
	t.Helper()
	store := must.ReturnT(NewInMemoryBackingStore(json.RawMessage(params), AuditorOpts{
		Registry: prometheus.NewPedanticRegistry(),
	}))(t)
	return store.(*inMemoryBackingStore)
}

func newTestFileBackingStore(t *testing.T, params string) *fileBackingStore {
	t.Helper()
	params = strings.Replace(params, "{", fmt.Sprintf(`{"directory":%q,`, t.TempDir()), 1)
	params = strings.Replace(params, ",}", "}", 1)
	store := must.ReturnT(NewFileBackingStore(json.RawMessage(params), AuditorOpts{
		Registry: prometheus.NewPedanticRegistry(),
	}))(t)
	return store.(*fileBackingStore)
}

func testWithEachTypeOfStore(t *testing.T, action func(*testing.T, BackingStore)) {
	t.Run("with memory store", func(t *testing.T) {
		action(t, newTestMemoryBackingStore(t, `{}`))
	})
	t.Run("with file store", func(t *testing.T) {
		action(t, newTestFileBackingStore(t, `{}`))
	})
	t.Run("with PostgreSQL store", func(t *testing.T) {
		action(t, newTestSQLBackingStore(t, connectForTest(t), `{}`))
	})
}

////////////////////////////////////////////////////////////////////////////////
// tests for all types of store

func TestBackingStoreWriteAndRead(t *testing.T) {
	testWithEachTypeOfStore(t, func(t *testing.T, store BackingStore) {
		events, commit, err := store.ReadBatch()
		assert.ErrEqual(t, err, nil)
		assert.Equal(t, len(events), 0)
		assert.Equal(t, commit == nil, true)

		mustWrite(t, store, "event-1", "event-2", "event-3")
		assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-1", "event-2", "event-3"})
		assert.Equal(t, mustReadBatch(t, store, -1), []string{})
	})
}

func TestBackingStorePartialCommit(t *testing.T) {
	testWithEachTypeOfStore(t, func(t *testing.T, store BackingStore) {
		mustWrite(t, store, "event-1", "event-2", "event-3")

		// nothing published -> same batch again
		assert.Equal(t, mustReadBatch(t, store, 0), []string{"event-1", "event-2", "event-3"})
		// first event published -> only that one is removed
		assert.Equal(t, mustReadBatch(t, store, 1), []string{"event-1", "event-2", "event-3"})
		mustWrite(t, store, "event-4")
		ids := mustReadBatch(t, store, -1)
		ids = append(ids, mustReadBatch(t, store, -1)...) // the file store has event-4 in a separate file
		assert.Equal(t, ids, []string{"event-2", "event-3", "event-4"})
		assert.Equal(t, mustReadBatch(t, store, -1), []string{})
	})
}

func TestBackingStoreRejectsUnknownParams(t *testing.T) {
	db := connectForTest(t)
	factories := map[string]BackingStoreFactory{
		"memory": NewInMemoryBackingStore,
		"file":   NewFileBackingStore,
		"sql":    SQLBackingStoreFactoryWithPostgresDB(db),
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			params := fmt.Sprintf(`{"directory":%q,"max_event":10}`, t.TempDir())
			if name != "file" {
				params = `{"max_event":10}`
			}
			_, err := factory(json.RawMessage(params), AuditorOpts{Registry: prometheus.NewPedanticRegistry()})
			assert.ErrEqual(t, err, regexp.MustCompile(`unknown field "max_event"`))
		})
	}
}

func TestBackingStoreRejectsInvalidParams(t *testing.T) {
	db := connectForTest(t)
	testCases := []struct {
		factory BackingStoreFactory
		params  string
		err     string
	}{
		{NewInMemoryBackingStore, `{"max_events":-1}`, "invalid max_events for memory backing store: -1"},
		{NewFileBackingStore, `{"directory":"$DIR","max_file_size":0}`, "invalid max_file_size for file backing store: 0"},
		{NewFileBackingStore, `{"directory":"$DIR","max_total_size":-1}`, "invalid max_total_size for file backing store: -1"},
		{SQLBackingStoreFactoryWithPostgresDB(db), `{"batch_size":0}`, "invalid batch_size for SQL backing store: 0"},
		{SQLBackingStoreFactoryWithPostgresDB(db), `{"max_events":-1}`, "invalid max_events for SQL backing store: -1"},
	}
	for _, tc := range testCases {
		t.Run(tc.err, func(t *testing.T) {
			params := strings.ReplaceAll(tc.params, "$DIR", t.TempDir())
			_, err := tc.factory(json.RawMessage(params), AuditorOpts{Registry: prometheus.NewPedanticRegistry()})
			assert.ErrEqual(t, err, "audittools: "+tc.err)
		})
	}
}

// gatherMetrics returns the values of all metrics in the registry, summed over all label values.
func gatherMetrics(t *testing.T, registry *prometheus.Registry) map[string]float64 {
	t.Helper()
	result := make(map[string]float64)
	for _, mf := range must.ReturnT(registry.Gather())(t) {
		for _, m := range mf.GetMetric() {
			result[mf.GetName()] += m.GetCounter().GetValue() + m.GetGauge().GetValue()
		}
	}
	return result
}

////////////////////////////////////////////////////////////////////////////////
// memory store

func TestMemoryBackingStoreIsUnlimitedByDefault(t *testing.T) {
	store := newTestMemoryBackingStore(t, `{}`)
	for i := range 10000 {
		must.SucceedT(t, store.Write(testEvent(fmt.Sprintf("event-%d", i))))
	}
	assert.Equal(t, len(mustReadBatch(t, store, -1)), 10000)
}

func TestMemoryBackingStoreMaxEvents(t *testing.T) {
	store := newTestMemoryBackingStore(t, `{"max_events":3}`)
	mustWrite(t, store, "event-1", "event-2", "event-3")
	assert.ErrEqual(t, store.Write(testEvent("event-4")), errBackingStoreFull)

	mustReadBatch(t, store, 1)
	mustWrite(t, store, "event-4")
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-2", "event-3", "event-4"})
}

func TestMemoryBackingStoreMetrics(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	store := must.ReturnT(NewInMemoryBackingStore(json.RawMessage(`{"max_events":3}`), AuditorOpts{Registry: registry}))(t)

	mustWrite(t, store, "event-1", "event-2", "event-3")
	assert.ErrEqual(t, store.Write(testEvent("event-4")), errBackingStoreFull)
	mustReadBatch(t, store, 2)
	must.SucceedT(t, store.UpdateMetrics())

	assert.Equal(t, gatherMetrics(t, registry), map[string]float64{
		"audittools_backing_store_writes_total": 3,
		"audittools_backing_store_reads_total":  2,
		"audittools_backing_store_errors_total": 1,
		"audittools_backing_store_events":       1,
	})
}

////////////////////////////////////////////////////////////////////////////////
// file store

func TestFileBackingStoreLargeEvent(t *testing.T) {
	// bufio.Scanner, which an earlier version used, cannot read lines longer than 64 KiB
	store := newTestFileBackingStore(t, `{}`)
	event := testEvent("large")
	event.Attachments = []cadf.Attachment{{Name: "payload", TypeURI: "mime:text/plain", Content: strings.Repeat("x", 100<<10)}}
	must.SucceedT(t, store.Write(event))
	mustWrite(t, store, "small")

	events, commit, err := store.ReadBatch()
	must.SucceedT(t, err)
	assert.Equal(t, len(events), 2)
	assert.Equal(t, events[0], event)
	must.SucceedT(t, commit(2))
	assert.Equal(t, mustReadBatch(t, store, -1), []string{})
}

func TestFileBackingStoreReopen(t *testing.T) {
	// events written before a restart are published by the new process
	store := newTestFileBackingStore(t, `{"max_file_size":1}`)
	mustWrite(t, store, "event-1", "event-2")

	registry := prometheus.NewPedanticRegistry()
	params := fmt.Sprintf(`{"directory":%q}`, store.directory)
	reopened := must.ReturnT(NewFileBackingStore(json.RawMessage(params), AuditorOpts{Registry: registry}))(t)
	assert.Equal(t, gatherMetrics(t, registry)["audittools_backing_store_files"], 2)

	var published []string
	auditTrail{BackingStore: reopened}.drainBackingStore(func(e *cadf.Event) bool {
		published = append(published, e.ID)
		return true
	})
	assert.Equal(t, published, []string{"event-1", "event-2"})
	assert.Equal(t, len(mustListFiles(t, store)), 0)
}

func TestFileBackingStoreRotation(t *testing.T) {
	store := newTestFileBackingStore(t, `{"max_file_size":1}`)
	mustWrite(t, store, "event-1", "event-2")
	assert.Equal(t, len(mustListFiles(t, store)), 2)

	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-1"})
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-2"})
	assert.Equal(t, len(mustListFiles(t, store)), 0)
}

func TestFileBackingStoreMaxTotalSize(t *testing.T) {
	store := newTestFileBackingStore(t, `{"max_total_size":200}`)
	mustWrite(t, store, "event-1", "event-2") // about 150 bytes each
	assert.ErrEqual(t, store.Write(testEvent("event-3")), errBackingStoreFull)

	mustReadBatch(t, store, 1)
	mustWrite(t, store, "event-3")
	ids := mustReadBatch(t, store, -1)
	ids = append(ids, mustReadBatch(t, store, -1)...)
	assert.Equal(t, ids, []string{"event-2", "event-3"})
}

func TestFileBackingStorePermissions(t *testing.T) {
	store := newTestFileBackingStore(t, `{}`)
	mustWrite(t, store, "event-1")

	assert.Equal(t, must.ReturnT(os.Stat(store.directory))(t).Mode().Perm(), 0700)
	files := mustListFiles(t, store)
	assert.Equal(t, len(files), 1)
	assert.Equal(t, must.ReturnT(os.Stat(files[0]))(t).Mode().Perm(), 0600)
}

func TestFileBackingStoreCorruptLines(t *testing.T) {
	store := newTestFileBackingStore(t, `{}`)
	mustWrite(t, store, "event-1")
	files := mustListFiles(t, store)
	assert.Equal(t, len(files), 1)
	appendLine(t, files[0], `{"id":42}`) // "id" must be a string
	mustWrite(t, store, "event-2")

	// while events cannot be published, the corrupt line is left alone
	assert.Equal(t, mustReadBatch(t, store, 0), []string{"event-1", "event-2"})
	assert.Equal(t, mustReadBatch(t, store, 1), []string{"event-1", "event-2"})
	assert.Equal(t, len(mustGlob(t, store, "*.corrupt")), 0)

	// once all events are published, the corrupt line is moved aside
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-2"})
	assert.Equal(t, len(mustListFiles(t, store)), 0)
	corruptFiles := mustGlob(t, store, "*.corrupt")
	assert.Equal(t, len(corruptFiles), 1)
	assert.Equal(t, string(must.ReturnT(os.ReadFile(corruptFiles[0]))(t)), "{\"id\":42}\n")
}

func TestFileBackingStoreOnlyCorruptLines(t *testing.T) {
	store := newTestFileBackingStore(t, `{}`)
	path := filepath.Join(store.directory, "audit-events-1.jsonl")
	appendLine(t, path, `garbage`)

	events, commit, err := store.ReadBatch()
	must.SucceedT(t, err)
	assert.Equal(t, len(events), 0)
	must.SucceedT(t, commit(0))
	assert.Equal(t, len(mustListFiles(t, store)), 0)
	assert.Equal(t, len(mustGlob(t, store, "*.corrupt")), 1)
}

func TestFileBackingStoreWriteErrorStartsNewFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot provoke write errors with file permissions when running as root")
	}
	store := newTestFileBackingStore(t, `{}`)
	mustWrite(t, store, "event-1")
	files := mustListFiles(t, store)
	assert.Equal(t, len(files), 1)

	// after a failed write, the next event must not be appended to the same file,
	// where it might be glued to a partially written line
	must.SucceedT(t, os.Chmod(files[0], 0400))
	assert.ErrEqual(t, store.Write(testEvent("event-2")), regexp.MustCompile(`permission denied`))
	must.SucceedT(t, os.Chmod(files[0], 0600))
	mustWrite(t, store, "event-3")
	assert.Equal(t, len(mustListFiles(t, store)), 2)

	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-1"})
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-3"})
}

func TestWriteFileAtomicallyRemovesTempFile(t *testing.T) {
	// renaming fails because the target is a non-empty directory
	path := filepath.Join(t.TempDir(), "target")
	must.SucceedT(t, os.MkdirAll(filepath.Join(path, "subdir"), 0700))
	assert.ErrEqual(t, writeFileAtomically(path, []byte("foo\n")), regexp.MustCompile(`rename`))
	_, err := os.Stat(path + ".tmp")
	assert.Equal(t, errors.Is(err, fs.ErrNotExist), true)
}

func mustListFiles(t *testing.T, store *fileBackingStore) []string {
	t.Helper()
	files, _, err := store.listFiles()
	must.SucceedT(t, err)
	return files
}

func mustGlob(t *testing.T, store *fileBackingStore, pattern string) []string {
	t.Helper()
	return must.ReturnT(filepath.Glob(filepath.Join(store.directory, pattern)))(t)
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	must.SucceedT(t, appendToFile(path, []byte(line+"\n")))
}
