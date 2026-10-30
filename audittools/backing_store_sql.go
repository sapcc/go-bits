// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/option"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"
)

var sqlIdentifierRx = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// SQLBackingStoreFactoryWithPostgresDB returns a BackingStoreFactory for a BackingStore that keeps events
// in a table in the given PostgreSQL database. Params (all optional):
//
//	{
//	  "table_name": "audit_events",
//	  "batch_size": 100,  // number of events read and published in one transaction
//	  "max_events": 0     // default: 0 (unlimited)
//	}
//
// The application must create the table (see "SQL backing store" in the package documentation).
// If max_events is set and reached, or if the database is unreachable,
// Auditor.Record() blocks until RabbitMQ or the database is reachable again.
func SQLBackingStoreFactoryWithPostgresDB(db *sql.DB) BackingStoreFactory {
	return func(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
		var cfg struct {
			TableName option.Option[string] `json:"table_name"`
			BatchSize option.Option[int]    `json:"batch_size"`
			MaxEvents option.Option[int]    `json:"max_events"`
		}
		err := unmarshalJSONStrict(params, &cfg)
		if err != nil {
			return nil, fmt.Errorf("audittools: cannot parse params for SQL backing store: %w", err)
		}

		if db == nil {
			return nil, errors.New("audittools: SQL backing store requires a database connection")
		}
		s := &sqlBackingStore{
			db:        db,
			tableName: cfg.TableName.UnwrapOr("audit_events"),
			batchSize: cfg.BatchSize.UnwrapOr(100),
			maxEvents: cfg.MaxEvents.UnwrapOr(0),
			eventsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
				Name: "audittools_backing_store_events",
				Help: "Number of audit events in the backing store.",
			}),
		}
		// the table name is concatenated into queries below, so it must be a plain identifier
		if !sqlIdentifierRx.MatchString(s.tableName) {
			return nil, fmt.Errorf("audittools: invalid table name for SQL backing store: %q", s.tableName)
		}
		if s.batchSize <= 0 {
			return nil, fmt.Errorf("audittools: invalid batch_size for SQL backing store: %d", s.batchSize)
		}

		if s.maxEvents < 0 {
			return nil, fmt.Errorf("audittools: invalid max_events for SQL backing store: %d", s.maxEvents)
		}

		query := `SELECT id, event_data FROM ` + s.tableName + ` LIMIT 0` //nolint:gosec // table name was validated above
		_, err = db.Exec(query)
		if err != nil {
			return nil, fmt.Errorf("audittools: cannot use table %s for SQL backing store (see the package documentation for how to create it): %w", s.tableName, err)
		}

		s.metrics = newBackingStoreMetrics(opts.Registry, s.eventsGauge)
		return s, nil
	}
}

// sqlBackingStore is the BackingStore built by SQLBackingStoreFactoryWithPostgresDB.
type sqlBackingStore struct {
	db          *sql.DB
	tableName   string
	batchSize   int
	maxEvents   int
	metrics     backingStoreMetrics
	eventsGauge prometheus.Gauge
}

// Write implements the BackingStore interface.
func (s *sqlBackingStore) Write(event cadf.Event) error {
	err := s.write(event)
	if err != nil {
		s.metrics.Errors.WithLabelValues("write").Inc()
		return err
	}
	s.metrics.Writes.Inc()
	return nil
}

func (s *sqlBackingStore) write(event cadf.Event) error {
	// The count and the insert are not atomic, so with several writers, max_events can be exceeded slightly.
	if s.maxEvents > 0 {
		count, err := s.countEvents()
		if err != nil {
			return err
		}
		if count >= int64(s.maxEvents) {
			return fmt.Errorf("%w (max_events = %d)", errBackingStoreFull, s.maxEvents)
		}
	}

	buf, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("audittools: cannot serialize event for SQL backing store: %w", err)
	}
	query := `INSERT INTO ` + s.tableName + ` (event_data) VALUES ($1)` //nolint:gosec // table name was validated in the factory
	_, err = s.db.Exec(query, buf)
	if err != nil {
		return fmt.Errorf("audittools: cannot insert into %s: %w", s.tableName, err)
	}
	return nil
}

// ReadBatch implements the BackingStore interface.
//
// The rows are locked until commit is called, so other processes sharing the same table will skip them.
func (s *sqlBackingStore) ReadBatch() (events []cadf.Event, commit func(int) error, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		s.metrics.Errors.WithLabelValues("read").Inc()
		return nil, nil, fmt.Errorf("audittools: cannot begin transaction for SQL backing store: %w", err)
	}
	defer func() {
		if commit == nil {
			rollback(tx)
		}
	}()

	var (
		eventIDs   []int64 // IDs of the rows in `events`, in the same order
		corruptIDs []int64 // IDs of rows that cannot be decoded
	)
	query := `SELECT id, event_data FROM ` + s.tableName + ` ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`
	err = sqlext.ForeachRow(tx, query, []any{s.batchSize}, func(rows *sql.Rows) error {
		var (
			id  int64
			buf []byte
		)
		err := rows.Scan(&id, &buf)
		if err != nil {
			return err
		}
		var event cadf.Event
		if json.Unmarshal(buf, &event) == nil {
			events = append(events, event)
			eventIDs = append(eventIDs, id)
		} else {
			corruptIDs = append(corruptIDs, id)
		}
		return nil
	})
	if err != nil {
		s.metrics.Errors.WithLabelValues("read").Inc()
		return nil, nil, fmt.Errorf("audittools: cannot read from %s: %w", s.tableName, err)
	}
	if len(eventIDs) == 0 && len(corruptIDs) == 0 {
		return nil, nil, nil
	}

	commit = func(published int) error {
		idsToDelete := eventIDs[:published:published]
		if published == len(events) {
			idsToDelete = append(idsToDelete, corruptIDs...)
		}
		err := s.deleteAndCommit(tx, idsToDelete)
		if err != nil {
			s.metrics.Errors.WithLabelValues("commit").Inc()
			return err
		}
		s.metrics.Reads.Add(float64(published))
		if published == len(events) && len(corruptIDs) > 0 {
			logg.Error("audittools: deleted %d rows from %s that could not be decoded as audit events", len(corruptIDs), s.tableName)
			s.metrics.Errors.WithLabelValues("read").Add(float64(len(corruptIDs)))
		}
		return nil
	}
	return events, commit, nil
}

func (s *sqlBackingStore) deleteAndCommit(tx *sql.Tx, ids []int64) error {
	if len(ids) > 0 {
		placeholders := make([]string, len(ids))
		args := make([]any, len(ids))
		for idx, id := range ids {
			placeholders[idx] = "$" + strconv.Itoa(idx+1)
			args[idx] = id
		}
		query := `DELETE FROM ` + s.tableName + ` WHERE id IN (` + strings.Join(placeholders, ",") + `)` //nolint:gosec // table name was validated in the factory
		_, err := tx.Exec(query, args...)
		if err != nil {
			rollback(tx)
			return fmt.Errorf("audittools: cannot delete from %s: %w", s.tableName, err)
		}
	}
	err := tx.Commit()
	if err != nil {
		return fmt.Errorf("audittools: cannot commit transaction for SQL backing store: %w", err)
	}
	return nil
}

// UpdateMetrics implements the BackingStore interface.
func (s *sqlBackingStore) UpdateMetrics() error {
	count, err := s.countEvents()
	if err != nil {
		return err
	}
	s.eventsGauge.Set(float64(count))
	return nil
}

func (s *sqlBackingStore) countEvents() (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + s.tableName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("audittools: cannot count rows in %s: %w", s.tableName, err)
	}
	return count, nil
}

// rollback is like sqlext.RollbackUnlessCommitted, but does not log anything on success.
// (Rolling back is the normal outcome of ReadBatch when the table is empty, and this happens once per minute.)
func rollback(tx *sql.Tx) {
	err := tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		logg.Error("audittools: cannot roll back transaction for SQL backing store: %s", err.Error())
	}
}
