// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
)

// TestMain sets up a test database server for all tests
func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int {
		return m.Run()
	})
}

// emptyMigration provides a minimal migration config for easypg.
// The SQLBackingStore creates its own table via ensureTableExists(),
// so we only need to satisfy easypg's requirement for a non-empty migration map.
func emptyMigration() easypg.Configuration {
	return easypg.Configuration{
		Migrations: map[string]string{
			"001_empty.up.sql":   "SELECT 1",
			"001_empty.down.sql": "SELECT 1",
		},
	}
}

// TestSQLBackingStoreWriteAndRead tests basic write and read operations.
func TestSQLBackingStoreWriteAndRead(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStoreWithDB(t, db, SQLBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write events
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))
	mustWriteSQL(t, store, testEvent("event-3"))

	// Read batch (should get all events in FIFO order)
	events := mustReadBatchSQL(t, store)
	assert.DeepEqual(t, "event count", len(events), 3)
	assert.DeepEqual(t, "event 1 ID", events[0].ID, "event-1")
	assert.DeepEqual(t, "event 2 ID", events[1].ID, "event-2")
	assert.DeepEqual(t, "event 3 ID", events[2].ID, "event-3")

	// Read again (should be empty after commit)
	events = mustReadBatchSQL(t, store)
	assert.DeepEqual(t, "empty batch count", len(events), 0)
}

// TestSQLBackingStoreMaxEventsLimit tests the max events limit enforcement.
func TestSQLBackingStoreMaxEventsLimit(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStoreWithDB(t, db, SQLBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 3,
	})
	defer store.Close()

	// Write up to the limit
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))
	mustWriteSQL(t, store, testEvent("event-3"))

	// Writing beyond the limit should fail
	err := store.Write(testEvent("event-4"))
	assert.ErrEqual(t, err, ErrBackingStoreFull)

	// After reading and committing, we should be able to write again
	events := mustReadBatchSQL(t, store)
	assert.DeepEqual(t, "batch size", len(events), 3)

	// Now we should be able to write again
	mustWriteSQL(t, store, testEvent("event-4"))
}

// TestSQLBackingStoreBatchSize tests that batch size is respected.
func TestSQLBackingStoreBatchSize(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStoreWithDB(t, db, SQLBackingStoreOpts{
		BatchSize: 2,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write more events than batch size
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))
	mustWriteSQL(t, store, testEvent("event-3"))
	mustWriteSQL(t, store, testEvent("event-4"))

	// First batch should have 2 events
	events := mustReadBatchSQL(t, store)
	assert.DeepEqual(t, "first batch size", len(events), 2)
	assert.DeepEqual(t, "event 1 ID", events[0].ID, "event-1")
	assert.DeepEqual(t, "event 2 ID", events[1].ID, "event-2")

	// Second batch should have remaining 2 events
	events = mustReadBatchSQL(t, store)
	assert.DeepEqual(t, "second batch size", len(events), 2)
	assert.DeepEqual(t, "event 3 ID", events[0].ID, "event-3")
	assert.DeepEqual(t, "event 4 ID", events[1].ID, "event-4")
}

// TestSQLBackingStoreUpdateMetrics tests metrics updates.
func TestSQLBackingStoreUpdateMetrics(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStoreWithDB(t, db, SQLBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write some events
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))

	// Update metrics
	err := store.UpdateMetrics()
	if err != nil {
		t.Fatalf("UpdateMetrics failed: %v", err)
	}

	// Read and commit
	_ = mustReadBatchSQL(t, store)

	// Update metrics again
	err = store.UpdateMetrics()
	if err != nil {
		t.Fatalf("UpdateMetrics failed after read: %v", err)
	}
}

// TestSQLBackingStoreConcurrency tests concurrent write and read operations.
func TestSQLBackingStoreConcurrency(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStoreWithDB(t, db, SQLBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write events concurrently
	done := make(chan bool)
	for i := range 10 {
		go func(id int) {
			err := store.Write(testEvent(fmt.Sprintf("event-%d", id)))
			if err != nil {
				t.Errorf("concurrent write failed: %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all writes
	for range 10 {
		<-done
	}

	// Read all events
	events := mustReadBatchSQL(t, store)
	assert.DeepEqual(t, "concurrent writes count", len(events), 10)
}

// TestSQLBackingStoreTableNameValidation tests SQL injection prevention.
func TestSQLBackingStoreTableNameValidation(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	// Invalid table names should be rejected
	invalidNames := []string{
		"audit_events; DROP TABLE users;",
		"audit-events",
		"audit.events",
		"123_events",
	}

	for _, tableName := range invalidNames {
		configJSON := fmt.Sprintf(`{"table_name":%q}`, tableName)
		factory := SQLBackingStoreFactoryWithDB(db)
		_, err := factory(json.RawMessage(configJSON), AuditorOpts{
			Registry: prometheus.NewRegistry(),
		})
		if err == nil {
			t.Errorf("expected error for invalid table name %q, got nil", tableName)
		}
	}

	// Valid table names should be accepted
	validNames := []string{
		"audit_events",
		"AuditEvents",
		"_audit_events",
		"audit_events_123",
	}

	for _, tableName := range validNames {
		configJSON := fmt.Sprintf(`{"table_name":%q}`, tableName)
		factory := SQLBackingStoreFactoryWithDB(db)
		store, err := factory(json.RawMessage(configJSON), AuditorOpts{
			Registry: prometheus.NewRegistry(),
		})
		if err != nil {
			t.Errorf("expected no error for valid table name %q, got: %v", tableName, err)
			continue
		}
		store.Close()
	}
}

// Test helper types and functions

type SQLBackingStoreOpts struct {
	TableName string
	BatchSize int
	MaxEvents int
}

func newTestSQLBackingStoreWithDB(t *testing.T, db *sql.DB, opts SQLBackingStoreOpts) *SQLBackingStore {
	t.Helper()

	if opts.TableName == "" {
		// Unique table name per test enables parallel test execution without conflicts.
		opts.TableName = "audit_events_test_" + t.Name()
	}

	configJSON := fmt.Sprintf(`{"table_name":%q,"batch_size":%d,"max_events":%d}`,
		opts.TableName, opts.BatchSize, opts.MaxEvents)

	// Use new factory signature with AuditorOpts
	factory := SQLBackingStoreFactoryWithDB(db)
	store, err := factory(json.RawMessage(configJSON), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	sqlStore, ok := store.(*SQLBackingStore)
	if !ok {
		t.Fatalf("expected *SQLBackingStore, got %T", store)
	}

	// Clean up table on test completion
	t.Cleanup(func() {
		//nolint:errcheck // cleanup in test
		_, _ = db.Exec("DROP TABLE IF EXISTS " + sqlStore.TableName)
	})

	return sqlStore
}

func mustWriteSQL(t *testing.T, store *SQLBackingStore, event cadf.Event) {
	t.Helper()

	if err := store.Write(event); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func mustReadBatchSQL(t *testing.T, store *SQLBackingStore) []cadf.Event {
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
