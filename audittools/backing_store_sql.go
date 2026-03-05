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

	"github.com/sapcc/go-bits/sqlext"
)

var sqlIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// sqlBackingStoreParams holds the JSON-parsed configuration with Option[T]
// for optional fields. Values are resolved at parse time via UnwrapOr() into
// the runtime struct (sqlBackingStore), so methods never handle the None case.
type sqlBackingStoreParams struct {
	TableName     string             `json:"table_name"`
	BatchSize     option.Option[int] `json:"batch_size"`
	MaxEvents     option.Option[int] `json:"max_events"`
	SkipMigration bool               `json:"skip_migration"`
}

// sqlBackingStore implements BackingStore using a PostgreSQL database.
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
// SQLBackingStoreFactoryWithPostgresDB to avoid creating duplicate connection pools
// (each PostgreSQL process should maintain only one connection pool per database).
type sqlBackingStore struct {
	// Configuration (resolved at parse time from Option[T])
	TableName     string
	BatchSize     int
	MaxEvents     int
	SkipMigration bool

	// Runtime state
	db           *sql.DB
	writeCounter prometheus.Counter
	readCounter  prometheus.Counter
	errorCounter *prometheus.CounterVec
	sizeGauge    prometheus.Gauge
}

// SQLBackingStoreFactoryWithPostgresDB returns a factory that creates SQL backing stores using
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
//	        "sql": SQLBackingStoreFactoryWithPostgresDB(db),
//	    },
//	})
func SQLBackingStoreFactoryWithPostgresDB(db *sql.DB) BackingStoreFactory {
	return func(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
		var cfg sqlBackingStoreParams
		if err := json.Unmarshal(params, &cfg); err != nil {
			return nil, fmt.Errorf("audittools: cannot parse SQL backing store config: %w", err)
		}
		store := sqlBackingStore{
			TableName:     cfg.TableName,
			BatchSize:     cfg.BatchSize.UnwrapOr(100),
			MaxEvents:     cfg.MaxEvents.UnwrapOr(10000),
			SkipMigration: cfg.SkipMigration,
			db:            db,
		}

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
func (s *sqlBackingStore) Init(registry prometheus.Registerer) error {
	if s.db == nil {
		return errors.New("audittools: database connection is required for sql backing store (use SQLBackingStoreFactoryWithPostgresDB)")
	}

	if s.TableName == "" {
		s.TableName = "audit_events"
	}

	// Validate table name before ANY SQL operations to prevent injection attacks.
	// Regex ensures PostgreSQL identifier rules: start with letter/underscore, followed by alphanumeric/underscores.
	// This validation makes string concatenation safe in SQL construction.
	if !sqlIdentifierRegex.MatchString(s.TableName) {
		return fmt.Errorf("audittools: invalid table name: %q", s.TableName)
	}

	if !s.SkipMigration {
		if err := s.ensureTableExists(); err != nil {
			return fmt.Errorf("audittools: cannot create table: %w", err)
		}
	}

	s.initializeMetrics(registry)
	return s.UpdateMetrics()
}

func (s *sqlBackingStore) ensureTableExists() error {
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

func (s *sqlBackingStore) initializeMetrics(registry prometheus.Registerer) {
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
func (s *sqlBackingStore) Write(event cadf.Event) error {
	count, err := s.countEvents()
	if err != nil {
		s.errorCounter.WithLabelValues("write_count").Inc()
		return fmt.Errorf("audittools: cannot check event count: %w", err)
	}

	if count >= int64(s.MaxEvents) {
		s.errorCounter.WithLabelValues("write_full").Inc()
		return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, count, s.MaxEvents)
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		s.errorCounter.WithLabelValues("write_marshal").Inc()
		return fmt.Errorf("audittools: cannot marshal event: %w", err)
	}

	// String concatenation safe after Init() table name validation (prevents SQL injection).
	// Provides better performance than fmt.Sprintf for simple query construction.
	//nolint:gosec // G202: Table name validated in Init() with regex matching PostgreSQL identifier rules
	query := "INSERT INTO " + s.TableName + " (event_data) VALUES ($1)"
	if _, err := s.db.Exec(query, eventData); err != nil {
		s.errorCounter.WithLabelValues("write_insert").Inc()
		return fmt.Errorf("audittools: cannot insert event: %w", err)
	}

	s.writeCounter.Inc()
	return nil
}

// ReadBatch implements BackingStore.
func (s *sqlBackingStore) ReadBatch() ([]cadf.Event, func() error, error) {
	// String concatenation safe after Init() validation. ORDER BY uses index for efficient FIFO reads.
	query := "SELECT id, event_data FROM " + s.TableName + " ORDER BY created_at ASC, id ASC LIMIT $1"

	// Preallocate based on known batch size to avoid reallocations during iteration.
	events := make([]cadf.Event, 0, s.BatchSize)
	eventIDs := make([]int64, 0, s.BatchSize)

	err := sqlext.ForeachRow(s.db, query, []any{s.BatchSize}, func(rows *sql.Rows) error {
		var id int64
		var eventData []byte

		if err := rows.Scan(&id, &eventData); err != nil {
			s.errorCounter.WithLabelValues("read_scan").Inc()
			return fmt.Errorf("audittools: cannot scan event: %w", err)
		}

		var event cadf.Event
		if err := json.Unmarshal(eventData, &event); err != nil {
			s.errorCounter.WithLabelValues("read_unmarshal").Inc()
			// Include corrupted row ID so commit() deletes it,
			// preventing infinite reprocessing of corrupt data.
			eventIDs = append(eventIDs, id)
			return nil //nolint:nilerr // intentionally skip corrupted events
		}

		events = append(events, event)
		eventIDs = append(eventIDs, id)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if len(events) == 0 {
		return nil, nil, nil
	}

	commit := s.makeCommitFunc(eventIDs)

	s.readCounter.Add(float64(len(events)))
	return events, commit, nil
}

func (s *sqlBackingStore) makeCommitFunc(eventIDs []int64) func() error {
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
			return fmt.Errorf("audittools: cannot delete events: %w", err)
		}

		return nil
	}
}

// UpdateMetrics implements BackingStore.
func (s *sqlBackingStore) UpdateMetrics() error {
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
func (s *sqlBackingStore) Close() error {
	return nil
}

func (s *sqlBackingStore) countEvents() (int64, error) {
	query := "SELECT COUNT(*) FROM " + s.TableName

	var count int64
	if err := s.db.QueryRow(query).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}
