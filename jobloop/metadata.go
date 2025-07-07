// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package jobloop

import (
	"fmt"
	"maps"

	"github.com/prometheus/client_golang/prometheus"
)

// JobMetadata contains metadata and common configuration for a job. Types that
// implement the Job interface will usually be holding one of these.
type JobMetadata struct {
	// A readable name or short description for this job. This will be used in
	// log messages.
	ReadableName string
	// Whether it is safe to have multiple tasks running in parallel. If set to
	// false, the job will never select a new task before the previous task has
	// been fully processed, thus avoiding any concurrent processing of tasks.
	ConcurrencySafe bool

	// Metadata for the counter metric that will be emitted by the job.
	CounterOpts prometheus.CounterOpts
	// The labels of the counter metric. Besides the application-specific labels
	// listed here, the counter metric will always have the label "task_outcome"
	// with the possible values "success" and "failure". This label will be
	// filled by the job implementation.
	CounterLabels []string

	counter *prometheus.CounterVec
}

const (
	outcomeLabelName    = "task_outcome"
	outcomeValueSuccess = "success"
	outcomeValueFailure = "failure"
)

// Internal API for job implementations: Registers and initializes the
// CounterVec that is described by this JobMetadata.
func (m *JobMetadata) setup(registerer prometheus.Registerer) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	allLabelNames := append([]string{outcomeLabelName}, m.CounterLabels...)
	m.counter = prometheus.NewCounterVec(m.CounterOpts, allLabelNames)
	registerer.MustRegister(m.counter)

	// ensure that at least one timeseries for each outcome exists in this counter
	// (so that absence alerts are useful)
	labels := make(prometheus.Labels, len(m.CounterLabels)+1)
	for _, label := range m.CounterLabels {
		labels[label] = "unknown"
	}
	labels[outcomeLabelName] = outcomeValueSuccess
	m.counter.With(labels).Add(0)
	labels[outcomeLabelName] = "failure"
	m.counter.With(labels).Add(0)
}

// State associated with a task (a single run of a job).
// The job implementation can put a suitable payload in the type argument slot.
type taskContext[T any] struct {
	Payload T
	Labels  prometheus.Labels
}

// Internal API for job implementations: Constructs a taskContext with default
// values for all labels defined for this job's CounterVec.
func makeTaskContext[T any](m JobMetadata, cfg jobConfig) taskContext[T] {
	labels := make(prometheus.Labels, len(m.CounterLabels)+1)
	for _, label := range m.CounterLabels {
		labels[label] = "early-db-access"
	}
	maps.Copy(labels, cfg.PrefilledLabels)
	return taskContext[T]{Labels: labels}
}

// Internal API for job implementations: Counts a finished or failed task.
// The "task_outcome" label will be set based on whether `err` is nil or not.
func (tc taskContext[T]) countTask(m JobMetadata, err error) {
	if err == nil {
		tc.Labels[outcomeLabelName] = outcomeValueSuccess
	} else {
		tc.Labels[outcomeLabelName] = "failure"
	}
	m.counter.With(tc.Labels).Inc()
}

// Internal API for job implementations: Enrich errors with additional error context if necessary.
// The `verb` argument describes which step of the task this error came from.
func (tc taskContext[T]) enrichError(verb string, err error, m JobMetadata, cfg jobConfig) error {
	if err == nil || !cfg.WantsExtraErrorContext {
		return err
	}

	return fmt.Errorf("could not %s task%s for job %q: %w",
		verb, cfg.PrefilledLabelsAsString(), m.ReadableName, err)
}
