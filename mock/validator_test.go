/******************************************************************************
*
*  Copyright 2023 SAP SE
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

package mock

import (
	"net/http"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestValidator(t *testing.T) {
	v := NewValidator(NewEnforcer(), nil)

	//setup a simple HTTP handler that just outputs status 204, 401 or 403 depending on auth result
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !v.CheckToken(r).Require(w, "api:access") {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	//the default behavior is permissive
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	//Forbid() on an unrelated rule does not affect the result
	v.Enforcer.Forbid("api:details")
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)

	//Forbid() on the relevant rule causes 403 error
	v.Enforcer.Forbid("api:access")
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusForbidden,
	}.Check(t, h)

	//explicit Allow() reverses an earlier Forbid
	v.Enforcer.Allow("api:access")
	assert.HTTPRequest{
		Method:       http.MethodGet,
		Path:         "/",
		ExpectStatus: http.StatusNoContent,
	}.Check(t, h)
}
