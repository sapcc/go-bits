// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// This needs to be in a separate package to allow importing go-bits/assert
// without causing an import loop.
package osext_test

import (
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
	assert.Equal(t, str, VAL)
	assert.Equal(t, err, nil)

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.Equal(t, str, VAL)

	ok := osext.GetenvBool(KEY)
	assert.Equal(t, ok, false) // not a valid boolean literal -> false

	// test with empty value
	t.Setenv(KEY, "")

	_, err = osext.NeedGetenv(KEY)
	assert.Equal(t, err, error(osext.MissingEnvError{Key: KEY}))

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.Equal(t, str, DEFAULT)

	ok = osext.GetenvBool(KEY)
	assert.Equal(t, ok, false)

	// test with null value
	os.Unsetenv(KEY)

	_, err = osext.NeedGetenv(KEY)
	assert.Equal(t, err, error(osext.MissingEnvError{Key: KEY}))

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.Equal(t, str, DEFAULT)

	ok = osext.GetenvBool(KEY)
	assert.Equal(t, ok, false)

	// test GetenvBool with explicitly true-ish values
	for _, value := range []string{"t", "True", "1"} {
		t.Logf("testing GetenvBool for %q", value)
		t.Setenv(KEY, value)
		assert.Equal(t, osext.GetenvBool(KEY), true)
	}

	// test GetenvBool with explicitly false-ish values
	for _, value := range []string{"f", "False", "0"} {
		t.Logf("testing GetenvBool for %q", value)
		t.Setenv(KEY, value)
		assert.Equal(t, osext.GetenvBool(KEY), false)
	}
}
