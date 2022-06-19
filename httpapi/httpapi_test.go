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

package httpapi_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/logg"
)

func TestHealthCheckAPI(t *testing.T) {
	//setup the healthcheck API with a choice of error value
	var currentError error
	h := httpapi.Compose(
		httpapi.HealthCheckAPI{
			Check: func() error {
				return currentError
			},
		},
		httpapi.WithoutLogging(),
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
	h := httpapi.Compose(httpapi.HealthCheckAPI{})

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
	h = httpapi.Compose(
		httpapi.HealthCheckAPI{
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
	h = httpapi.Compose(
		httpapi.HealthCheckAPI{
			Check: func() error {
				return errors.New("log suppression too strong")
			},
		},
		httpapi.WithoutLogging(),
	)

	assert.HTTPRequest{
		Method:       "GET",
		Path:         "/healthcheck",
		ExpectStatus: http.StatusInternalServerError,
		ExpectBody:   assert.StringData("log suppression too strong\n"),
	}.Check(t, h)
	expectLog("")
}
