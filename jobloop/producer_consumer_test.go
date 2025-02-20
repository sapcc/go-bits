/*******************************************************************************
*
* Copyright 2023 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package jobloop

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sapcc/go-bits/assert"
)

type producerConsumerEngine struct {
	// internal state of the producers and consumers (guarded by mutex)
	mutex      sync.Mutex
	discovered int
	processed  []string

	// hooks for the test to control the order of execution
	processingBlocker chan struct{}
	wgProcessorsReady sync.WaitGroup
}

func (e *producerConsumerEngine) Job(registerer prometheus.Registerer) Job {
	return (&ProducerConsumerJob[int]{
		Metadata: JobMetadata{
			ReadableName:    "test job",
			ConcurrencySafe: true,
			CounterOpts:     prometheus.CounterOpts{Name: "test_job_runs", Help: "Hello World."},
			CounterLabels:   []string{},
		},
		DiscoverTask: e.DiscoverTask,
		ProcessTask:  e.ProcessTask,
	}).Setup(registerer)
}

func (e *producerConsumerEngine) DiscoverTask(ctx context.Context, labels prometheus.Labels) (int, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// only generate 10 tasks
	if e.discovered >= 10 {
		return 0, sql.ErrNoRows
	}

	// generate the next task
	e.discovered += 1
	return e.discovered, nil
}

func (e *producerConsumerEngine) ProcessTask(ctx context.Context, value int, labels prometheus.Labels) error {
	// signal to the test that ProcessTask has been started (the test uses this to
	// wait until the expected number of tasks were scheduled)
	e.wgProcessorsReady.Done()
	// wait for the test to allow us to proceed
	if e.processingBlocker != nil {
		for range e.processingBlocker {
		}
	}

	// track which tasks were processed
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.processed = append(e.processed, fmt.Sprintf("%02d", value))
	return nil
}

func (e *producerConsumerEngine) checkAllProcessed(t *testing.T, registry *prometheus.Registry) {
	// check that 10 tasks were dispatched
	if e.discovered != 10 {
		t.Errorf("expected 10 tasks to be discovered, but got %d", e.discovered)
	}

	// check that all 10 tasks were processed
	sort.Strings(e.processed)
	if strings.Join(e.processed, ",") != "01,02,03,04,05,06,07,08,09,10" {
		t.Errorf("expected tasks 01 through 10 to be processed, but got %v", e.processed)
	}

	expectedMetrics := []string{
		"# HELP test_job_runs Hello World.\n",
		"# TYPE test_job_runs counter\n",
		"test_job_runs{task_outcome=\"failure\"} 0\n",
		"test_job_runs{task_outcome=\"success\"} 10\n",
	}
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/metrics",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData(strings.Join(expectedMetrics, "")),
	}.Check(t, handler)
}

func TestSingleThreaded(t *testing.T) {
	// This test covers the single-threaded (or rather, single-goroutined)
	// execution model for the ProducerConsumerJob.
	engine := producerConsumerEngine{}
	registry := prometheus.NewPedanticRegistry()
	job := engine.Job(registry)

	// start the job machinery
	var wgJobLoop sync.WaitGroup
	wgJobLoop.Add(1)
	engine.wgProcessorsReady.Add(10)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer wgJobLoop.Done()
		job.Run(ctx)
	}()

	// wait until all tasks have been dispatched
	engine.wgProcessorsReady.Wait()
	// instruct job loop to shutdown
	cancel()
	wgJobLoop.Wait()

	engine.checkAllProcessed(t, registry)
}

func TestMultiThreaded(t *testing.T) {
	// This test checks that the queueing in the multi-threaded job loop works as
	//intended: When there are multiple operations to execute, each operation
	// gets executed exactly once, without having to wait for earlier tasks to
	// complete (as long as there are enough workers).
	engine := producerConsumerEngine{
		processingBlocker: make(chan struct{}),
	}
	registry := prometheus.NewPedanticRegistry()
	job := engine.Job(registry)

	// start the job machinery
	var wgJobLoop sync.WaitGroup
	wgJobLoop.Add(1)
	engine.wgProcessorsReady.Add(10)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer wgJobLoop.Done()
		job.Run(ctx, NumGoroutines(11))
	}()

	// wait until all tasks have been dispatched
	engine.wgProcessorsReady.Wait()
	// allow them to proceed all at once
	close(engine.processingBlocker)
	// wait until all processing is done
	cancel()
	wgJobLoop.Wait()

	engine.checkAllProcessed(t, registry)
}
