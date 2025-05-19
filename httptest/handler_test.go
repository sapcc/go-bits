// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httptest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
)

// This example handler recognizes the endpoint "POST /reflect",
// which returns all request headers as response headers with the extra prefix "Reflected-"
// (e.g. "Reflected-Content-Type: text/plain").
// Also, if there is a request body, it will be copied into the response body.
var exampleHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.URL.Path != "/reflect" {
		http.NotFound(w, r)
		return
	}

	for k, v := range r.Header {
		w.Header()["Reflected-"+k] = v
	}
	w.WriteHeader(http.StatusOK)

	if r.Body != nil {
		_, err := io.Copy(w, r.Body)
		if err != nil {
			panic(err.Error())
		}
	}
})

func TestRespondTo(t *testing.T) {
	h := httptest.NewHandler(exampleHandler)
	ctx := t.Context() // TODO: use t.Context() in Go 1.24+

	// most basic invocation
	resp := h.RespondTo(ctx, "POST /reflect")
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)

	// check WithHeader()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeader("Foo", "bar"),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected-Foo", resp.Header["Reflected-Foo"], []string{"bar"})

	// check WithHeaders()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeaders(http.Header{
			"Foo":     {"bar"},
			"Numbers": {"23", "42"},
		}),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected-Foo", resp.Header["Reflected-Foo"], []string{"bar"})
	assert.DeepEqual(t, "Reflected-Numbers", resp.Header["Reflected-Numbers"], []string{"23", "42"})

	// check WithBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("Hello world")),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected-Content-Type", resp.Header.Get("Reflected-Content-Type"), "application/octet-stream")
	buf := must.Return(io.ReadAll(resp.Body))
	assert.DeepEqual(t, "Reflected Body", string(buf), "Hello world")

	// check that WithHeader("Content-Type") overrides the default of WithBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeader("Content-Type", "text/plain"),
		httptest.WithBody(strings.NewReader("Hello world")),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected-Content-Type", resp.Header.Get("Reflected-Content-Type"), "text/plain")
	buf = must.Return(io.ReadAll(resp.Body))
	assert.DeepEqual(t, "Reflected Body", string(buf), "Hello world")

	// check WithJSONBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithJSONBody([]bool{true, false, true}),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected-Content-Type", resp.Header.Get("Reflected-Content-Type"), "application/json; charset=utf-8")
	buf = must.Return(io.ReadAll(resp.Body))
	assert.DeepEqual(t, "Reflected Body", string(buf), `[true,false,true]`)

	// check that WithHeader("Content-Type") overrides the default of WithJSONBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeader("Content-Type", "application/x-just-bools+json"),
		httptest.WithJSONBody([]bool{true, false, true}),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected-Content-Type", resp.Header.Get("Reflected-Content-Type"), "application/x-just-bools+json")
	buf = must.Return(io.ReadAll(resp.Body))
	assert.DeepEqual(t, "Reflected Body", string(buf), `[true,false,true]`)

	// check ReceiveJSONInto()
	var output map[string]any
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"foo":"foofoo"}`)),
		httptest.ReceiveJSONInto(&output),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected Body", output, map[string]any{"foo": "foofoo"})

	// check that ReceiveJSONInto() zeroes its target before unmarshaling
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"bar":"barbar"}`)),
		httptest.ReceiveJSONInto(&output),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 200)
	assert.DeepEqual(t, "Reflected Body", output, map[string]any{"bar": "barbar"}) // "foo" member from previous test was removed before unmarshaling

	// check how JSON marshaling errors are reported
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithJSONBody(time.Now), // functions cannot be serialized as JSON
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 999)
	assert.DeepEqual(t, "Status", resp.Status, "999 JSON Marshal Error")
	buf = must.Return(io.ReadAll(resp.Body))
	assert.DeepEqual(t, "Error Message In Body", string(buf), "json: unsupported type: func() time.Time")

	// check how JSON unmarshaling errors are reported
	var outputNumber int
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`"Hello"`)),
		httptest.ReceiveJSONInto(&outputNumber),
	)
	assert.DeepEqual(t, "Status", resp.StatusCode, 999)
	assert.DeepEqual(t, "Status", resp.Status, "999 JSON Unmarshal Error")
	buf = must.Return(io.ReadAll(resp.Body))
	assert.DeepEqual(t, "Error Message In Body", string(buf), "json: cannot unmarshal string into Go value of type int")
}
