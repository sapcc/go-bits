// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
)

func TestValidator(t *testing.T) {
	ctx := t.Context()
	v := NewValidator(NewEnforcer(), nil)

	// setup a simple HTTP handler that just outputs status 204, 401 or 403 depending on auth result
	h := httptest.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !v.CheckToken(r).Require(w, "api:access") {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	// the default behavior is permissive
	resp := h.RespondTo(ctx, "GET /")
	assert.Equal(t, resp.StatusCode(), http.StatusNoContent)

	// Forbid() on an unrelated rule does not affect the result
	v.Enforcer.Forbid("api:details")
	resp = h.RespondTo(ctx, "GET /")
	assert.Equal(t, resp.StatusCode(), http.StatusNoContent)

	// Forbid() on the relevant rule causes 403 error
	v.Enforcer.Forbid("api:access")
	resp = h.RespondTo(ctx, "GET /")
	assert.Equal(t, resp.StatusCode(), http.StatusForbidden)

	// explicit Allow() reverses an earlier Forbid
	v.Enforcer.Allow("api:access")
	resp = h.RespondTo(ctx, "GET /")
	assert.Equal(t, resp.StatusCode(), http.StatusNoContent)
}
