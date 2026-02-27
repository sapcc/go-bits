// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewAuditorInvalidBackingStoreConfig(t *testing.T) {
	t.Setenv("TEST_AUDIT_BACKING_STORE", `{"type":"invalid_type","params":{}}`)
	t.Setenv("TEST_AUDIT_QUEUE_NAME", "test-queue")

	_, err := newTestAuditor(t, AuditorOpts{
		EnvPrefix: "TEST_AUDIT",
	})

	if err == nil {
		t.Fatal("expected error for invalid backing store config, got nil")
	}

	expectedMsg := "unknown backing store type"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("expected error containing %q, got: %v", expectedMsg, err)
	}
}

func TestNewAuditorValidBackingStoreConfig(t *testing.T) {
	tmpDir := t.TempDir()
	backingStoreConfig := `{"type":"file","params":{"directory":"` + tmpDir + `","max_total_size":1073741824}}`
	t.Setenv("TEST_AUDIT_BACKING_STORE", backingStoreConfig)
	t.Setenv("TEST_AUDIT_QUEUE_NAME", "test-queue")

	auditor, err := newTestAuditor(t, AuditorOpts{
		EnvPrefix: "TEST_AUDIT",
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if auditor == nil {
		t.Fatal("expected auditor to be created, got nil")
	}
}

// newTestAuditor creates an Auditor with sensible test defaults.
func newTestAuditor(t *testing.T, opts AuditorOpts) (Auditor, error) {
	t.Helper()

	if opts.Observer.TypeURI == "" {
		opts.Observer = Observer{
			TypeURI: "service/test",
			Name:    "test-service",
			ID:      "test-id",
		}
	}

	if opts.Registry == nil {
		opts.Registry = prometheus.NewRegistry()
	}

	// Provide default backing store factories if not specified
	if opts.BackingStoreFactories == nil && opts.BackingStore == nil {
		opts.BackingStoreFactories = map[string]BackingStoreFactory{
			"file":   NewFileBackingStore,
			"memory": NewInMemoryBackingStore,
		}
	}

	return NewAuditor(context.Background(), opts)
}
