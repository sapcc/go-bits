// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/assert"
)

// fakeRabbitMQ provides a sendEvent function for auditTrail that can be switched between working and failing.
type fakeRabbitMQ struct {
	mutex     sync.Mutex
	isUp      bool
	hangFor   time.Duration // while down, each publish fails only after this long (like a server that does not respond)
	published []string
}

func (f *fakeRabbitMQ) SendEvent(e *cadf.Event) bool {
	// like rabbitConnection.PublishEvent
	_, err := json.Marshal(e)
	if err != nil {
		return false
	}

	f.mutex.Lock()
	isUp, hangFor := f.isUp, f.hangFor
	if isUp {
		f.published = append(f.published, e.ID)
	}
	f.mutex.Unlock()
	if !isUp {
		time.Sleep(hangFor)
	}
	return isUp
}

func (f *fakeRabbitMQ) SetUp(isUp bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.isUp = isUp
}

func (f *fakeRabbitMQ) Published() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.published
}

func eventIDs(count int) []string {
	ids := make([]string, count)
	for idx := range ids {
		ids[idx] = fmt.Sprintf("event-%d", idx)
	}
	return ids
}

// runTrail runs auditTrail.processEvents in the background until the returned channel is closed.
func runTrail(store BackingStore, rabbit *fakeRabbitMQ) chan<- cadf.Event {
	eventSink := make(chan cadf.Event, 20) // same size as in NewAuditor
	go auditTrail{EventSink: eventSink, BackingStore: store}.processEvents(rabbit.SendEvent)
	return eventSink
}

func TestTrailWithDefaultBackingStoreDoesNotBlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// without configuration, events are buffered in memory without limit (like before backing stores existed),
		// so a RabbitMQ outage does not block Record()
		rabbit := &fakeRabbitMQ{isUp: false}
		eventSink := runTrail(newTestMemoryBackingStore(t, `{}`), rabbit)
		ids := eventIDs(5000)
		sent := make(chan struct{})
		go func() {
			for _, id := range ids {
				eventSink <- testEvent(id)
			}
			close(sent)
		}()
		synctest.Wait()
		select {
		case <-sent:
		default:
			t.Fatal("trail stopped accepting events, so Record() would block")
		}
		assert.Equal(t, len(rabbit.Published()), 0)

		// once RabbitMQ is back, everything is published in order on the next drain
		rabbit.SetUp(true)
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Equal(t, rabbit.Published(), ids)

		close(eventSink)
	})
}

func TestTrailKeepsEventWhenBackingStoreIsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rabbit := &fakeRabbitMQ{isUp: false}
		eventSink := runTrail(newTestMemoryBackingStore(t, `{"max_events":2}`), rabbit)
		ids := eventIDs(5)
		for _, id := range ids {
			eventSink <- testEvent(id)
		}

		// event-0 and event-1 go into the store, event-2 is held by the trail,
		// and the trail does not take event-3 and event-4 until event-2 is dealt with
		synctest.Wait()
		assert.Equal(t, len(eventSink), 2)

		// as long as RabbitMQ is down, the drain does not change anything (in particular, nothing is lost)
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Equal(t, len(eventSink), 2)
		assert.Equal(t, len(rabbit.Published()), 0)

		// once RabbitMQ is back, nothing was lost
		rabbit.SetUp(true)
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Equal(t, rabbit.Published(), ids)
		assert.Equal(t, len(eventSink), 0)

		close(eventSink)
	})
}

func TestTrailWithHangingRabbitMQ(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// RabbitMQ accepts connections, but does not respond
		rabbit := &fakeRabbitMQ{isUp: false, hangFor: 5 * time.Second}
		eventSink := runTrail(newTestMemoryBackingStore(t, `{}`), rabbit)
		ids := eventIDs(30)

		// Record() blocks once the buffer is full, until the trail has given up on enough events:
		// the trail takes event-0 right away, the buffer takes the next 20 events,
		// and each of the remaining 9 events has to wait for one failed publish
		start := time.Now()
		for _, id := range ids {
			eventSink <- testEvent(id)
		}
		assert.Equal(t, time.Since(start), 9*5*time.Second)

		// once RabbitMQ is back, nothing was lost
		rabbit.SetUp(true)
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		assert.Equal(t, slices.Sorted(slices.Values(rabbit.Published())), slices.Sorted(slices.Values(ids)))

		close(eventSink)
	})
}

func TestTrailDropsEventThatCannotBeSerialized(t *testing.T) {
	stores := map[string]func(*testing.T) BackingStore{
		"memory": func(t *testing.T) BackingStore { return newTestMemoryBackingStore(t, `{}`) },
		"file":   func(t *testing.T) BackingStore { return newTestFileBackingStore(t, `{}`) },
	}
	for name, newStore := range stores {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rabbit := &fakeRabbitMQ{isUp: true}
				store := newStore(t)
				eventSink := runTrail(store, rabbit)

				// this event can never be published, so it must not block the events after it
				event := testEvent("unserializable")
				event.Attachments = []cadf.Attachment{{Name: "payload", Content: make(chan int)}}
				eventSink <- event
				ids := eventIDs(3)
				for _, id := range ids {
					eventSink <- testEvent(id)
				}

				// the event is neither held by the trail nor put in the backing store
				close(eventSink)
				synctest.Wait()
				assert.Equal(t, rabbit.Published(), ids)
				assert.Equal(t, mustReadBatch(t, store, -1), []string{})
			})
		})
	}
}

func TestDrainBackingStoreWithPartialFailure(t *testing.T) {
	store := newTestMemoryBackingStore(t, `{}`)
	mustWrite(t, store, "event-1", "event-2", "event-3")
	trail := auditTrail{BackingStore: store}

	// RabbitMQ goes away after the first event
	rabbit := &fakeRabbitMQ{isUp: true}
	trail.drainBackingStore(func(e *cadf.Event) bool {
		ok := rabbit.SendEvent(e)
		rabbit.SetUp(false)
		return ok
	})
	assert.Equal(t, rabbit.Published(), []string{"event-1"})

	// only the unpublished events are retried, so nothing is published twice
	rabbit.SetUp(true)
	trail.drainBackingStore(rabbit.SendEvent)
	assert.Equal(t, rabbit.Published(), []string{"event-1", "event-2", "event-3"})
	assert.Equal(t, mustReadBatch(t, store, -1), []string{})
}
