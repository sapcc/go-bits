// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"encoding/json"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

// NewInMemoryBackingStore is a BackingStoreFactory for a BackingStore that keeps events in memory.
// Events in it are lost when the process exits.
// This is the backing store that NewAuditor uses when no other backing store is configured.
// Params (all optional):
//
//	{
//	  "max_events": 1000  // default: 0 (unlimited)
//	}
//
// If max_events is set and the store is full, Auditor.Record() blocks until RabbitMQ is reachable again.
func NewInMemoryBackingStore(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
	var cfg struct {
		MaxEvents int `json:"max_events"`
	}
	err := unmarshalJSONStrict(params, &cfg)
	if err != nil {
		return nil, fmt.Errorf("audittools: cannot parse params for memory backing store: %w", err)
	}
	if cfg.MaxEvents < 0 {
		return nil, fmt.Errorf("audittools: invalid max_events for memory backing store: %d", cfg.MaxEvents)
	}

	s := &inMemoryBackingStore{
		maxEvents: cfg.MaxEvents,
		eventsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "audittools_backing_store_events",
			Help: "Number of audit events in the backing store.",
		}),
	}
	s.metrics = newBackingStoreMetrics(opts.Registry, s.eventsGauge)
	return s, nil
}

// inMemoryBackingStore is the BackingStore built by NewInMemoryBackingStore.
type inMemoryBackingStore struct {
	maxEvents   int
	events      []cadf.Event
	metrics     backingStoreMetrics
	eventsGauge prometheus.Gauge
}

// Write implements the BackingStore interface.
func (s *inMemoryBackingStore) Write(event cadf.Event) error {
	if s.maxEvents > 0 && len(s.events) >= s.maxEvents {
		s.metrics.Errors.WithLabelValues("write").Inc()
		return fmt.Errorf("%w (max_events = %d)", errBackingStoreFull, s.maxEvents)
	}
	s.events = append(s.events, event)
	s.metrics.Writes.Inc()
	return nil
}

// ReadBatch implements the BackingStore interface.
func (s *inMemoryBackingStore) ReadBatch() ([]cadf.Event, func(int) error, error) {
	count := len(s.events)
	if count == 0 {
		return nil, nil, nil
	}
	commit := func(published int) error {
		s.events = s.events[published:]
		if len(s.events) == 0 {
			s.events = nil // release the backing array
		}
		s.metrics.Reads.Add(float64(published))
		return nil
	}
	return s.events[:count:count], commit, nil
}

// UpdateMetrics implements the BackingStore interface.
func (s *inMemoryBackingStore) UpdateMetrics() error {
	s.eventsGauge.Set(float64(len(s.events)))
	return nil
}
