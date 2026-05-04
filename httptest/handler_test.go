// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httptest_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/internal/testutil"
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
	ctx := t.Context()

	// most basic invocation
	resp := h.RespondTo(ctx, "POST /reflect")
	assert.Equal(t, resp.StatusCode(), 200)

	// check WithHeader()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeader("Foo", "bar"),
	)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.DeepEqual(t, "Reflected-Foo", resp.Header()["Reflected-Foo"], []string{"bar"})

	// check WithHeaders()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeaders(http.Header{
			"Foo":     {"bar"},
			"Numbers": {"23", "42"},
		}),
	)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.DeepEqual(t, "Reflected-Foo", resp.Header()["Reflected-Foo"], []string{"bar"})
	assert.DeepEqual(t, "Reflected-Numbers", resp.Header()["Reflected-Numbers"], []string{"23", "42"})

	// check WithBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("Hello world")),
	)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.Equal(t, resp.Header().Get("Reflected-Content-Type"), "application/octet-stream")
	assert.Equal(t, resp.BodyString(), "Hello world")

	// check that WithHeader("Content-Type") overrides the default of WithBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeader("Content-Type", "text/plain"),
		httptest.WithBody(strings.NewReader("Hello world")),
	)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.Equal(t, resp.Header().Get("Reflected-Content-Type"), "text/plain")
	assert.Equal(t, resp.BodyString(), "Hello world")

	// same check as the one above, but rephrased to provide test coverage for MergeRequestOptions()
	resp = h.RespondTo(ctx, "POST /reflect", httptest.MergeRequestOptions(
		httptest.WithHeader("Content-Type", "text/plain"),
		httptest.WithBody(strings.NewReader("Hello world")),
	))
	assert.Equal(t, resp.StatusCode(), 200)
	assert.Equal(t, resp.Header().Get("Reflected-Content-Type"), "text/plain")
	assert.Equal(t, resp.BodyString(), "Hello world")

	// check WithJSONBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithJSONBody([]bool{true, false, true}),
	)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.Equal(t, resp.Header().Get("Reflected-Content-Type"), "application/json; charset=utf-8")
	assert.Equal(t, resp.BodyString(), `[true,false,true]`)

	// check that WithHeader("Content-Type") overrides the default of WithJSONBody()
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeader("Content-Type", "application/x-just-bools+json"),
		httptest.WithJSONBody([]bool{true, false, true}),
	)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.Equal(t, resp.Header().Get("Reflected-Content-Type"), "application/x-just-bools+json")
	assert.Equal(t, resp.BodyString(), `[true,false,true]`)

	// check CaptureJSON()
	var output map[string]any
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"foo":"foofoo"}`)),
	).CaptureJSON(&output)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.DeepEqual(t, "Reflected Body", output, map[string]any{"foo": "foofoo"})

	// check that CaptureJSON() zeroes its target before unmarshaling
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"bar":"barbar"}`)),
	).CaptureJSON(&output)
	assert.Equal(t, resp.StatusCode(), 200)
	assert.DeepEqual(t, "Reflected Body", output, map[string]any{"bar": "barbar"}) // "foo" member from previous test was removed before unmarshaling

	// check that CaptureJSON() zeroes its target even if unmarshaling
	// is skipped because the response has a non-2xx status
	resp = h.RespondTo(ctx, "GET /not-found").CaptureJSON(&output)
	assert.Equal(t, resp.StatusCode(), 404)                    // was not overwritten with 999 for a failed unmarshal
	assert.Equal(t, resp.BodyString(), "404 page not found\n") // was not overwritten with an unmarshal error
	assert.DeepEqual(t, "Reflected Body", output, nil)

	// check CaptureHeader()
	var (
		quxHeader     = "unset"
		fooHeader     = "unset"
		numbersHeader = "unset"
	)
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithHeaders(http.Header{
			"Foo":     {"bar"},
			"Numbers": {"23", "42"},
		}),
		httptest.WithBody(strings.NewReader(`hello`))).
		CaptureHeader("reflected-foo", &fooHeader).
		CaptureHeader("Reflected-Qux", &quxHeader).
		CaptureHeader("rEfLeCtEd-nUmBeRs", &numbersHeader) // check key normalization
	assert.Equal(t, fooHeader, "bar")
	assert.Equal(t, quxHeader, "")       // check that non-existent header writes empty string
	assert.Equal(t, numbersHeader, "23") // check that header with multiple values writes first result

	// check how WithJSONBody() reports JSON marshaling errors
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithJSONBody(time.Now), // functions cannot be serialized as JSON
	)
	assert.Equal(t, resp.StatusCode(), 999)
	assert.Equal(t, resp.Response().Status, "999 JSON Marshal Error")
	assert.Equal(t, resp.BodyString(), "json: unsupported type: func() time.Time")

	// check how CaptureJSON() reports JSON unmarshaling errors
	var outputNumber int
	resp = h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`"Hello"`)),
	).CaptureJSON(&outputNumber)
	assert.Equal(t, resp.StatusCode(), 999)
	assert.Equal(t, resp.Response().Status, "999 JSON Unmarshal Error")
	assert.Equal(t, resp.BodyString(), "json: cannot unmarshal string into Go value of type int")

	// check ExpectJSON()
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"foo":23,"bar":42}`)),
	).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
		"foo": 23,
		"bar": 42,
	})

	// check how ExpectJSON() reports an unexpected status code
	mock := &testutil.MockT{}
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"foo":23,"bar":42}`)),
	).ExpectJSON(mock, http.StatusNotFound, jsonmatch.Object{})
	mock.ExpectErrors(t, `expected HTTP status 404, but got 200 (body was "{\"foo\":23,\"bar\":42}")`)

	// check how ExpectJSON() reports diffs without Pointer
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"foo":23,"bar":42}`)),
	).ExpectJSON(mock, http.StatusOK, jsonmatch.Scalar(true))
	mock.ExpectErrors(t, `type mismatch: expected true, but got {"bar":42,"foo":23}`)

	// check how ExpectJSON() reports diffs with Pointer
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader(`{"foo":23,"bar":42}`)),
	).ExpectJSON(mock, http.StatusOK, jsonmatch.Object{
		"foo": 23,
		"bar": 45,
	})
	mock.ExpectErrors(t, `value mismatch at /bar: expected 45, but got 42`)

	// check ExpectStatus()
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectStatus(t, http.StatusOK)

	// check how ExpectStatus() reports an unexpected status code
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectStatus(mock, http.StatusNotFound)
	mock.ExpectErrors(t, `expected HTTP status 404, but got 200 (body was "hello")`)

	// check ExpectBody()
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectBody(t, http.StatusOK, []byte("hello"))

	// check how ExpectBody() reports an unexpected status code
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectBody(mock, http.StatusNotFound, []byte("hello"))
	mock.ExpectErrors(t, `expected HTTP status 404, but got 200 (body was "hello")`)

	// check how ExpectBody() reports an unexpected response body
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectBody(mock, http.StatusOK, []byte("world"))
	mock.ExpectErrors(t, `expected "world", but got "hello"`)

	// check how ExpectBody() reacts to non-UTF-8 response bodies
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(bytes.NewReader([]byte{0xff, 0xfe, 0xfd})),
	).ExpectBody(mock, http.StatusOK, []byte("world"))
	mock.ExpectErrors(t, `expected "world", but got "\xff\xfe\xfd"`)

	// check ExpectText()
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectText(t, http.StatusOK, "hello")

	// check how ExpectText() reports an unexpected status code
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectText(mock, http.StatusNotFound, "hello")
	mock.ExpectErrors(t, `expected HTTP status 404, but got 200 (body was "hello")`)

	// check how ExpectText() reports an unexpected response body
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(strings.NewReader("hello")),
	).ExpectText(mock, http.StatusOK, "world")
	mock.ExpectErrors(t, `expected "world", but got "hello"`)

	// check how ExpectText() reacts to non-UTF-8 response bodies
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(bytes.NewReader([]byte{0xff, 0xfe, 0xfd})),
	).ExpectText(mock, http.StatusOK, "world")
	mock.ExpectErrors(t, `expected "world", but got "�"`)

	// check ExpectBodyAsInFixture()
	h.RespondTo(ctx, "POST /reflect",
		httptest.WithBody(bytes.NewReader(must.Return(os.ReadFile("fixtures/example.txt")))),
	).ExpectBodyAsInFixture(t, http.StatusOK, "fixtures/example.txt")

	// check ExpectHeader()
	h.RespondTo(ctx, "POST /reflect", httptest.WithHeaders(http.Header{
		"Foo":     {"bar"},
		"Numbers": {"23", "42"},
	})).
		ExpectHeader(t, "rEfLeCtEd-fOo", "bar").    // check normalization of keys
		ExpectHeader(t, "Reflected-Numbers", "23"). // Header.Get() only returns first value
		ExpectHeader(t, "Reflected-Nonsense", "").  // check that this header is absent
		ExpectStatus(t, http.StatusOK)

	// same check as the one above, rewritten to provide test coverage for Response.Expect()
	customAssertion := func(r httptest.Response) {
		r.ExpectHeader(t, "rEfLeCtEd-fOo", "bar")
		r.ExpectHeader(t, "Reflected-Numbers", "23")
		r.ExpectHeader(t, "Reflected-Nonsense", "")
	}
	h.RespondTo(ctx, "POST /reflect", httptest.WithHeaders(http.Header{
		"Foo":     {"bar"},
		"Numbers": {"23", "42"},
	})).Expect(customAssertion).ExpectStatus(t, http.StatusOK)

	// check how ExpectHeader() reports an unexpected header
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect", httptest.WithHeader("Foo", "qux")).
		ExpectHeader(mock, "rEfLeCtEd-fOo", "bar"). // check normalization of keys
		ExpectStatus(mock, http.StatusOK)
	mock.ExpectErrors(t, `expected "Reflected-Foo: bar", but got "Reflected-Foo: qux"`)

	// check ExpectHeaders()
	h.RespondTo(ctx, "POST /reflect", httptest.WithHeaders(http.Header{
		"Foo":     {"bar"},
		"Numbers": {"23", "42"},
	})).ExpectHeaders(t, http.Header{
		"rEfLeCtEd-fOo":      {"bar"}, // check normalization of keys
		"Reflected-Numbers":  {"23", "42"},
		"Reflected-Nonsense": {}, // check that this header is absent
	}).ExpectStatus(t, http.StatusOK)

	// check how ExpectHeaders() reports an unexpected header
	mock.Errors = nil
	h.RespondTo(ctx, "POST /reflect", httptest.WithHeader("Numbers", "-1")).
		ExpectHeaders(mock, http.Header{
			"rEfLeCtEd-nUmBeRs": {"23", "42"}, // check normalization of keys
		}).ExpectStatus(mock, http.StatusOK)
	mock.ExpectErrors(t, `expected "Reflected-Numbers: 23\r\nReflected-Numbers: 42", but got "Reflected-Numbers: -1"`)
}
