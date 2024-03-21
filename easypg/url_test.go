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

package easypg

import (
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestURLFrom(t *testing.T) {
	// replace os.Hostname() with a test double
	osHostname = func() (string, error) {
		return "testhostname", nil
	}

	// check a URL with everything set
	url, err := URLFrom(URLParts{
		HostName:          "localhost",
		Port:              "5432",
		UserName:          "foouser",
		Password:          "foopass",
		ConnectionOptions: "sslmode=disable",
		DatabaseName:      "foodb",
	})
	if err != nil {
		t.Fatal(err.Error())
	}
	expected := "postgres://foouser:foopass@localhost:5432/foodb?application_name=go-bits%40testhostname&sslmode=disable"
	assert.DeepEqual(t, "URLFrom result with everything included", url.String(), expected)

	// check a URL with optional parts omitted
	url, err = URLFrom(URLParts{
		HostName:     "localhost",
		UserName:     "foouser",
		DatabaseName: "foodb",
	})
	if err != nil {
		t.Fatal(err.Error())
	}
	expected = "postgres://foouser@localhost/foodb?application_name=go-bits%40testhostname"
	assert.DeepEqual(t, "URLFrom result with optional parts omitted", url.String(), expected)
}
