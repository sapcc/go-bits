/*******************************************************************************
*
* Copyright 2022 SAP SE
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

package httpext

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestSetInsecureSkipVerify(t *testing.T) {
	orig := &http.Transport{}
	rt := http.RoundTripper(orig)
	wrap := WrapTransport(&rt)

	assert.DeepEqual(t, "TLSCLientConfig", orig.TLSClientConfig, (*tls.Config)(nil))

	wrap.SetInsecureSkipVerify(false)
	assert.DeepEqual(t, "TLSCLientConfig", orig.TLSClientConfig, (*tls.Config)(nil)) //check that false -> false is a true no-op

	wrap.SetInsecureSkipVerify(true)
	assert.DeepEqual(t, "TLSCLientConfig", orig.TLSClientConfig, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test fixture

	wrap.SetInsecureSkipVerify(false)
	assert.DeepEqual(t, "TLSCLientConfig", orig.TLSClientConfig, &tls.Config{InsecureSkipVerify: false}) //nolint:gosec // test fixture
}

func TestOverridesAndWraps(t *testing.T) {
	rt := http.RoundTripper(dummyRoundTripper{})

	//baseline
	hdr := makeDummyRequest(t, rt)
	assert.DeepEqual(t, "response headers", hdr, http.Header{
		"Host":   {"Dummy RoundTripper"},
		"Origin": {"Dummy Request"},
	})

	//just wrapping the RoundTripper without configuring anything does not change the result
	wrap := WrapTransport(&rt)
	hdr = makeDummyRequest(t, rt)
	assert.DeepEqual(t, "response headers", hdr, http.Header{
		"Host":   {"Dummy RoundTripper"},
		"Origin": {"Dummy Request"},
	})

	//now we add our User-Agent
	wrap.SetOverrideUserAgent("foo", "1.0")
	hdr = makeDummyRequest(t, rt)
	assert.DeepEqual(t, "response headers", hdr, http.Header{
		"Host":       {"Dummy RoundTripper"},
		"Origin":     {"Dummy Request"},
		"User-Agent": {"foo/1.0"},
	})

	//and we attach an additional middleware
	wrap.Attach(addHeader("Foo", "Bar"))
	hdr = makeDummyRequest(t, rt)
	assert.DeepEqual(t, "response headers", hdr, http.Header{
		"Foo":        {"Bar"},
		"Host":       {"Dummy RoundTripper"},
		"Origin":     {"Dummy Request"},
		"User-Agent": {"foo/1.0"},
	})
}

// A simple http.RoundTripper that just copies request headers into the response headers.
type dummyRoundTripper struct{}

func (dummyRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	hdr := make(http.Header, len(r.Header)+1)
	for k, v := range r.Header {
		hdr[k] = v
	}
	hdr.Set("Host", "Dummy RoundTripper")

	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     hdr,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Request:    r,
	}, nil
}

func makeDummyRequest(t *testing.T, rt http.RoundTripper) http.Header {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, "http://example.com/", http.NoBody)
	if err != nil {
		t.Fatal(err.Error())
	}
	req.Header.Set("Origin", "Dummy Request")

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer resp.Body.Close()
	return resp.Header
}

// A http.RoundTripper middleware that we can use to test Attach().
type headerAdder struct {
	Key   string
	Value string
	Inner http.RoundTripper
}

func addHeader(key, value string) func(http.RoundTripper) http.RoundTripper {
	return func(inner http.RoundTripper) http.RoundTripper {
		return headerAdder{key, value, inner}
	}
}

func (h headerAdder) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set(h.Key, h.Value)
	return h.Inner.RoundTrip(r)
}
