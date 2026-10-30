// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"testing"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/assert"
	"go.xyrillian.de/gg/pgruntime"

	"github.com/sapcc/go-bits/must"
)

// TestMain spawns a PostgreSQL server for the duration of the test run.
func TestMain(m *testing.M) {
	pgruntime.WithTestDB(m, m.Run)
}

// createTableQuery creates a table with the schema from the package documentation.
func createTableQuery(tableName string) string {
	return `CREATE TABLE ` + tableName + ` (
		id         BIGSERIAL NOT NULL PRIMARY KEY,
		event_data JSONB     NOT NULL
	)`
}

// connectForTest opens a fresh test database with the table "audit_events",
// which is created by a migration in the same way as in an application.
func connectForTest(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := pgruntime.StdConnector("postgres").ConnectForTest(t, pgruntime.ConnectionBehavior{
		Migrations: map[int64]string{1: createTableQuery("audit_events")},
	})
	return db.DB
}

func newTestSQLBackingStore(t *testing.T, db *sql.DB, params string) *sqlBackingStore {
	t.Helper()
	store := must.ReturnT(SQLBackingStoreFactoryWithPostgresDB(db)(json.RawMessage(params), AuditorOpts{
		Registry: prometheus.NewPedanticRegistry(),
	}))(t)
	return store.(*sqlBackingStore)
}

func TestSQLBackingStoreMaxEvents(t *testing.T) {
	store := newTestSQLBackingStore(t, connectForTest(t), `{"max_events":3}`)
	mustWrite(t, store, "event-1", "event-2", "event-3")
	assert.ErrEqual(t, store.Write(testEvent("event-4")), errBackingStoreFull)

	mustReadBatch(t, store, 1)
	mustWrite(t, store, "event-4")
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-2", "event-3", "event-4"})
}

func TestSQLBackingStoreIsUnlimitedByDefault(t *testing.T) {
	store := newTestSQLBackingStore(t, connectForTest(t), `{}`)
	assert.Equal(t, store.maxEvents, 0)
}

func TestSQLBackingStoreBatchSize(t *testing.T) {
	store := newTestSQLBackingStore(t, connectForTest(t), `{"batch_size":2}`)
	mustWrite(t, store, "event-1", "event-2", "event-3")
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-1", "event-2"})
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-3"})
}

func TestSQLBackingStoreSharedTable(t *testing.T) {
	// two replicas of the same service, sharing one table
	db := connectForTest(t)
	storeA := newTestSQLBackingStore(t, db, `{"batch_size":2}`)
	storeB := newTestSQLBackingStore(t, db, `{"batch_size":2}`)
	mustWrite(t, storeA, "event-1", "event-2", "event-3")

	// while A holds a batch, B only sees the rows that are not in it
	eventsA, commitA, err := storeA.ReadBatch()
	must.SucceedT(t, err)
	assert.Equal(t, len(eventsA), 2)
	assert.Equal(t, mustReadBatch(t, storeB, -1), []string{"event-3"})
	assert.Equal(t, mustReadBatch(t, storeB, -1), []string{})

	// if A fails to publish, the rows become available again
	must.SucceedT(t, commitA(0))
	assert.Equal(t, mustReadBatch(t, storeB, -1), []string{"event-1", "event-2"})
	assert.Equal(t, mustReadBatch(t, storeA, -1), []string{})
}

func TestSQLBackingStoreUnreadableRows(t *testing.T) {
	db := connectForTest(t)
	store := newTestSQLBackingStore(t, db, `{"batch_size":2}`)
	// "id" must be a string, so these rows cannot be decoded
	for range 2 {
		must.ReturnT(db.Exec(`INSERT INTO audit_events (event_data) VALUES ('{"id":42}')`))(t)
	}
	mustWrite(t, store, "event-1")

	// the first batch consists only of unreadable rows, which must not stop the drain
	var published []string
	trail := auditTrail{BackingStore: store}
	trail.drainBackingStore(func(e *cadf.Event) bool {
		published = append(published, e.ID)
		return true
	})
	assert.Equal(t, published, []string{"event-1"})
	assert.Equal(t, must.ReturnT(store.countEvents())(t), 0)
}

func TestSQLBackingStoreRequiresTable(t *testing.T) {
	db := connectForTest(t)
	factory := SQLBackingStoreFactoryWithPostgresDB(db)
	params := json.RawMessage(`{"table_name":"my_audit_events"}`)
	must.ReturnT(db.Exec(`DROP TABLE IF EXISTS my_audit_events`))(t) // the test DB is reused between runs

	// the table is not created by the backing store
	_, err := factory(params, AuditorOpts{Registry: prometheus.NewPedanticRegistry()})
	assert.ErrEqual(t, err, regexp.MustCompile(`cannot use table my_audit_events .* relation "my_audit_events" does not exist`))

	must.ReturnT(db.Exec(createTableQuery("my_audit_events")))(t)
	store := must.ReturnT(factory(params, AuditorOpts{Registry: prometheus.NewPedanticRegistry()}))(t)
	mustWrite(t, store, "event-1")
	assert.Equal(t, mustReadBatch(t, store, -1), []string{"event-1"})
}

func TestSQLBackingStoreConcurrentStartup(t *testing.T) {
	// several replicas of the same service starting at the same time, sharing one table
	db := connectForTest(t)
	factory := SQLBackingStoreFactoryWithPostgresDB(db)
	stores := make([]BackingStore, 8)
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for idx := range stores {
		wg.Go(func() {
			stores[idx], errs[idx] = factory(json.RawMessage(`{}`), AuditorOpts{Registry: prometheus.NewPedanticRegistry()})
		})
	}
	wg.Wait()
	for _, err := range errs {
		assert.ErrEqual(t, err, nil)
	}

	// all of them work on the same table
	for idx, store := range stores {
		mustWrite(t, store, fmt.Sprintf("event-%d", idx))
	}
	assert.Equal(t, len(mustReadBatch(t, stores[0], -1)), len(stores))
}

func TestSQLBackingStoreReopen(t *testing.T) {
	// events written before a restart are published by the new process
	db := connectForTest(t)
	mustWrite(t, newTestSQLBackingStore(t, db, `{}`), "event-1", "event-2")

	registry := prometheus.NewPedanticRegistry()
	store := must.ReturnT(SQLBackingStoreFactoryWithPostgresDB(db)(json.RawMessage(`{}`), AuditorOpts{Registry: registry}))(t)
	must.SucceedT(t, store.UpdateMetrics())
	assert.Equal(t, gatherMetrics(t, registry)["audittools_backing_store_events"], 2)

	var published []string
	auditTrail{BackingStore: store}.drainBackingStore(func(e *cadf.Event) bool {
		published = append(published, e.ID)
		return true
	})
	assert.Equal(t, published, []string{"event-1", "event-2"})
	assert.Equal(t, mustReadBatch(t, store, -1), []string{})
}

func TestSQLBackingStoreMetrics(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	store := must.ReturnT(SQLBackingStoreFactoryWithPostgresDB(connectForTest(t))(json.RawMessage(`{"max_events":3}`), AuditorOpts{Registry: registry}))(t)

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

func TestSQLBackingStoreTableNameValidation(t *testing.T) {
	db := connectForTest(t)
	factory := SQLBackingStoreFactoryWithPostgresDB(db)

	for _, tableName := range []string{"audit_events; DROP TABLE users;", "audit-events", "audit.events", "123_events", ""} {
		t.Run("invalid/"+tableName, func(t *testing.T) {
			params := fmt.Sprintf(`{"table_name":%q}`, tableName)
			_, err := factory(json.RawMessage(params), AuditorOpts{Registry: prometheus.NewPedanticRegistry()})
			assert.ErrEqual(t, err, regexp.MustCompile("invalid table name"))
		})
	}
	for _, tableName := range []string{"AuditEvents", "_audit_events", "audit_events_123"} {
		t.Run("valid/"+tableName, func(t *testing.T) {
			must.ReturnT(db.Exec(`DROP TABLE IF EXISTS ` + tableName))(t) // the test DB is reused between runs
			must.ReturnT(db.Exec(createTableQuery(tableName)))(t)
			params := fmt.Sprintf(`{"table_name":%q}`, tableName)
			_, err := factory(json.RawMessage(params), AuditorOpts{Registry: prometheus.NewPedanticRegistry()})
			assert.ErrEqual(t, err, nil)
		})
	}
}
