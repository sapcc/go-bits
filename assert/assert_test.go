// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/internal/testutil"
)

func TestErrEqual(t *testing.T) {
	var (
		actual error
		mock   = &testutil.MockT{}
	)
	checkPasses := func(expected any) {
		t.Helper()
		ok := assert.ErrEqual(mock, actual, expected)
		assert.Equal(t, ok, true)
		mock.ExpectNoErrors(t)
	}
	checkFails := func(expected any, message string) {
		t.Helper()
		ok := assert.ErrEqual(mock, actual, expected)
		assert.Equal(t, ok, false)
		mock.ExpectErrors(t, message)
	}

	// some helper errors for below
	errFoo := errors.New("wrong foo supplied")
	errBar := fmt.Errorf("could not connect to bar: %w", errFoo)
	errQux := errors.New("found no relation from qux to foo/bar")

	// check assertions for when the actual error is nil
	actual = nil
	checkPasses(nil)
	checkPasses(error(nil))
	checkFails(errFoo, `expected error stack to contain "wrong foo supplied", but got no error`)
	checkPasses("")
	checkFails("datacenter on fire", `expected error with message "datacenter on fire", but got no error`)
	checkFails(regexp.MustCompile(`.*`), `expected error with message matching /.*/, but got no error`)

	// check assertions with a simple error
	actual = errFoo
	checkFails(nil, `expected success, but got error: wrong foo supplied`)
	checkFails(error(nil), `expected success, but got error: wrong foo supplied`)
	checkPasses(errFoo)
	checkFails(errBar, `expected error stack to contain "could not connect to bar: wrong foo supplied", but got error: wrong foo supplied`)
	checkFails(errQux, `expected error stack to contain "found no relation from qux to foo/bar", but got error: wrong foo supplied`)
	checkFails("", `expected success, but got error: wrong foo supplied`)
	checkPasses("wrong foo supplied")
	checkFails("datacenter on fire", `expected error with message "datacenter on fire", but got error: wrong foo supplied`)
	checkPasses(regexp.MustCompile(`wrong fo* supplied`))
	checkFails(regexp.MustCompile(`connect to bar`), `expected error with message matching /connect to bar/, but got error: wrong foo supplied`)

	// check assertions with an error stack
	actual = errBar
	checkFails(nil, `expected success, but got error: could not connect to bar: wrong foo supplied`)
	checkFails(error(nil), `expected success, but got error: could not connect to bar: wrong foo supplied`)
	checkPasses(errFoo) // both with the contained error...
	checkPasses(errBar) // ...as well as with the full error
	checkFails(errQux, `expected error stack to contain "found no relation from qux to foo/bar", but got error: could not connect to bar: wrong foo supplied`)
	checkFails("", `expected success, but got error: could not connect to bar: wrong foo supplied`)
	checkFails("wrong foo supplied", `expected error with message "wrong foo supplied", but got error: could not connect to bar: wrong foo supplied`)
	checkPasses("could not connect to bar: wrong foo supplied")
	checkFails("datacenter on fire", `expected error with message "datacenter on fire", but got error: could not connect to bar: wrong foo supplied`)
	checkPasses(regexp.MustCompile(`wrong fo* supplied`))
	checkPasses(regexp.MustCompile(`connect to bar`))
}
