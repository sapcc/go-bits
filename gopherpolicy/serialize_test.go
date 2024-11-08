/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package gopherpolicy

import (
	"fmt"
	"testing"

	policy "github.com/databus23/goslo.policy"

	"github.com/sapcc/go-bits/assert"
)

func TestSerializeCompactContext(t *testing.T) {
	testCases := []struct {
		Context    policy.Context
		Serialized string
	}{
		// project scope with user from the same domain
		{
			Context: policy.Context{
				Auth: map[string]string{
					"project_domain_id":   "123",
					"project_domain_name": "acme",
					"project_id":          "234",
					"project_name":        "roadrunner",
					"tenant_domain_id":    "123",
					"tenant_domain_name":  "acme",
					"tenant_id":           "234",
					"tenant_name":         "roadrunner",
					"user_domain_id":      "123",
					"user_domain_name":    "acme",
					"user_id":             "345",
					"user_name":           "coyote",
				},
				Roles: []string{
					"admin",
					"member",
				},
			},
			Serialized: `{"v":1,"p":["234","roadrunner"],"d":["123","acme"],"u":["345","coyote"],"r":["admin","member"]}`,
		},
		// same as above, but spawned from an application credential (SAP Converged Cloud extension)
		{
			Context: policy.Context{
				Auth: map[string]string{
					"project_domain_id":           "123",
					"project_domain_name":         "acme",
					"project_id":                  "234",
					"project_name":                "roadrunner",
					"tenant_domain_id":            "123",
					"tenant_domain_name":          "acme",
					"tenant_id":                   "234",
					"tenant_name":                 "roadrunner",
					"user_domain_id":              "123",
					"user_domain_name":            "acme",
					"user_id":                     "345",
					"user_name":                   "coyote",
					"application_credential_id":   "456",
					"application_credential_name": "machine",
				},
				Roles: []string{
					"admin",
					"member",
				},
			},
			Serialized: `{"v":1,"p":["234","roadrunner"],"d":["123","acme"],"u":["345","coyote"],"ac":["456","machine"],"r":["admin","member"]}`,
		},
		// project scope with user from a different domain
		{
			Context: policy.Context{
				Auth: map[string]string{
					"project_domain_id":   "123",
					"project_domain_name": "acme",
					"project_id":          "234",
					"project_name":        "roadrunner",
					"tenant_domain_id":    "123",
					"tenant_domain_name":  "acme",
					"tenant_id":           "234",
					"tenant_name":         "roadrunner",
					"user_domain_id":      "default",
					"user_domain_name":    "Default",
					"user_id":             "012",
					"user_name":           "admin",
				},
				Roles: []string{
					"admin",
				},
			},
			Serialized: `{"v":1,"p":["234","roadrunner"],"d":["123","acme"],"u":["012","admin"],"ud":["default","Default"],"r":["admin"]}`,
		},
		// domain scope with user from the same domain
		{
			Context: policy.Context{
				Auth: map[string]string{
					"domain_id":        "123",
					"domain_name":      "acme",
					"user_domain_id":   "123",
					"user_domain_name": "acme",
					"user_id":          "345",
					"user_name":        "coyote",
				},
				Roles: []string{
					"admin",
					"member",
				},
			},
			Serialized: `{"v":1,"d":["123","acme"],"u":["345","coyote"],"r":["admin","member"]}`,
		},
		// domain scope with user from a different domain
		{
			Context: policy.Context{
				Auth: map[string]string{
					"domain_id":        "123",
					"domain_name":      "acme",
					"user_domain_id":   "default",
					"user_domain_name": "Default",
					"user_id":          "012",
					"user_name":        "admin",
				},
				Roles: []string{
					"admin",
				},
			},
			Serialized: `{"v":1,"d":["123","acme"],"u":["012","admin"],"ud":["default","Default"],"r":["admin"]}`,
		},
		// system scope
		{
			Context: policy.Context{
				Auth: map[string]string{
					"user_domain_id":   "default",
					"user_domain_name": "Default",
					"user_id":          "012",
					"user_name":        "admin",
				},
				Roles: []string{},
			},
			Serialized: `{"v":1,"u":["012","admin"],"ud":["default","Default"],"r":[]}`,
		},
	}

	for _, tc := range testCases {
		// test serialization
		buf, err := SerializeCompactContextToJSON(tc.Context)
		if err != nil {
			t.Errorf("unexpected error in SerializeCompactContextToJSON(%#v): %s", tc.Context, err.Error())
			continue
		}
		assert.DeepEqual(t, fmt.Sprintf("SerializeCompactContextToJSON(%#v)", tc.Context), string(buf), tc.Serialized)

		// test deserialization
		parsed, err := DeserializeCompactContextFromJSON([]byte(tc.Serialized))
		if err != nil {
			t.Errorf("unexpected error in DeserializeCompactContextFromJSON(%q): %s", tc.Serialized, err.Error())
		}
		assert.DeepEqual(t, fmt.Sprintf("DeserializeCompactContextFromJSON(%q)", tc.Serialized), parsed, tc.Context)
	}
}