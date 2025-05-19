// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestValidator(t *testing.T) {
	v := NewValidator(NewEnforcer(), nil)

	// setup a simple HTTP handler that just outputs status 204, 401 or 403 depending on auth result
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !v.CheckToken(r).Require(w, "api:access") {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// the default behavior is permissive
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	// Forbid() on an unrelated rule does not affect the result
	v.Enforcer.Forbid("api:details")
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	// Forbid() on the relevant rule causes 403 error
	v.Enforcer.Forbid("api:access")
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

	// explicit Allow() reverses an earlier Forbid
	v.Enforcer.Allow("api:access")
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
}
