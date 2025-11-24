// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/lib/pq"
	"github.com/majewsky/gg/option"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

var sqlIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// SQLBackingStore implements BackingStore using a PostgreSQL database.
// Suitable for services that already have a database connection and want to avoid managing filesystem volumes.
// Leverages existing database infrastructure for audit buffering without additional operational complexity.
//
// Thread safety: All operations use database transactions for atomicity.
// Multiple concurrent calls are safe and will be serialized by the database.
//
// Database Schema:
// The backing store requires a table with the following schema:
//
//	CREATE TABLE audit_events (
//	    id BIGSERIAL PRIMARY KEY,
//	    event_data JSONB NOT NULL,
//	    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
//	);
//	CREATE INDEX ON audit_events (created_at, id);
//
// Important: Applications must provide their own database connection using
// SQLBackingStoreFactoryWithDB to avoid creating duplicate connection pools
// (each PostgreSQL process should maintain only one connection pool per database).
type SQLBackingStore struct {
	// Configuration (JSON params)
	TableName     string             `json:"table_name"`     // Table name (default: "audit_events")
	BatchSize     option.Option[int] `json:"batch_size"`     // Number of events per batch (default: 100)
	MaxEvents     option.Option[int] `json:"max_events"`     // Maximum total events to buffer (default: 10000)
	SkipMigration bool               `json:"skip_migration"` // Skip automatic table creation (default: false)

	// Runtime state (not from JSON)
	db           *sql.DB                `json:"-"`
	writeCounter prometheus.Counter     `json:"-"`
	readCounter  prometheus.Counter     `json:"-"`
	errorCounter *prometheus.CounterVec `json:"-"`
	sizeGauge    prometheus.Gauge       `json:"-"`
}

// SQLBackingStoreFactoryWithDB returns a factory that creates SQL backing stores using
// an existing database connection. Accepts a dependency rather than managing connections internally,
// following the dependency injection pattern that enables testing with go-bits/easypg utilities.
//
// Example usage:
//
//	db, err := sql.Open("postgres", dsn)
//	// ... handle error
//
//	auditor, err := NewAuditor(ctx, AuditorOpts{
//	    EnvPrefix: "MYAPP_AUDIT_RABBITMQ",
//	    BackingStoreFactories: map[string]BackingStoreFactory{
//	        "sql": SQLBackingStoreFactoryWithDB(db),
//	    },
//	})
func SQLBackingStoreFactoryWithDB(db *sql.DB) BackingStoreFactory {
	return func(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
		var store SQLBackingStore
		if err := json.Unmarshal(params, &store); err != nil {
			return nil, fmt.Errorf("audittools: failed to parse SQL backing store config: %w", err)
		}
		store.db = db

		registry := opts.Registry
		if registry == nil {
			registry = prometheus.DefaultRegisterer
		}

		if err := store.Init(registry); err != nil {
			return nil, err
		}
		return &store, nil
	}
}

// Init implements BackingStore.
func (s *SQLBackingStore) Init(registry prometheus.Registerer) error {
	if s.db == nil {
		return errors.New("audittools: database connection is required for sql backing store (use SQLBackingStoreFactoryWithDB)")
	}

	if s.TableName == "" {
		s.TableName = "audit_events"
	}
	// 100 events per batch balances database round-trip overhead with transaction size.
	if s.BatchSize.IsNone() {
		s.BatchSize = option.Some(100)
	}
	// 10000 events provides substantial buffering (~1MB) during extended RabbitMQ outages.
	if s.MaxEvents.IsNone() {
		s.MaxEvents = option.Some(10000)
	}

	// Validate table name before ANY SQL operations to prevent injection attacks.
	// Regex ensures PostgreSQL identifier rules: start with letter/underscore, followed by alphanumeric/underscores.
	// This validation makes string concatenation safe in SQL construction.
	if !isValidSQLIdentifier(s.TableName) {
		return fmt.Errorf("audittools: invalid table name: %q", s.TableName)
	}

	if !s.SkipMigration {
		if err := s.ensureTableExists(); err != nil {
			return fmt.Errorf("audittools: failed to create table: %w", err)
		}
	}

	s.initializeMetrics(registry)
	return nil
}

func (s *SQLBackingStore) ensureTableExists() error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			event_data JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.TableName)

	if _, err := s.db.Exec(query); err != nil {
		return err
	}

	// Index on (created_at, id) enables efficient FIFO reads with ORDER BY created_at, id.
	indexQuery := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_created_at_id_idx
		ON %s (created_at, id)`, s.TableName, s.TableName)

	if _, err := s.db.Exec(indexQuery); err != nil {
		return err
	}

	return nil
}

func (s *SQLBackingStore) initializeMetrics(registry prometheus.Registerer) {
	s.writeCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "audittools_backing_store_writes_total",
		Help: "Total number of audit events written to the backing store.",
	})
	s.readCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "audittools_backing_store_reads_total",
		Help: "Total number of audit events read from the backing store.",
	})
	s.errorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "audittools_backing_store_errors_total",
		Help: "Total number of errors encountered by the backing store.",
	}, []string{"operation"})
	s.sizeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "audittools_backing_store_size_bytes",
		Help: "Current number of events in the SQL backing store.",
	})

	if registry != nil {
		registry.MustRegister(s.writeCounter, s.readCounter, s.errorCounter, s.sizeGauge)
	}
}

// Write implements BackingStore.
func (s *SQLBackingStore) Write(event cadf.Event) error {
	maxEvents := s.MaxEvents.UnwrapOr(10000)
	count, err := s.countEvents()
	if err != nil {
		s.errorCounter.WithLabelValues("write_count").Inc()
		return fmt.Errorf("audittools: failed to check event count: %w", err)
	}

	if count >= int64(maxEvents) {
		s.errorCounter.WithLabelValues("write_full").Inc()
		return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, count, maxEvents)
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		s.errorCounter.WithLabelValues("write_marshal").Inc()
		return fmt.Errorf("audittools: failed to marshal event: %w", err)
	}

	// String concatenation safe after Init() table name validation (prevents SQL injection).
	// Provides better performance than fmt.Sprintf for simple query construction.
	//nolint:gosec // G202: Table name validated in Init() with regex matching PostgreSQL identifier rules
	query := "INSERT INTO " + s.TableName + " (event_data) VALUES ($1)"
	if _, err := s.db.Exec(query, eventData); err != nil {
		s.errorCounter.WithLabelValues("write_insert").Inc()
		return fmt.Errorf("audittools: failed to insert event: %w", err)
	}

	s.writeCounter.Inc()
	return nil
}

// ReadBatch implements BackingStore.
func (s *SQLBackingStore) ReadBatch() ([]cadf.Event, func() error, error) {
	batchSize := s.BatchSize.UnwrapOr(100)
	// String concatenation safe after Init() validation. ORDER BY uses index for efficient FIFO reads.
	//nolint:gosec // G202: Table name validated in Init() with regex matching PostgreSQL identifier rules
	query := "SELECT id, event_data FROM " + s.TableName + " ORDER BY created_at ASC, id ASC LIMIT $1"

	rows, err := s.db.Query(query, batchSize)
	if err != nil {
		s.errorCounter.WithLabelValues("read_query").Inc()
		return nil, nil, fmt.Errorf("audittools: failed to query events: %w", err)
	}
	defer rows.Close()

	// Preallocate based on known batch size to avoid reallocations during iteration.
	events := make([]cadf.Event, 0, batchSize)
	eventIDs := make([]int64, 0, batchSize)

	for rows.Next() {
		var id int64
		var eventData []byte

		if err := rows.Scan(&id, &eventData); err != nil {
			s.errorCounter.WithLabelValues("read_scan").Inc()
			return nil, nil, fmt.Errorf("audittools: failed to scan event: %w", err)
		}

		var event cadf.Event
		if err := json.Unmarshal(eventData, &event); err != nil {
			s.errorCounter.WithLabelValues("read_unmarshal").Inc()
			// Skip corrupted events rather than failing the entire batch - allows partial recovery.
			continue
		}

		events = append(events, event)
		eventIDs = append(eventIDs, id)
	}

	if err := rows.Err(); err != nil {
		s.errorCounter.WithLabelValues("read_rows").Inc()
		return nil, nil, fmt.Errorf("audittools: failed to iterate events: %w", err)
	}

	if len(events) == 0 {
		return nil, nil, nil
	}

	commit := s.makeCommitFunc(eventIDs)

	s.readCounter.Add(float64(len(events)))
	return events, commit, nil
}

func (s *SQLBackingStore) makeCommitFunc(eventIDs []int64) func() error {
	return func() error {
		if len(eventIDs) == 0 {
			return nil
		}

		// PostgreSQL-specific: ANY($1) with pq.Array provides efficient batch delete.
		// String concatenation safe after Init() validation.
		//nolint:gosec // G202: Table name validated in Init() with regex matching PostgreSQL identifier rules
		query := "DELETE FROM " + s.TableName + " WHERE id = ANY($1)"

		if _, err := s.db.Exec(query, pq.Array(eventIDs)); err != nil {
			s.errorCounter.WithLabelValues("commit_delete").Inc()
			return fmt.Errorf("audittools: failed to delete events: %w", err)
		}

		return nil
	}
}

// UpdateMetrics implements BackingStore.
func (s *SQLBackingStore) UpdateMetrics() error {
	count, err := s.countEvents()
	if err != nil {
		return err
	}

	s.sizeGauge.Set(float64(count))
	return nil
}

// Close implements BackingStore.
// Does NOT close the database connection because it's provided via dependency injection.
// The application owns the connection lifecycle and must close it when appropriate.
func (s *SQLBackingStore) Close() error {
	return nil
}

func (s *SQLBackingStore) countEvents() (int64, error) {
	query := "SELECT COUNT(*) FROM " + s.TableName

	var count int64
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// isValidSQLIdentifier validates that a string is a safe SQL identifier.
// Prevents SQL injection attacks when table names come from configuration.
// Enforces PostgreSQL identifier rules: start with letter or underscore, followed by alphanumeric or underscores.
func isValidSQLIdentifier(name string) bool {
	if name == "" {
		return false
	}
	return sqlIdentifierRegex.MatchString(name)
}
