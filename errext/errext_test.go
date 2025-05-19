// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package errext

import (
	"fmt"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestAsAndIs(t *testing.T) {
	err1 := error(fooError{23})

	// unwrapped error can be type-casted
	ferr, ok := As[fooError](err1)
	assert.DeepEqual(t, "As", ferr.Data, 23)
	assert.DeepEqual(t, "As", ok, true)
	ok = IsOfType[fooError](err1)
	assert.DeepEqual(t, "As", ok, true)

	// unwrapped error cannot be type-casted into incompatible type
	_, ok = As[barError](err1) //nolint:errcheck
	assert.DeepEqual(t, "As", ok, false)
	ok = IsOfType[barError](err1)
	assert.DeepEqual(t, "As", ok, false)

	err2 := fmt.Errorf("operation failed: %w", err1)

	// wrapped error can be type-casted
	ferr, ok = As[fooError](err2)
	assert.DeepEqual(t, "As", ferr.Data, 23)
	assert.DeepEqual(t, "As", ok, true)
	ok = IsOfType[fooError](err1)
	assert.DeepEqual(t, "As", ok, true)

	// wrapped error cannot be type-casted into incompatible type
	_, ok = As[barError](err2) //nolint:errcheck
	assert.DeepEqual(t, "As", ok, false)
	ok = IsOfType[barError](err1)
	assert.DeepEqual(t, "As", ok, false)

	// nil error cannot be type-casted at all
	_, ok = As[fooError](nil) //nolint:errcheck
	assert.DeepEqual(t, "As", ok, false)
	ok = IsOfType[fooError](nil)
	assert.DeepEqual(t, "As", ok, false)
}

type fooError struct{ Data int }
type barError struct{ Data int }

func (fooError) Error() string { return "foo" }
func (barError) Error() string { return "bar" }
