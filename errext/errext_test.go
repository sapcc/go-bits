/*******************************************************************************
*
* Copyright 2023 SAP SE
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

package errext

import (
	"fmt"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestAsAndIs(t *testing.T) {
	err1 := error(fooError{23})

	//unwrapped error can be type-casted
	ferr, ok := As[fooError](err1)
	assert.DeepEqual(t, "As", ferr, err1)
	assert.DeepEqual(t, "As", ok, true)
	ok = IsOfType[fooError](err1)
	assert.DeepEqual(t, "As", ok, true)

	//unwrapped error cannot be type-casted into incompatible type
	_, ok = As[barError](err1) //nolint:errcheck
	assert.DeepEqual(t, "As", ok, false)
	ok = IsOfType[barError](err1)
	assert.DeepEqual(t, "As", ok, false)

	err2 := fmt.Errorf("operation failed: %w", err1)

	//wrapped error can be type-casted
	ferr, ok = As[fooError](err2)
	assert.DeepEqual(t, "As", ferr, err1)
	assert.DeepEqual(t, "As", ok, true)
	ok = IsOfType[fooError](err1)
	assert.DeepEqual(t, "As", ok, true)

	//wrapped error cannot be type-casted into incompatible type
	_, ok = As[barError](err2) //nolint:errcheck
	assert.DeepEqual(t, "As", ok, false)
	ok = IsOfType[barError](err1)
	assert.DeepEqual(t, "As", ok, false)

	//nil error cannot be type-casted at all
	_, ok = As[fooError](nil) //nolint:errcheck
	assert.DeepEqual(t, "As", ok, false)
	ok = IsOfType[fooError](nil)
	assert.DeepEqual(t, "As", ok, false)
}

type fooError struct{ Data int }
type barError struct{ Data int }

func (fooError) Error() string { return "foo" }
func (barError) Error() string { return "bar" }
