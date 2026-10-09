// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/option"

	"github.com/sapcc/go-bits/logg"
)

type auditTrail struct {
	EventSink           <-chan cadf.Event
	OnSuccessfulPublish func()
	OnFailedPublish     func()
	BackingStore        BackingStore
}

// Commit takes a AuditTrail that receives audit events from an event sink and publishes them to
// a specific RabbitMQ Connection using the specified amqp URI and queue name.
// The OnSuccessfulPublish and OnFailedPublish closures are executed as per their respective case.
// Events that cannot be published are put in the BackingStore, which is drained once per minute.
//
// This function blocks the current goroutine forever. It should be invoked with the "go" keyword.
func (t auditTrail) Commit(ctx context.Context, rabbitmqURI url.URL, rabbitmqQueueName string) {
	lastConnectAttempt := time.Now()
	rc, err := newRabbitConnection(rabbitmqURI, rabbitmqQueueName)
	if err != nil {
		logg.Error(err.Error())
	}

	sendEvent := func(e *cadf.Event) bool {
		// While RabbitMQ is unreachable, only try to reconnect every 10 seconds, and put events into the backing store in between.
		// (Each attempt can take several seconds, and Record() blocks once its small buffer is full.)
		if !rc.IsNilOrClosed() || time.Since(lastConnectAttempt) >= 10*time.Second {
			if rc.IsNilOrClosed() {
				lastConnectAttempt = time.Now()
			}
			rc = refreshConnectionIfClosedOrOld(rc, rabbitmqURI, rabbitmqQueueName)
		}
		err := rc.PublishEvent(ctx, e)
		if err != nil {
			t.OnFailedPublish()
			logg.Error("audittools: failed to publish audit event with ID %q: %s", e.ID, err.Error())
			return false
		}
		t.OnSuccessfulPublish()
		return true
	}

	t.processEvents(sendEvent)
}

func refreshConnectionIfClosedOrOld(rc *rabbitConnection, uri url.URL, queueName string) *rabbitConnection {
	if !rc.IsNilOrClosed() && time.Since(rc.LastConnectedAt) < 5*time.Minute {
		return rc
	}
	if rc != nil {
		rc.Disconnect()
	}

	connection, err := newRabbitConnection(uri, queueName)
	if err != nil {
		logg.Error(err.Error())
		return nil
	}

	return connection
}

// processEvents is the main loop of Commit. It is separate from Commit only so that tests can provide their own sendEvent.
// It returns when t.EventSink is closed, which only happens in tests.
func (t auditTrail) processEvents(sendEvent func(*cadf.Event) bool) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// If an event can neither be published nor stored, we hold on to it and stop taking new events,
	// so that Record() blocks instead of losing events.
	var stalledEvent option.Option[cadf.Event]

	for {
		eventSink := t.EventSink
		if stalledEvent.IsSome() {
			eventSink = nil
		}

		select {
		case e, ok := <-eventSink:
			if !ok {
				return
			}
			if !t.handleEvent(e, sendEvent) {
				stalledEvent = option.Some(e)
			}
		case <-ticker.C:
			t.drainBackingStore(sendEvent)
			if e, ok := stalledEvent.Unpack(); ok && t.handleEvent(e, sendEvent) {
				stalledEvent = option.None[cadf.Event]()
			}
			err := t.BackingStore.UpdateMetrics()
			if err != nil {
				logg.Error("audittools: cannot update backing store metrics: %s", err.Error())
			}
		}
	}
}

// handleEvent publishes the event, or puts it in the backing store if it cannot be published.
// Returns false if neither worked.
// Events that cannot be serialized are dropped, since publishing them can never succeed.
//
// Events from the backing store are only published on the next drain,
// so events can be published out of order while RabbitMQ is coming back.
func (t auditTrail) handleEvent(e cadf.Event, sendEvent func(*cadf.Event) bool) bool {
	if sendEvent(&e) {
		return true
	}
	_, err := json.Marshal(e)
	if err != nil {
		logg.Error("audittools: dropping audit event with ID %q that cannot be serialized: %s", e.ID, err.Error())
		return true
	}
	err = t.BackingStore.Write(e)
	if err != nil {
		logg.Error("audittools: cannot write audit event with ID %q to backing store: %s", e.ID, err.Error())
		return false
	}
	return true
}

// drainBackingStore publishes events from the backing store until it is empty or publishing fails.
func (t auditTrail) drainBackingStore(sendEvent func(*cadf.Event) bool) {
	for {
		events, commit, err := t.BackingStore.ReadBatch()
		if err != nil {
			logg.Error("audittools: cannot read from backing store: %s", err.Error())
			return
		}
		if commit == nil {
			return // backing store is empty
		}

		published := 0
		for _, e := range events {
			if !sendEvent(&e) {
				break
			}
			published++
		}

		err = commit(published)
		if err != nil {
			logg.Error("audittools: cannot remove published events from backing store: %s", err.Error())
			return
		}
		if published < len(events) {
			return
		}
	}
}
