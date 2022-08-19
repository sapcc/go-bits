/******************************************************************************
*
*  Copyright 2022 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
)

func TestHealthCheckAPI(t *testing.T) {
	//setup the healthcheck API with a choice of error value
	var currentError error
	h := Compose(
		HealthCheckAPI{
			Check: func() error {
				return currentError
			},
		},
		WithoutLogging(),
	)

	//test succeeding healthcheck
	currentError = nil
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData("ok\n"),
	}.Check(t, h)

	//test failing healthcheck
	currentError = errors.New("datacenter on fire")
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusInternalServerError,
		ExpectBody:   assert.StringData("datacenter on fire\n"),
	}.Check(t, h)
}

func TestLogging(t *testing.T) {
	//setup a buffer to capture the log into
	var buf bytes.Buffer
	logg.SetLogger(log.New(&buf, "", 0))

	//after every request, we will call this function to assert on what was written into `buf`
	expectLog := func(pattern string) {
		t.Helper()
		rx := regexp.MustCompile(fmt.Sprintf("^(?:%s)$", pattern))
		actualLog, err := io.ReadAll(&buf)
		if err != nil {
			t.Fatal(err.Error())
		}
		if !rx.Match(actualLog) {
			t.Errorf("expected log that looks like %q, but got %q", pattern, string(actualLog))
		}
	}

	////////////////////////////////////////////////////////////
	//scenario 1: everything is logged
	h := Compose(HealthCheckAPI{})

	//test a minimal request that logs
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData("ok\n"),
	}.Check(t, h)
	expectLog(`REQUEST: 192.0.2.1 - - "GET /healthcheck HTTP/1.1" 200 3 "-" "-" 0.\d{3}s\n`)

	//test a request that logs header values
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		Header:       map[string]string{"User-Agent": "unit-test/1.0", "Referer": "https://example.org/"},
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData("ok\n"),
	}.Check(t, h)
	expectLog(`REQUEST: 192.0.2.1 - - "GET /healthcheck HTTP/1.1" 200 3 "https://example.org/" "unit-test/1.0" 0.\d{3}s\n`)

	////////////////////////////////////////////////////////////
	//scenario 2: non-error logs are suppressed
	var currentError error
	h = Compose(
		HealthCheckAPI{
			SkipRequestLog: true,
			Check: func() error {
				return currentError
			},
		},
	)

	//test a request that has its log suppressed
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData("ok\n"),
	}.Check(t, h)
	expectLog("")

	//test that errors are not suppressed even if the request uses SkipRequestLog()
	currentError = errors.New("log suppression is on fire")
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusInternalServerError,
		ExpectBody:   assert.StringData("log suppression is on fire\n"),
	}.Check(t, h)
	expectLog(
		`REQUEST: 192.0.2.1 - - "GET /healthcheck HTTP/1.1" 500 27 "-" "-" 0.\d{3}s\n` +
			`ERROR: during "GET /healthcheck": log suppression is on fire\n`,
	)

	////////////////////////////////////////////////////////////
	//scenario 3: all logs are suppressed
	h = Compose(
		HealthCheckAPI{
			Check: func() error {
				return errors.New("log suppression too strong")
			},
		},
		WithoutLogging(),
	)

	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusInternalServerError,
		ExpectBody:   assert.StringData("log suppression too strong\n"),
	}.Check(t, h)
	expectLog("")
}

func TestMetrics(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	testSetRegisterer(registry)

	h := Compose(metricsTestingAPI{})

	//perform some calls to populate metrics
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/sleep/0.01/return/50",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData(strings.Repeat(".", 50)),
	}.Check(t, h)
	assert.HTTPRequest{
		Method:       "POST",
		Path:         "/sleep/0.15/return/5000",
		Body:         assert.StringData(strings.Repeat(".", 5000)),
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.StringData(strings.Repeat(".", 5000)),
	}.Check(t, h)

	//collect metrics report
	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/metrics",
		ExpectStatus: http.StatusOK,
		ExpectBody:   assert.FixtureFile("fixtures/metrics.prom"),
	}.Check(t, promhttpNormalizer(promhttp.HandlerFor(registry, promhttp.HandlerOpts{})))
}

type metricsTestingAPI struct{}

func (m metricsTestingAPI) AddTo(r *mux.Router) {
	r.Methods("POST").Path("/sleep/{secs}/return/{count}").HandlerFunc(m.handleRequest)
}

func (m metricsTestingAPI) handleRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	secs, err := strconv.ParseFloat(vars["secs"], 64)
	if respondwith.ErrorText(w, err) {
		return
	}
	//NOTE: `time.Duration(secs)` does not work because all values < 1 would all be truncated to 0.
	time.Sleep(time.Duration(secs * float64(time.Second)))

	count, err := strconv.Atoi(vars["count"])
	if respondwith.ErrorText(w, err) {
		return
	}
	w.Write(bytes.Repeat([]byte("."), count)) //nolint:errcheck
}

func promhttpNormalizer(inner http.Handler) http.Handler {
	//This middleware first collects a complete `GET /metrics` response, then
	//does one very specific rewrite for test reproducability.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//slurp the response from `GET /metrics`
		rec := httptest.NewRecorder()
		inner.ServeHTTP(rec, r)
		resp := rec.Result()

		//remove the undeterministic values for the `..._seconds_sum` metrics
		buf, err := io.ReadAll(resp.Body)
		if respondwith.ErrorText(w, err) {
			return
		}
		rx := regexp.MustCompile(`(seconds_sum{[^{}]*}) \d*\.\d*(?m:$)`)
		buf = rx.ReplaceAll(buf, []byte("$1 VARYING"))

		//replay the edited response into the actual ResponseWriter
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(buf) //nolint:errcheck
	})
}
