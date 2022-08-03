/******************************************************************************
*
*  Copyright 2022 SAP SE
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
	//test with string value
	os.Setenv(KEY, VAL)

	str, err := osext.NeedGetenv(KEY)
	assert.DeepEqual(t, "result from NeedGetenv", str, VAL)
	assert.DeepEqual(t, "error from NeedGetenv", err, nil)

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.DeepEqual(t, "result from GetenvOrDefault", str, VAL)

	ok := osext.GetenvBool(KEY)
	assert.DeepEqual(t, "result from GetenvBool", ok, false) //not a valid boolean literal -> false

	//test with empty value
	os.Setenv(KEY, "")

	_, err = osext.NeedGetenv(KEY)
	assert.DeepEqual(t, "error from NeedGetenv", err, osext.MissingEnvError{Key: KEY})

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.DeepEqual(t, "result from GetenvOrDefault", str, DEFAULT)

	ok = osext.GetenvBool(KEY)
	assert.DeepEqual(t, "result from GetenvBool", ok, false)

	//test with null value
	os.Unsetenv(KEY)

	_, err = osext.NeedGetenv(KEY)
	assert.DeepEqual(t, "error from NeedGetenv", err, osext.MissingEnvError{Key: KEY})

	str = osext.GetenvOrDefault(KEY, DEFAULT)
	assert.DeepEqual(t, "result from GetenvOrDefault", str, DEFAULT)

	ok = osext.GetenvBool(KEY)
	assert.DeepEqual(t, "result from GetenvBool", ok, false)

	//test GetenvBool with explicitly true-ish values
	for _, value := range []string{"t", "True", "1"} {
		os.Setenv(KEY, value)
		ok = osext.GetenvBool(KEY)
		msg := fmt.Sprintf("result from GetenvBool for %q", value)
		assert.DeepEqual(t, msg, ok, true)
	}

	//test GetenvBool with explicitly false-ish values
	for _, value := range []string{"f", "False", "0"} {
		os.Setenv(KEY, value)
		ok = osext.GetenvBool(KEY)
		msg := fmt.Sprintf("result from GetenvBool for %q", value)
		assert.DeepEqual(t, msg, ok, false)
	}
}
