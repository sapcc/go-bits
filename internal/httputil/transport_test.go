// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httputil_test

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/sapcc/go-bits/internal/httputil"
	"github.com/sapcc/go-bits/must"
)

func TestDefaultTransport(t *testing.T) {
	actual := must.ReturnT(httputil.NewTransport(httputil.TransportOpts{}))(t)
	expected := http.DefaultTransport.(*http.Transport)

	// we cannot use reflect.DeepEqual() to compare both sides because there are
	// private fields with pointers that will always be different, and that is
	// intentional (we don't want to carbon-copy the default transport object and
	// thus share its internal mutexes etc.)
	diff := cmp.Diff(expected, actual,
		cmpopts.IgnoreUnexported(http.Transport{}),
		cmpopts.IgnoreFields(http.Transport{}, "Proxy", "DialContext"), // cannot deep-compare function pointers
	)
	if diff != "" {
		t.Errorf("mismatch in default transport (-httputil.NewTransport +http.DefaultTransport):\n%s", diff)
	}
}
