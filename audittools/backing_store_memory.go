// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

func init() {
	// Self-register with the backing store registry
	BackingStoreRegistry.Add(func() BackingStore {
		return &InMemoryBackingStore{}
	})
}

// InMemoryBackingStore implements BackingStore using an in-memory slice.
//
// Thread safety: Write() and ReadBatch() are serialized by a mutex.
// Multiple concurrent calls are safe but will block each other.
// This implementation is suitable for services without persistent volumes
// that need temporary buffering during RabbitMQ unavailability.
type InMemoryBackingStore struct {
	// Configuration (JSON params)
	MaxEvents int `json:"max_events"`

	// Runtime state (not from JSON)
	mu     sync.Mutex   `json:"-"`
	events []cadf.Event `json:"-"`

	// Metrics (initialized in Init)
	writeCounter prometheus.Counter `json:"-"`
	readCounter  prometheus.Counter `json:"-"`
	sizeGauge    prometheus.Gauge   `json:"-"`
}

// PluginTypeID implements pluggable.Plugin.
func (s *InMemoryBackingStore) PluginTypeID() string {
	return "memory"
}

// Init implements BackingStore.
func (s *InMemoryBackingStore) Init(registry prometheus.Registerer) error {
	// Set defaults
	if s.MaxEvents == 0 {
		s.MaxEvents = 1000
	}

	// Initialize metrics
	s.initializeMetrics(registry)

	return nil
}

// initializeMetrics creates and registers Prometheus metrics.
func (s *InMemoryBackingStore) initializeMetrics(registry prometheus.Registerer) {
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
func (s *InMemoryBackingStore) Write(event cadf.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check capacity limit
	if len(s.events) >= s.MaxEvents {
		return fmt.Errorf("%w: current size %d exceeds limit %d", ErrBackingStoreFull, len(s.events), s.MaxEvents)
	}

	s.events = append(s.events, event)
	s.writeCounter.Inc()
	return nil
}

// ReadBatch implements BackingStore.
func (s *InMemoryBackingStore) ReadBatch() ([]cadf.Event, func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.events) == 0 {
		return nil, nil, nil
	}

	// Return a copy of all events
	eventsCopy := make([]cadf.Event, len(s.events))
	copy(eventsCopy, s.events)

	commit := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Clear all events
		s.events = nil
		return nil
	}

	s.readCounter.Add(float64(len(eventsCopy)))
	return eventsCopy, commit, nil
}

// UpdateMetrics implements BackingStore.
func (s *InMemoryBackingStore) UpdateMetrics() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sizeGauge.Set(float64(len(s.events)))
	return nil
}

// Close implements BackingStore.
func (s *InMemoryBackingStore) Close() error {
	return nil
}
