// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	httptest_std "net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
)

func TestHealthCheckAPI(t *testing.T) {
	ctx := t.Context()

	// setup the healthcheck API with a choice of error value
	var currentError error
	h := httptest.NewHandler(Compose(
		HealthCheckAPI{
			Check: func() error {
				return currentError
			},
		},
		WithoutLogging(),
	))

	// test succeeding healthcheck
	currentError = nil
	h.RespondTo(ctx, "GET /healthcheck").
		ExpectText(t, http.StatusOK, "ok\n")

	// test failing healthcheck
	currentError = errors.New("datacenter on fire")
	h.RespondTo(ctx, "GET /healthcheck").
		ExpectText(t, http.StatusInternalServerError, "datacenter on fire\n")
}

func TestLogging(t *testing.T) {
	ctx := t.Context()

	// setup a buffer to capture the log into
	var buf bytes.Buffer
	logg.SetLogger(log.New(&buf, "", 0))

	// after every request, we will call this function to assert on what was written into `buf`
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
	// scenario 1: everything is logged
	h := httptest.NewHandler(Compose(HealthCheckAPI{}))

	// test a minimal request that logs
	h.RespondTo(ctx, "GET /healthcheck").
		ExpectText(t, http.StatusOK, "ok\n")
	expectLog(`REQUEST: 192.0.2.1 - - "GET /healthcheck HTTP/1.1" 200 3 "-" "-" 0.\d{3}s\n`)

	// test a request that logs header values
	h.RespondTo(ctx, "GET /healthcheck",
		httptest.WithHeader("User-Agent", "unit-test/1.0"),
		httptest.WithHeader("Referer", "https://example.org/"),
	).ExpectText(t, http.StatusOK, "ok\n")
	expectLog(`REQUEST: 192.0.2.1 - - "GET /healthcheck HTTP/1.1" 200 3 "https://example.org/" "unit-test/1.0" 0.\d{3}s\n`)

	////////////////////////////////////////////////////////////
	// scenario 2: non-error logs are suppressed
	var currentError error
	h = httptest.NewHandler(Compose(
		HealthCheckAPI{
			SkipRequestLog: true,
			Check: func() error {
				return currentError
			},
		},
	))

	// test a request that has its log suppressed
	h.RespondTo(ctx, "GET /healthcheck").
		ExpectText(t, http.StatusOK, "ok\n")
	expectLog("")

	// test that errors are not suppressed even if the request uses SkipRequestLog()
	currentError = errors.New("log suppression is on fire")
	h.RespondTo(ctx, "GET /healthcheck").
		ExpectText(t, http.StatusInternalServerError, "log suppression is on fire\n")
	expectLog(
		`REQUEST: 192.0.2.1 - - "GET /healthcheck HTTP/1.1" 500 27 "-" "-" 0.\d{3}s\n` +
			`ERROR: during "GET /healthcheck": log suppression is on fire\n`,
	)

	////////////////////////////////////////////////////////////
	// scenario 3: all logs are suppressed
	h = httptest.NewHandler(Compose(
		HealthCheckAPI{
			Check: func() error {
				return errors.New("log suppression too strong")
			},
		},
		WithoutLogging(),
	))

	h.RespondTo(ctx, "GET /healthcheck").
		ExpectText(t, http.StatusInternalServerError, "log suppression too strong\n")
	expectLog("")
}

func TestMetrics(t *testing.T) {
	ctx := t.Context()
	registry := prometheus.NewPedanticRegistry()
	testSetRegisterer(registry)

	h := httptest.NewHandler(Compose(metricsTestingAPI{}))

	// perform some calls to populate metrics
	resp := h.RespondTo(ctx, "POST /sleep/0.01/return/50")
	assert.Equal(t, resp.StatusCode(), http.StatusOK)
	assert.Equal(t, resp.BodyString(), strings.Repeat(".", 50))

	resp = h.RespondTo(ctx, "POST /sleep/0.15/return/5000",
		httptest.WithBody(strings.NewReader(strings.Repeat(".", 5000))),
	)
	assert.Equal(t, resp.StatusCode(), http.StatusOK)
	assert.Equal(t, resp.BodyString(), strings.Repeat(".", 5000))

	// collect metrics report
	h = httptest.NewHandler(promhttpNormalizer(promhttp.HandlerFor(registry, promhttp.HandlerOpts{})))
	resp = h.RespondTo(ctx, "GET /metrics")
	resp.ExpectBodyAsInFixture(t, http.StatusOK, "fixtures/metrics.prom")
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
	// This middleware first collects a complete `GET /metrics` response, then
	// does one very specific rewrite for test reproducibility.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// slurp the response from `GET /metrics`
		rec := httptest_std.NewRecorder()
		inner.ServeHTTP(rec, r)
		resp := rec.Result()

		// remove the indeterministic values for the `..._seconds_sum` metrics
		buf, err := io.ReadAll(resp.Body)
		if respondwith.ErrorText(w, err) {
			return
		}
		rx := regexp.MustCompile(`(seconds_sum{[^{}]*}) \d*\.\d*(?m:$)`)
		buf = rx.ReplaceAll(buf, []byte("$1 VARYING"))

		// replay the edited response into the actual ResponseWriter
		maps.Copy(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		w.Write(buf) //nolint:errcheck
	})
}
