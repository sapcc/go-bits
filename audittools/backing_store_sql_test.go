// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/assert"
)

// TestSQLBackingStoreWriteAndRead tests basic write and read operations.
func TestSQLBackingStoreWriteAndRead(t *testing.T) {
	store := newTestSQLBackingStore(t, SQLBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 100,
	})
	defer cleanupTestSQLBackingStore(t, store)

	// Write events
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))
	mustWriteSQL(t, store, testEvent("event-3"))

	// Read batch (should get all events in FIFO order)
	events := mustReadBatchSQL(t, store)
	assert.Equal(t, len(events), 3)
	assert.Equal(t, events[0].ID, "event-1")
	assert.Equal(t, events[1].ID, "event-2")
	assert.Equal(t, events[2].ID, "event-3")

	// Read again (should be empty after commit)
	events = mustReadBatchSQL(t, store)
	assert.Equal(t, len(events), 0)
}

// TestSQLBackingStoreMaxEventsLimit tests the max events limit enforcement.
func TestSQLBackingStoreMaxEventsLimit(t *testing.T) {
	store := newTestSQLBackingStore(t, SQLBackingStoreOpts{
		BatchSize: 10,
		MaxEvents: 3,
	})
	defer cleanupTestSQLBackingStore(t, store)

	// Write up to the limit
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))
	mustWriteSQL(t, store, testEvent("event-3"))

	// Writing beyond the limit should fail
	err := store.Write(testEvent("event-4"))
	if !assert.ErrEqual(t, err, ErrBackingStoreFull) {
		t.FailNow()
	}

	// After reading and committing, we should be able to write again
	events := mustReadBatchSQL(t, store)
	assert.Equal(t, len(events), 3)

	// Now we should be able to write again
	mustWriteSQL(t, store, testEvent("event-4"))
}

// TestSQLBackingStoreBatchSize tests that batch size is respected.
func TestSQLBackingStoreBatchSize(t *testing.T) {
	store := newTestSQLBackingStore(t, SQLBackingStoreOpts{
		BatchSize: 2,
		MaxEvents: 100,
	})
	defer cleanupTestSQLBackingStore(t, store)

	// Write more events than batch size
	mustWriteSQL(t, store, testEvent("event-1"))
	mustWriteSQL(t, store, testEvent("event-2"))
	mustWriteSQL(t, store, testEvent("event-3"))
	mustWriteSQL(t, store, testEvent("event-4"))

	// First batch should have 2 events
	events := mustReadBatchSQL(t, store)
	assert.Equal(t, len(events), 2)
	assert.Equal(t, events[0].ID, "event-1")
	assert.Equal(t, events[1].ID, "event-2")

	// Second batch should have remaining 2 events
	events = mustReadBatchSQL(t, store)
	assert.Equal(t, len(events), 2)
	assert.Equal(t, events[0].ID, "event-3")
	assert.Equal(t, events[1].ID, "event-4")
}

// TestSQLBackingStoreSkipMigration tests that skip_migration works.
func TestSQLBackingStoreSkipMigration(t *testing.T) {
	dsn := getTestDSN(t)

	// Create table manually
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	tableName := fmt.Sprintf("audit_events_test_%d", os.Getpid())

	// Create table manually
	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGSERIAL PRIMARY KEY,
			event_data JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, tableName))
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	defer func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + tableName) //nolint:errcheck // cleanup in test
	}()

	// Create backing store with skip_migration
	configJSON := fmt.Sprintf(`{"type":"sql","params":{"dsn":%q,"table_name":%q,"skip_migration":true}}`,
		dsn, tableName)

	store, err := NewBackingStore(configJSON, nil)
	if err != nil {
		t.Fatalf("NewBackingStore failed: %v", err)
	}
	defer store.Close()

	sqlStore, ok := store.(*SQLBackingStore)
	if !ok {
		t.Fatalf("expected *SQLBackingStore, got %T", store)
	}

	// Should be able to write and read
	mustWriteSQL(t, sqlStore, testEvent("event-1"))
	events := mustReadBatchSQL(t, sqlStore)
	assert.Equal(t, len(events), 1)
}

// TestSQLBackingStoreTableNameValidation tests SQL injection prevention.
func TestSQLBackingStoreTableNameValidation(t *testing.T) {
	dsn := getTestDSN(t)

	// Invalid table names should be rejected
	invalidNames := []string{
		"audit_events; DROP TABLE users;",
		"audit-events",
		"audit.events",
		"123_events",
		"",
	}

	for _, tableName := range invalidNames {
		configJSON := fmt.Sprintf(`{"type":"sql","params":{"dsn":%q,"table_name":%q}}`,
			dsn, tableName)

		_, err := NewBackingStore(configJSON, nil)
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
		configJSON := fmt.Sprintf(`{"type":"sql","params":{"dsn":%q,"table_name":%q}}`,
			dsn, tableName)

		store, err := NewBackingStore(configJSON, nil)
		if err != nil {
			t.Errorf("expected no error for valid table name %q, got: %v", tableName, err)
			continue
		}
		store.Close()

		// Clean up table
		db, err := sql.Open("postgres", dsn)
		if err == nil {
			_, _ = db.Exec("DROP TABLE IF EXISTS " + tableName) //nolint:errcheck // cleanup in test
			db.Close()
		}
	}
}

// Test helper types and functions

type SQLBackingStoreOpts struct {
	DSN        string
	TableName  string
	BatchSize  int
	MaxEvents  int
	DriverName string
}

func newTestSQLBackingStore(t *testing.T, opts SQLBackingStoreOpts) *SQLBackingStore {
	t.Helper()

	if opts.DSN == "" {
		opts.DSN = getTestDSN(t)
	}

	if opts.TableName == "" {
		// Use unique table name per test to allow parallel execution
		opts.TableName = fmt.Sprintf("audit_events_test_%d", os.Getpid())
	}

	if opts.DriverName == "" {
		opts.DriverName = "postgres"
	}

	configJSON := fmt.Sprintf(`{"type":"sql","params":{"dsn":%q,"table_name":%q,"batch_size":%d,"max_events":%d,"driver_name":%q}}`,
		opts.DSN, opts.TableName, opts.BatchSize, opts.MaxEvents, opts.DriverName)

	store, err := NewBackingStore(configJSON, nil)
	if err != nil {
		t.Fatalf("NewBackingStore failed: %v", err)
	}

	sqlStore, ok := store.(*SQLBackingStore)
	if !ok {
		t.Fatalf("expected *SQLBackingStore, got %T", store)
	}

	return sqlStore
}

func cleanupTestSQLBackingStore(t *testing.T, store *SQLBackingStore) {
	t.Helper()

	// Drop test table
	if store.db != nil {
		_, err := store.db.Exec("DROP TABLE IF EXISTS " + store.TableName)
		if err != nil {
			t.Logf("failed to drop test table: %v", err)
		}
	}

	store.Close()
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

func getTestDSN(t *testing.T) string {
	t.Helper()

	// Check for test database environment variable
	dsn := os.Getenv("AUDITTOOLS_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("AUDITTOOLS_TEST_DB_DSN not set, skipping SQL backing store tests. " +
			"Set to a PostgreSQL connection string to run these tests, e.g.: " +
			"postgres://user:password@localhost:5432/testdb?sslmode=disable")
	}

	return dsn
}
