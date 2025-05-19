// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// This needs to be in a separate package to allow importing go-bits/assert
// without causing an import loop.
package osext_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/osext"
)

const KEY = "GOBITS_OSENV_FOO"
const VAL = "this is an example value"
const DEFAULT = "some default value"

func TestGetenv(t *testing.T) {
	// test with string value
	t.Setenv(KEY, VAL)

	str, err := osext.NeedGetenv(KEY)
	assert.DeepEqual(t, "result from NeedGetenv", str, VAL)
	assert.DeepEqual(t, "error from NeedGetenv", err, nil)

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.DeepEqual(t, "result from GetenvOrDefault", str, VAL)

	ok := osext.GetenvBool(KEY)
	assert.DeepEqual(t, "result from GetenvBool", ok, false) // not a valid boolean literal -> false

	// test with empty value
	t.Setenv(KEY, "")

	_, err = osext.NeedGetenv(KEY)
	assert.DeepEqual(t, "error from NeedGetenv", err, error(osext.MissingEnvError{Key: KEY}))

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.DeepEqual(t, "result from GetenvOrDefault", str, DEFAULT)

	ok = osext.GetenvBool(KEY)
	assert.DeepEqual(t, "result from GetenvBool", ok, false)

	// test with null value
	os.Unsetenv(KEY)

	_, err = osext.NeedGetenv(KEY)
	assert.DeepEqual(t, "error from NeedGetenv", err, error(osext.MissingEnvError{Key: KEY}))

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.DeepEqual(t, "result from GetenvOrDefault", str, DEFAULT)

	ok = osext.GetenvBool(KEY)
	assert.DeepEqual(t, "result from GetenvBool", ok, false)

	// test GetenvBool with explicitly true-ish values
	for _, value := range []string{"t", "True", "1"} {
		t.Setenv(KEY, value)
		ok = osext.GetenvBool(KEY)
		msg := fmt.Sprintf("result from GetenvBool for %q", value)
		assert.DeepEqual(t, msg, ok, true)
	}

	// test GetenvBool with explicitly false-ish values
	for _, value := range []string{"f", "False", "0"} {
		t.Setenv(KEY, value)
		ok = osext.GetenvBool(KEY)
		msg := fmt.Sprintf("result from GetenvBool for %q", value)
		assert.DeepEqual(t, msg, ok, false)
	}
}
