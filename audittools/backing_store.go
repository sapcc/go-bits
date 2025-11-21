// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/pluggable"
)

// BackingStore is an interface for buffering audit events when the primary sink (RabbitMQ) is unavailable.
//
// Implementations must be pluggable and support initialization via the Init method.
// Thread safety requirements vary by implementation - see specific implementation documentation.
type BackingStore interface {
	pluggable.Plugin

	// Init initializes the backing store with the given Prometheus registry.
	// This is called after the store is instantiated and configuration is unmarshaled.
	Init(prometheus.Registerer) error

	// Write persists an event to the store.
	Write(event cadf.Event) error

	// ReadBatch reads the next batch of events from the store.
	// It returns the events and a commit function. The commit function should be called
	// when the events have been successfully processed to remove them from the store.
	// If no events are available, returns (nil, nil, nil).
	ReadBatch() (events []cadf.Event, commit func() error, err error)

	// UpdateMetrics updates the backing store metrics (e.g. size, file count).
	// This may be called periodically and should be efficient.
	UpdateMetrics() error

	// Close cleans up any resources used by the store.
	Close() error
}

// BackingStoreRegistry is the global registry for backing store implementations.
// Implementations register themselves via init() functions.
var BackingStoreRegistry pluggable.Registry[BackingStore]

// NewBackingStore creates a new backing store from a JSON configuration string.
// The JSON format is:
//
//	{"type":"<plugin_type_id>","params":{<plugin-specific-params>}}
//
// Example for file-based storage:
//
//	{"type":"file","params":{"directory":"/var/cache/audit","max_file_size":10485760,"max_total_size":1073741824}}
//
// Example for in-memory storage:
//
//	{"type":"memory","params":{"max_events":50}}
func NewBackingStore(configJSON string, registry prometheus.Registerer) (BackingStore, error) {
	// Parse config JSON
	var cfg struct {
		PluginTypeID string          `json:"type"`
		Params       json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("audittools: failed to unmarshal backing store config: %w", err)
	}

	// Default empty params if not provided
	if len(cfg.Params) == 0 {
		cfg.Params = json.RawMessage("{}")
	}

	// Instantiate plugin by type ID
	store := BackingStoreRegistry.Instantiate(cfg.PluginTypeID)
	if store == nil {
		return nil, fmt.Errorf("audittools: unknown backing store type: %q", cfg.PluginTypeID)
	}

	// Unmarshal params into store struct
	if err := json.Unmarshal(cfg.Params, store); err != nil {
		return nil, fmt.Errorf("audittools: failed to unmarshal backing store params: %w", err)
	}

	if err := store.Init(registry); err != nil {
		return nil, fmt.Errorf("audittools: failed to initialize backing store: %w", err)
	}

	return store, nil
}

// ErrBackingStoreFull is returned by Write() when a backing store has reached its maximum configured size.
var ErrBackingStoreFull = errors.New("audittools: backing store full")
