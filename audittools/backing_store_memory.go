// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/majewsky/gg/option"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

// NewInMemoryBackingStore creates an in-memory backing store from JSON parameters.
// This is the factory function for use in AuditorOpts.BackingStoreFactories.
//
// Example usage:
//
//	auditor, err := NewAuditor(ctx, AuditorOpts{
//	    BackingStoreFactories: map[string]BackingStoreFactory{
//	        "memory": NewInMemoryBackingStore,
//	    },
//	})
func NewInMemoryBackingStore(params json.RawMessage, opts AuditorOpts) (BackingStore, error) {
	var store inMemoryBackingStore
	if err := json.Unmarshal(params, &store); err != nil {
		return nil, fmt.Errorf("audittools: failed to parse memory backing store config: %w", err)
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

// inMemoryBackingStore implements BackingStore using an in-memory slice.
// Suitable for services without persistent volumes that need temporary buffering during RabbitMQ unavailability.
// Data is lost on process restart, but provides zero-configuration buffering for transient outages.
//
// Thread safety: Write() and ReadBatch() are serialized by a mutex.
// Multiple concurrent calls are safe but will block each other.
type inMemoryBackingStore struct {
	// Configuration (JSON params)
	MaxEvents option.Option[int] `json:"max_events"`

	// Runtime state (not from JSON)
	mu     sync.Mutex   `json:"-"`
	events []cadf.Event `json:"-"`

	// Metrics (initialized in Init)
	writeCounter prometheus.Counter `json:"-"`
	readCounter  prometheus.Counter `json:"-"`
	sizeGauge    prometheus.Gauge   `json:"-"`
}

// Init implements BackingStore.
func (s *inMemoryBackingStore) Init(registry prometheus.Registerer) error {
	s.initializeMetrics(registry)
	return s.UpdateMetrics()
}

func (s *inMemoryBackingStore) initializeMetrics(registry prometheus.Registerer) {
	s.writeCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "audittools_backing_store_writes_total",
		Help: "Total number of audit events written to the backing store.",
	})
	s.readCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "audittools_backing_store_reads_total",
		Help: "Total number of audit events read from the backing store.",
	})
	s.sizeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "audittools_backing_store_size_bytes",
		Help: "Current number of events in the in-memory backing store.",
	})

	if registry != nil {
		registry.MustRegister(s.writeCounter, s.readCounter, s.sizeGauge)
	}
}

// Write implements BackingStore.
func (s *inMemoryBackingStore) Write(event cadf.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxEvents := s.MaxEvents.UnwrapOr(1000)
	if len(s.events) >= maxEvents {
		return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, len(s.events), maxEvents)
	}

	s.events = append(s.events, event)
	s.writeCounter.Inc()
	return nil
}

// ReadBatch implements BackingStore.
func (s *inMemoryBackingStore) ReadBatch() ([]cadf.Event, func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.events) == 0 {
		return nil, nil, nil
	}

	// Copy events to prevent caller from mutating internal state.
	eventsCopy := make([]cadf.Event, len(s.events))
	copy(eventsCopy, s.events)

	// Capture the count of events being returned. The commit function will
	// remove only these events from the front of the slice, preserving any
	// events written between this ReadBatch() call and the commit() call.
	count := len(s.events)
	commit := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Remove only the events that were part of this batch.
		// Events appended by concurrent Write() calls are preserved.
		s.events = append([]cadf.Event(nil), s.events[count:]...)
		s.readCounter.Add(float64(count))
		return nil
	}

	return eventsCopy, commit, nil
}

// UpdateMetrics implements BackingStore.
func (s *inMemoryBackingStore) UpdateMetrics() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sizeGauge.Set(float64(len(s.events)))
	return nil
}

// Close implements BackingStore.
func (s *inMemoryBackingStore) Close() error {
	return nil
}
