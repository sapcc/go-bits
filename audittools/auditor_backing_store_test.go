// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/assert"

	"github.com/sapcc/go-bits/must"
)

func TestBackingStoreFromEnv(t *testing.T) {
	opts := AuditorOpts{
		EnvPrefix: "TEST_AUDIT",
		Registry:  prometheus.NewPedanticRegistry(),
		BackingStoreFactories: map[string]BackingStoreFactory{
			"file": NewFileBackingStore,
		},
	}

	// without configuration: unlimited memory store, same as before backing stores existed
	store := must.ReturnT(opts.newBackingStoreFromEnv())(t)
	memoryStore, ok := store.(*inMemoryBackingStore)
	assert.Equal(t, ok, true)
	assert.Equal(t, memoryStore.maxEvents, 0)

	// with configuration
	dir := t.TempDir()
	t.Setenv("TEST_AUDIT_BACKING_STORE", fmt.Sprintf(`{"type":"file","params":{"directory":%q}}`, dir))
	opts.Registry = prometheus.NewPedanticRegistry()
	store = must.ReturnT(opts.newBackingStoreFromEnv())(t)
	fileStore, ok := store.(*fileBackingStore)
	assert.Equal(t, ok, true)
	assert.Equal(t, fileStore.directory, dir)

	// with invalid configuration
	t.Setenv("TEST_AUDIT_BACKING_STORE", `{"type":"sql","params":{}}`)
	_, err := opts.newBackingStoreFromEnv()
	assert.ErrEqual(t, err, `invalid value for TEST_AUDIT_BACKING_STORE: unknown backing store type "sql" (available: file, memory)`)

	t.Setenv("TEST_AUDIT_BACKING_STORE", `{"type":"memory","param":{}}`)
	_, err = opts.newBackingStoreFromEnv()
	assert.ErrEqual(t, err, regexp.MustCompile(`unknown field "param"`))
}

// recordingBackingStore is a BackingStore that reports each written event on a channel.
type recordingBackingStore struct {
	written chan cadf.Event
}

func (s recordingBackingStore) Write(event cadf.Event) error {
	s.written <- event
	return nil
}

func (s recordingBackingStore) ReadBatch() ([]cadf.Event, func(int) error, error) {
	return nil, nil, nil
}

func (s recordingBackingStore) UpdateMetrics() error {
	return nil
}

type testUserInfo struct{}

func (testUserInfo) AsInitiator(_ cadf.Host) cadf.Resource { return cadf.Resource{} }

type testTarget struct{}

func (testTarget) Render() cadf.Resource {
	return cadf.Resource{TypeURI: "test/target", ID: "target-id"}
}

func TestNewAuditorWithConnectionURLAndBackingStore(t *testing.T) {
	// applications that set ConnectionURL instead of EnvPrefix can still choose a backing store
	store := recordingBackingStore{written: make(chan cadf.Event, 1)}
	auditor := must.ReturnT(NewAuditor(t.Context(), AuditorOpts{
		Observer:      Observer{TypeURI: "service/test", Name: "test-service", ID: "test-id"},
		ConnectionURL: "amqp://127.0.0.1:1/", // nothing listens there, so publishing fails
		QueueName:     "test-queue",
		Registry:      prometheus.NewPedanticRegistry(),
		BackingStore:  store,
	}))(t)

	auditor.Record(Event{
		Time:       time.Now(),
		Request:    httptest.NewRequest(http.MethodGet, "/test", http.NoBody),
		User:       testUserInfo{},
		ReasonCode: 200,
		Action:     cadf.CreateAction,
		Target:     testTarget{},
	})

	select {
	case event := <-store.written:
		assert.Equal(t, event.Target.ID, "target-id")
	case <-time.After(30 * time.Second):
		t.Fatal("event was not written into the backing store")
	}
}
