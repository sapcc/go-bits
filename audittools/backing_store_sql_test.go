// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"
)

// TestMain sets up a test database server for all tests
func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int {
		return m.Run()
	})
}

// emptyMigration provides a minimal migration config for easypg.
// The sqlBackingStore creates its own table via ensureTableExists(),
// so we only need to satisfy easypg's requirement for a non-empty migration map.
func emptyMigration() easypg.Configuration {
	return easypg.Configuration{
		Migrations: map[string]string{
			"001_empty.up.sql":   "SELECT 1",
			"001_empty.down.sql": "SELECT 1",
		},
	}
}

// TestSQLBackingStoreMaxEventsLimit tests the max events limit enforcement.
func TestSQLBackingStoreMaxEventsLimit(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStore(t, db, sqlBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 3,
	})
	defer store.Close()

	// Write up to the limit
	mustWriteStore(t, store, testEvent("event-1"))
	mustWriteStore(t, store, testEvent("event-2"))
	mustWriteStore(t, store, testEvent("event-3"))

	// Writing beyond the limit should fail
	err := store.Write(testEvent("event-4"))
	assert.ErrEqual(t, err, ErrBackingStoreFull)

	// After reading and committing, we should be able to write again
	events := mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 3)

	// Now we should be able to write again
	mustWriteStore(t, store, testEvent("event-4"))
}

// TestSQLBackingStoreBatchSize tests that batch size is respected.
func TestSQLBackingStoreBatchSize(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStore(t, db, sqlBackingStoreOpts{
		BatchSize: 2,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write more events than batch size
	mustWriteStore(t, store, testEvent("event-1"))
	mustWriteStore(t, store, testEvent("event-2"))
	mustWriteStore(t, store, testEvent("event-3"))
	mustWriteStore(t, store, testEvent("event-4"))

	// First batch should have 2 events
	events := mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 2)
	assert.Equal(t, events[0].ID, "event-1")
	assert.Equal(t, events[1].ID, "event-2")

	// Second batch should have remaining 2 events
	events = mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 2)
	assert.Equal(t, events[0].ID, "event-3")
	assert.Equal(t, events[1].ID, "event-4")
}

// TestSQLBackingStoreUpdateMetrics tests metrics updates.
func TestSQLBackingStoreUpdateMetrics(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStore(t, db, sqlBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write some events
	mustWriteStore(t, store, testEvent("event-1"))
	mustWriteStore(t, store, testEvent("event-2"))

	// Update metrics
	must.SucceedT(t, store.UpdateMetrics())

	// Read and commit
	_ = mustReadBatchStore(t, store)

	// Update metrics again
	must.SucceedT(t, store.UpdateMetrics())
}

// TestSQLBackingStoreConcurrency tests concurrent write and read operations.
func TestSQLBackingStoreConcurrency(t *testing.T) {
	db := easypg.ConnectForTest(t, emptyMigration())
	defer db.Close()

	store := newTestSQLBackingStore(t, db, sqlBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 100,
	})
	defer store.Close()

	// Write events concurrently
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			err := store.Write(testEvent(fmt.Sprintf("event-%d", i)))
			if err != nil {
				t.Errorf("concurrent write failed: %v", err)
			}
		})
	}
	wg.Wait()

	// Read all events
	events := mustReadBatchStore(t, store)
	assert.Equal(t, len(events), 10)
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
		factory := SQLBackingStoreFactoryWithPostgresDB(db)
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
		factory := SQLBackingStoreFactoryWithPostgresDB(db)
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

type sqlBackingStoreOpts struct {
	TableName string
	BatchSize int
	MaxEvents int
}

func newTestSQLBackingStore(t *testing.T, db *sql.DB, opts sqlBackingStoreOpts) BackingStore {
	t.Helper()

	if opts.TableName == "" {
		// Unique table name per test enables parallel test execution without conflicts.
		// t.Name() contains "/" for subtests, which is not valid in SQL identifiers.
		sanitized := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
		opts.TableName = "audit_events_test_" + sanitized
	}

	configJSON := fmt.Sprintf(`{"table_name":%q,"batch_size":%d,"max_events":%d}`,
		opts.TableName, opts.BatchSize, opts.MaxEvents)

	factory := SQLBackingStoreFactoryWithPostgresDB(db)
	store := must.ReturnT(factory(json.RawMessage(configJSON), AuditorOpts{
		Registry: prometheus.NewRegistry(),
	}))(t)

	sqlStore := store.(*sqlBackingStore)

	// Clean up table on test completion
	t.Cleanup(func() {
		//nolint:errcheck // cleanup in test
		_, _ = db.Exec("DROP TABLE IF EXISTS " + sqlStore.TableName)
	})

	return store
}
