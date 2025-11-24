// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"context"
	"net/url"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/logg"
)

type auditTrail struct {
	EventSink           <-chan cadf.Event
	OnSuccessfulPublish func()
	OnFailedPublish     func()
	BackingStore        BackingStore
}

// Commit implements the main event processing loop for the audit trail.
//
// Event Lifecycle States:
// 1. NEW: Just received from EventSink channel
// 2. BUFFERED: Stored in BackingStore (RabbitMQ down)
// 3. SENT: Published to RabbitMQ (terminal state)
//
// Flow Paths:
// - Normal operation: NEW → SENT
// - RabbitMQ down: NEW → BUFFERED → SENT
//
// The BackingStore can be either file-based (for persistent buffering) or
// in-memory (for services without persistent volumes). This replaces the
// old pendingEvents slice with a unified buffering mechanism.
//
// Flow Control:
// If BackingStore.Write() fails (backing store full), stop reading from EventSink
// to apply backpressure and prevent data loss.
//
// This function blocks the current goroutine forever. It should be invoked with the "go" keyword.
func (t auditTrail) Commit(ctx context.Context, rabbitmqURI url.URL, rabbitmqQueueName string) {
	rc, err := newRabbitConnection(rabbitmqURI, rabbitmqQueueName)
	if err != nil {
		logg.Error(err.Error())
	}

	sendEvent := func(e *cadf.Event) bool {
		rc = refreshConnectionIfClosedOrOld(rc, rabbitmqURI, rabbitmqQueueName)
		err := rc.PublishEvent(ctx, e)
		if err != nil {
			t.OnFailedPublish()
			logg.Error("audittools: failed to publish audit event with ID %q: %s", e.ID, err.Error())
			return false
		}
		t.OnSuccessfulPublish()
		return true
	}

	// Drain the backing store periodically
	drainTicker := time.NewTicker(1 * time.Minute)
	defer drainTicker.Stop()

	// Update metrics periodically
	metricsTicker := time.NewTicker(1 * time.Minute)
	defer metricsTicker.Stop()

	// Track if backing store is full to apply backpressure
	backingStoreFull := false

	for {
		// Flow control: If backing store is full, stop reading from EventSink.
		// This will cause the channel to fill up and eventually block Record().
		var channelToRead <-chan cadf.Event
		if !backingStoreFull {
			channelToRead = t.EventSink
		}

		select {
		case e := <-channelToRead:
			// Try to send immediately. If RabbitMQ is down, buffer the event.
			//
			// Note: Strict chronological ordering is not guaranteed. New events are sent
			// immediately if the connection is up, while old events from the backing store
			// are drained asynchronously.

			if !sendEvent(&e) {
				if err := t.BackingStore.Write(e); err != nil {
					logg.Error("audittools: failed to write to backing store: %s", err.Error())
					// Backing store is likely full. Apply backpressure to prevent data loss.
					backingStoreFull = true
				}
			}
		case <-metricsTicker.C:
			if err := t.BackingStore.UpdateMetrics(); err != nil {
				logg.Error("audittools: failed to update backing store metrics: %s", err.Error())
			}
		case <-drainTicker.C:
			// Drain backing store and resume reading from EventSink if successful
			drained := t.drainBackingStore(sendEvent)
			if drained && backingStoreFull {
				backingStoreFull = false
			}
		}
	}
}

func refreshConnectionIfClosedOrOld(rc *rabbitConnection, uri url.URL, queueName string) *rabbitConnection {
	if !rc.IsNilOrClosed() {
		if time.Since(rc.LastConnectedAt) < 5*time.Minute {
			return rc
		}
		rc.Disconnect()
	}

	connection, err := newRabbitConnection(uri, queueName)
	if err != nil {
		logg.Error(err.Error())
		return nil
	}

	return connection
}

// drainBackingStore attempts to drain all events from the backing store.
// Returns true if at least one batch was successfully drained, false otherwise.
func (t auditTrail) drainBackingStore(sendEvent func(*cadf.Event) bool) bool {
	// This function loops until the backing store is empty or sending fails.
	// It processes new events from EventSink during draining to avoid blocking.

	anyBatchDrained := false

	for {
		// Check for new events and write them to the backing store
		// This prevents blocking the main application during drain
		select {
		case e := <-t.EventSink:
			if err := t.BackingStore.Write(e); err != nil {
				logg.Error("audittools: failed to write to backing store during drain: %s", err.Error())
			}
		default:
			// No new events, continue draining
		}

		// Read and send one batch from backing store
		events, commit, err := t.BackingStore.ReadBatch()
		if err != nil {
			logg.Error("audittools: failed to read from backing store: %s", err.Error())
			return anyBatchDrained
		}

		if len(events) == 0 {
			// Empty batch - commit to clean up corrupted/empty files
			if commit != nil {
				if err := commit(); err != nil {
					logg.Error("audittools: failed to commit empty batch: %s", err.Error())
				}
			}
			return anyBatchDrained
		}

		// Send all events in the batch
		allSent := true
		for _, e := range events {
			// Check for new events between sends
			select {
			case newEvent := <-t.EventSink:
				if err := t.BackingStore.Write(newEvent); err != nil {
					logg.Error("audittools: failed to write to backing store during drain: %s", err.Error())
				}
			default:
			}

			if !sendEvent(&e) {
				allSent = false
				break
			}
		}

		if !allSent {
			// Sending failed, stop draining
			return anyBatchDrained
		}

		// Commit successful batch
		if err := commit(); err != nil {
			logg.Error("audittools: failed to commit to backing store: %s", err.Error())
		}

		anyBatchDrained = true
	}
}
