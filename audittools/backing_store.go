// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"encoding/json"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

// BackingStore is an interface for buffering audit events when the primary sink (RabbitMQ) is unavailable.
// This prevents audit data loss during RabbitMQ outages or network disruptions.
//
// Thread safety requirements vary by implementation - see specific implementation documentation.
type BackingStore interface {
	// Init initializes the backing store.
	// If the implementation wants to define its own Prometheus metrics, it shall use the provided registry.
	Init(prometheus.Registerer) error

	// Write persists an event to the store.
	Write(event cadf.Event) error

	// ReadBatch reads the next batch of events from the store.
	// Returns events and a commit function. The commit function removes events from the store
	// only after successful processing, preventing data loss if processing fails.
	// Returns (nil, nil, nil) if no events are available.
	ReadBatch() (events []cadf.Event, commit func() error, err error)

	// UpdateMetrics updates the backing store metrics (e.g. size, file count).
	// Must be efficient for periodic calls (typically every few seconds).
	UpdateMetrics() error

	// Close cleans up any resources used by the store.
	Close() error
}

// BackingStoreFactory is a function that creates a backing store from JSON parameters.
// Receives the entire AuditorOpts to enable dependency injection - applications can provide
// their own database connections, Prometheus registries, and configuration without creating
// duplicate resource pools.
type BackingStoreFactory func(params json.RawMessage, opts AuditorOpts) (BackingStore, error)

// ErrBackingStoreFull is returned by Write() when a backing store has reached its maximum configured size.
// Callers can distinguish this error from transient failures to implement backpressure strategies.
var ErrBackingStoreFull = errors.New("audittools: backing store full")
