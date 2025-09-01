// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package httptest builds on net/http/httptest to make process-local HTTP requests inside tests as smooth as possible.
package httptest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"

	"github.com/sapcc/go-bits/must"
)

// Handler is a wrapper around http.Handler providing convenience methods for use in tests.
type Handler struct {
	inner http.Handler
}

// NewHandler wraps the given http.Handler in type Handler to provide extra convenience methods.
func NewHandler(inner http.Handler) Handler {
	return Handler{inner}
}

// ServeHTTP implements the http.Handler interface.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.inner.ServeHTTP(w, r)
}

// RespondTo executes an HTTP request against this handler.
// The interface is optimized towards readability and brevity in tests for REST APIs:
//
//   - The request method and URL are given in a single string, e.g. "POST /v1/objects/new".
//   - Additional headers, a request body, etc. can be provided as a list of options.
//
// The interface is optimized towards users of Ginkgo/Gomega.
// When you get the response, always check it with HaveHTTPStatus() first.
// This will catch any protocol-level and marshaling errors that may occur during the request.
//
//	var assets []Asset
//	resp := h.RespondTo(ctx, "GET /v1/assets", httptest.ReceiveJSONInto(&assets)).Response()
//	Expect(resp).To(HaveHTTPStatus(http.StatusOK))
//	Expect(assets).To(HaveLen(4))
//	Expect(assets[2]).To(Equal("example"))
func (h Handler) RespondTo(ctx context.Context, methodAndPath string, options ...RequestOption) Response {
	// NOTE: This function does not have an error return,
	//       in order to avoid an extra `Expect(err).To(BeNil())` line at every callsite.
	//
	//       We expect users to do `Expect(resp).To(HaveHTTPStatus(some2xxStatus))`,
	//       which will print the entire response including the error in the body.
	//
	//       There are also some cases in which this function panics.
	//       This is reserved for situations where the test code is clearly written incorrectly.
	//       Marshaling errors could come from a legitimate problem in the business logic,
	//       so they return a fabricated response using newResponseFromError() instead of panicing.

	// parse methodAndPath
	method, path, ok := strings.Cut(methodAndPath, " ")
	if !ok {
		panic(fmt.Sprintf("no method declared in methodAndPath = %q", methodAndPath))
	}

	// collect options
	params := requestParams{
		Headers: make(http.Header),
	}
	for _, opt := range options {
		opt(&params)
	}

	// prepare request body, if any
	reqBody := params.Body
	if params.JSONBody != nil {
		if reqBody != nil {
			panic("cannot use both WithBody() and WithJSONBody() in the same request")
		}
		buf, err := json.Marshal(params.JSONBody)
		if err != nil {
			return newResponseFromError("JSON Marshal Error", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	// build request
	req := must.Return(http.NewRequestWithContext(ctx, method, path, reqBody))
	maps.Insert(req.Header, maps.All(params.Headers))

	// obtain response
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// parse response body (if requested)
	if params.JSONTarget != nil && (rec.Code >= 200 && rec.Code <= 299) {
		err := json.Unmarshal(rec.Body.Bytes(), params.JSONTarget)
		if err == nil {
			rec.Body = bytes.NewBuffer(nil)
		}
		if err != nil {
			return newResponseFromError("JSON Unmarshal Error", err)
		}
	}

	return newResponseFromRecording(rec)
}

// RequestOption controls optional behavior in func Handler.RespondTo().
type RequestOption func(*requestParams)

type requestParams struct {
	Headers    http.Header
	Body       io.Reader
	JSONBody   any
	JSONTarget any
}

// WithBody adds a request body to an HTTP request.
//
// If the caller does not specify a Content-Type using WithBody(), application/octet-stream will be set.
func WithBody(r io.Reader) RequestOption {
	return func(params *requestParams) {
		params.Body = r
		if params.Headers.Get("Content-Type") == "" {
			params.Headers.Set("Content-Type", "application/octet-stream")
		}
	}
}

// WithHeader adds a single HTTP header to an HTTP request.
func WithHeader(key, value string) RequestOption {
	return func(params *requestParams) {
		params.Headers.Set(key, value)
	}
}

// WithHeaders adds several HTTP headers to an HTTP request.
func WithHeaders(hdr http.Header) RequestOption {
	return func(params *requestParams) {
		maps.Insert(params.Headers, maps.All(hdr))
	}
}

// WithJSONBody adds a JSON request body to an HTTP request.
// The provided payload will be serialized into JSON.
//
// If the caller does not specify a Content-Type using WithBody(), application/json will be set.
func WithJSONBody(payload any) RequestOption {
	return func(params *requestParams) {
		params.JSONBody = payload
		if params.Headers.Get("Content-Type") == "" {
			params.Headers.Set("Content-Type", "application/json; charset=utf-8")
		}
	}
}

// ReceiveJSONInto adds parsing of a JSON response body to an HTTP request.
// If the response has a 2xx status code, its response body will be unmarshaled into the provided target.
// If unmarshaling fails, the response will have status code 999 and contain the error message as a response body.
func ReceiveJSONInto(target any) RequestOption {
	// clear target, if any
	//
	// This is intended for when subsequent tests reuse the same target variable,
	// to avoid data from a previous unmarshaling to leak into the next round.
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer {
		panic("argument for ReceiveJSONInto() must be a pointer")
	}
	reflect.Indirect(v).SetZero()

	return func(params *requestParams) {
		params.JSONTarget = target
	}
}

// Response is the result type of Handler.RespondTo().
// It provides all components of the generated HTTP response as plain data fields to assert against,
// as well as convenience methods for complex assertions:
//
//	resp := h.RespondTo(ctx, "GET /v1/assets")
//	assert.Equal(t, resp.Code, http.StatusOK)
//
// Alternatively, the Response() method provides a full *http.Response object, which is useful for Gomega matchers
// (or when matching more obscure parts of the HTTP response like trailers):
//
//	resp := h.RespondTo(ctx, "GET /v1/assets").Response()
//	Expect(resp).To(HaveHTTPStatus(http.StatusOK))
type Response struct {
	resp *http.Response
	body *bytes.Buffer
}

func newResponseFromRecording(rec *httptest.ResponseRecorder) Response {
	return Response{rec.Result(), rec.Body}
}

func newResponseFromError(reason string, err error) Response {
	body := bytes.NewBufferString(err.Error())
	resp := &http.Response{
		Status:     "999 " + reason,
		StatusCode: 999,
		Header:     http.Header{"Content-Type": {"text/plain; charset=utf-8"}},
		Body:       io.NopCloser(body),
	}
	return Response{resp, body}
}

// StatusCode returns the HTTP status code of the response, or 999 for unexpected errors during Handler.RespondTo().
// It is a shorthand for Response().StatusCode.
func (r Response) StatusCode() int {
	return r.Response().StatusCode
}

// Header returns the HTTP headers of the response.
// It is a shorthand for Response().Header().
func (r Response) Header() http.Header {
	return r.Response().Header
}

// Body returns the response body, or an empty buffer if there is no response body.
// It is similar to Response().Body, except that the result is casted to a more useful type and guaranteed to be non-nil.
// Every call to this function returns a fresh buffer that starts reading at the first byte.
func (r Response) Body() *bytes.Buffer {
	if r.body == nil {
		return bytes.NewBuffer(nil)
	} else {
		return bytes.NewBuffer(r.body.Bytes())
	}
}

// Response returns a handle to the underlying *http.Response object inside this recorded response.
func (r Response) Response() *http.Response {
	return r.resp
}
