// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
)

// BackingStore holds audit events that could not be published to RabbitMQ yet.
// See the package documentation for how to configure one.
//
// All methods are called from the single goroutine that publishes events,
// so implementations do not need to be safe for concurrent use.
type BackingStore interface {
	// Write adds an event to the store.
	// If this fails, the event is kept in memory and retried later.
	// Until then, no new events are taken from Auditor.Record(), which will block once its small buffer is full.
	Write(event cadf.Event) error

	// ReadBatch returns the oldest events in the store without removing them.
	// If the store is empty, all return values are nil.
	//
	// The caller tries to publish the events in order, and then calls commit with the number of events that were published.
	// commit removes those events from the store. The remaining events will be returned again by a later ReadBatch.
	// commit is always called before the next call to ReadBatch.
	//
	// Stored entries that cannot be decoded are not included in events.
	// They are removed by commit once all events in the batch were published.
	// If a batch consists only of such entries, events is empty, but commit is not nil.
	ReadBatch() (events []cadf.Event, commit func(published int) error, err error)

	// UpdateMetrics updates Prometheus metrics describing the state of the backing store (e.g. size, file count), if there are any.
	// It is called once per minute.
	UpdateMetrics() error
}

// BackingStoreFactory builds a BackingStore from the "params" section of its configuration (see AuditorOpts.BackingStoreFactories).
// If the BackingStore has its own Prometheus metrics, it shall register them with opts.Registry,
// or with the default registry if opts.Registry is nil.
type BackingStoreFactory func(params json.RawMessage, opts AuditorOpts) (BackingStore, error)

// errBackingStoreFull is returned by BackingStore.Write when the store has reached its configured size limit.
var errBackingStoreFull = errors.New("audittools: backing store is full")

// unmarshalJSONStrict is like json.Unmarshal, but rejects unknown fields to catch typos in the configuration.
func unmarshalJSONStrict(buf []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

// backingStoreMetrics are the metrics that all BackingStore implementations in this package have in common.
type backingStoreMetrics struct {
	Writes prometheus.Counter
	Reads  prometheus.Counter
	Errors *prometheus.CounterVec
}

// newBackingStoreMetrics builds and registers the common metrics, as well as any additional metrics given by the caller.
func newBackingStoreMetrics(registry prometheus.Registerer, additional ...prometheus.Collector) backingStoreMetrics {
	m := backingStoreMetrics{
		Writes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "audittools_backing_store_writes_total",
			Help: "Number of audit events written to the backing store.",
		}),
		Reads: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "audittools_backing_store_reads_total",
			Help: "Number of audit events that were published from the backing store and removed from it.",
		}),
		Errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "audittools_backing_store_errors_total",
			Help: `Number of failed backing store operations. The "read" operation also counts stored entries that cannot be decoded.`,
		}, []string{"operation"}),
	}
	for _, op := range []string{"write", "read", "commit"} {
		m.Errors.WithLabelValues(op).Add(0)
	}

	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}
	registry.MustRegister(append([]prometheus.Collector{m.Writes, m.Reads, m.Errors}, additional...)...)
	return m
}
