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

package regexpext

import (
	"testing"

	. "github.com/majewsky/gg/option"

	"github.com/sapcc/go-bits/assert"
)

func TestConfigSetPickWithLiterals(t *testing.T) {
	cs := ConfigSet[string, int]{
		{Key: "foo", Value: 42},
		{Key: "bar", Value: 23},
	}

	assert.DeepEqual(t, `cs.Pick("foo")`, cs.Pick("foo"), Some(42))
	assert.DeepEqual(t, `cs.Pick("bar")`, cs.Pick("bar"), Some(23))
	assert.DeepEqual(t, `cs.Pick("qux")`, cs.Pick("qux"), None[int]())
}

func TestConfigSetPickWithRegex(t *testing.T) {
	cs := ConfigSet[string, int]{
		{Key: "foo|bar", Value: 42},
		{Key: "bar", Value: 23},
	}

	assert.DeepEqual(t, `cs.Pick("foo")`, cs.Pick("foo"), Some(42))
	assert.DeepEqual(t, `cs.Pick("bar")`, cs.Pick("bar"), Some(42)) // first match wins!
	assert.DeepEqual(t, `cs.Pick("qux")`, cs.Pick("qux"), None[int]())
	assert.DeepEqual(t, `cs.Pick("foooo")`, cs.Pick("foooo"), None[int]()) // regex matches full string only
}

func TestConfigSetWithFill(t *testing.T) {
	type Name struct {
		FirstName string
		LastName  string
	}
	fill := func(value *Name, expand func(string) string) {
		value.FirstName = expand(value.FirstName)
		value.LastName = expand(value.LastName)
	}

	cs := ConfigSet[string, Name]{
		{Key: `(J\w*) (D\w*)`, Value: Name{FirstName: "$1", LastName: "$2"}},
		{Key: "Bob", Value: Name{FirstName: "Bob", LastName: "Mc$1"}},
	}

	value := cs.PickAndFill("Jane Doe", fill)
	assert.DeepEqual(t, `cs.PickAndFill("Jane Doe")`, value, Some(Name{FirstName: "Jane", LastName: "Doe"}))

	// expand from the same template again, but with different values (this tests that the template was not modified)
	value = cs.PickAndFill("John Dorian", fill)
	assert.DeepEqual(t, `cs.PickAndFill("John Dorian")`, value, Some(Name{FirstName: "John", LastName: "Dorian"}))

	// unknown capture groups expand to empty strings, same as regexp.ExpandString()
	value = cs.PickAndFill("Bob", fill)
	assert.DeepEqual(t, `cs.PickAndFill("Bob")`, value, Some(Name{FirstName: "Bob", LastName: "Mc"}))
}
