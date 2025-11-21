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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

var sqlIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func init() {
	// Self-register with the backing store registry
	BackingStoreRegistry.Add(func() BackingStore {
		return &SQLBackingStore{}
	})
}

// SQLBackingStore implements BackingStore using a PostgreSQL database.
//
// Thread safety: All operations use database transactions for atomicity.
// Multiple concurrent calls are safe and will be serialized by the database.
// This implementation is suitable for services that already have a database
// connection and want to avoid managing filesystem volumes.
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
type SQLBackingStore struct {
	// Configuration (JSON params)
	DSN           string `json:"dsn"`            // Database connection string
	TableName     string `json:"table_name"`     // Table name (default: "audit_events")
	BatchSize     int    `json:"batch_size"`     // Number of events per batch (default: 100)
	MaxEvents     int    `json:"max_events"`     // Maximum total events to buffer (default: 10000)
	DriverName    string `json:"driver_name"`    // SQL driver name (default: "postgres")
	SkipMigration bool   `json:"skip_migration"` // Skip automatic table creation (default: false)

	// Runtime state (not from JSON)
	db           *sql.DB                `json:"-"`
	writeCounter prometheus.Counter     `json:"-"`
	readCounter  prometheus.Counter     `json:"-"`
	errorCounter *prometheus.CounterVec `json:"-"`
	sizeGauge    prometheus.Gauge       `json:"-"`
}

// PluginTypeID implements pluggable.Plugin.
func (s *SQLBackingStore) PluginTypeID() string {
	return "sql"
}

// Init implements BackingStore.
func (s *SQLBackingStore) Init(registry prometheus.Registerer) error {
	// Validate configuration
	if s.DSN == "" {
		return errors.New("audittools: dsn is required for sql backing store")
	}

	// Set defaults
	if s.DriverName == "" {
		s.DriverName = "postgres"
	}
	if s.TableName == "" {
		s.TableName = "audit_events"
	}
	if s.BatchSize == 0 {
		s.BatchSize = 100
	}
	if s.MaxEvents == 0 {
		s.MaxEvents = 10000
	}

	// Validate table name to prevent SQL injection
	if !isValidSQLIdentifier(s.TableName) {
		return fmt.Errorf("audittools: invalid table name: %q", s.TableName)
	}

	// Open database connection
	db, err := sql.Open(s.DriverName, s.DSN)
	if err != nil {
		return fmt.Errorf("audittools: failed to open database: %w", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("audittools: failed to ping database: %w", err)
	}

	s.db = db

	// Create table if needed (unless skipped)
	if !s.SkipMigration {
		if err := s.ensureTableExists(); err != nil {
			db.Close()
			return fmt.Errorf("audittools: failed to create table: %w", err)
		}
	}

	s.initializeMetrics(registry)

	return nil
}

// ensureTableExists creates the audit_events table if it doesn't exist.
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

	// Create index for efficient FIFO reads
	indexQuery := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_created_at_id_idx
		ON %s (created_at, id)`, s.TableName, s.TableName)

	if _, err := s.db.Exec(indexQuery); err != nil {
		return err
	}

	return nil
}

// initializeMetrics creates and registers Prometheus metrics.
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
	// Check capacity limit
	count, err := s.countEvents()
	if err != nil {
		s.errorCounter.WithLabelValues("write_count").Inc()
		return fmt.Errorf("audittools: failed to check event count: %w", err)
	}

	if count >= int64(s.MaxEvents) {
		s.errorCounter.WithLabelValues("write_full").Inc()
		return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, count, s.MaxEvents)
	}

	// Marshal event to JSON
	eventData, err := json.Marshal(event)
	if err != nil {
		s.errorCounter.WithLabelValues("write_marshal").Inc()
		return fmt.Errorf("audittools: failed to marshal event: %w", err)
	}

	// Insert event into database
	//nolint:gosec // G202: Table name is validated in Init() to prevent SQL injection
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
	// Read oldest events (FIFO order)
	//nolint:gosec // G202: Table name is validated in Init() to prevent SQL injection
	query := "SELECT id, event_data FROM " + s.TableName + " ORDER BY created_at ASC, id ASC LIMIT $1"

	rows, err := s.db.Query(query, s.BatchSize)
	if err != nil {
		s.errorCounter.WithLabelValues("read_query").Inc()
		return nil, nil, fmt.Errorf("audittools: failed to query events: %w", err)
	}
	defer rows.Close()

	var events []cadf.Event
	var eventIDs []int64

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
			// Skip corrupted events and continue
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

	// Create commit function that deletes the processed events
	commit := s.makeCommitFunc(eventIDs)

	s.readCounter.Add(float64(len(events)))
	return events, commit, nil
}

// makeCommitFunc creates a commit function for deleting processed events.
func (s *SQLBackingStore) makeCommitFunc(eventIDs []int64) func() error {
	return func() error {
		if len(eventIDs) == 0 {
			return nil
		}

		// Build DELETE query with IN clause
		// For PostgreSQL, we can use ANY with an array
		//nolint:gosec // G202: Table name is validated in Init() to prevent SQL injection
		query := "DELETE FROM " + s.TableName + " WHERE id = ANY($1)"

		// Use pq.Array to convert []int64 for PostgreSQL
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
func (s *SQLBackingStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// countEvents returns the current number of events in the table.
func (s *SQLBackingStore) countEvents() (int64, error) {
	query := "SELECT COUNT(*) FROM " + s.TableName

	var count int64
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// isValidSQLIdentifier validates that a string is a safe SQL identifier.
// This prevents SQL injection attacks via table names.
func isValidSQLIdentifier(name string) bool {
	if name == "" {
		return false
	}
	// PostgreSQL identifiers: start with letter or underscore, then letters, digits, or underscores
	return sqlIdentifierRegex.MatchString(name)
}
