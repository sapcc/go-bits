// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package errext

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestErrorSetAdd(t *testing.T) {
	var errs ErrorSet

	// Add nil error - should not be added
	errs.Add(nil)
	assert.Equal(t, len(errs), 0)

	// Add non-nil error
	err1 := errors.New("error 1")
	errs.Add(err1)
	assert.Equal(t, len(errs), 1)
	assert.Equal(t, errs[0].Error(), "error 1")

	// Add another non-nil error
	err2 := errors.New("error 2")
	errs.Add(err2)
	assert.Equal(t, len(errs), 2)
	assert.Equal(t, errs[1].Error(), "error 2")

	// Add nil error again - should not be added
	errs.Add(nil)
	assert.Equal(t, len(errs), 2)
}

func TestErrorSetAddf(t *testing.T) {
	var errs ErrorSet

	// Add formatted error
	errs.Addf("error with number: %d", 42)
	assert.Equal(t, len(errs), 1)
	assert.Equal(t, errs[0].Error(), "error with number: 42")

	// Add another formatted error
	errs.Addf("error with string: %s", "test")
	assert.Equal(t, len(errs), 2)
	assert.Equal(t, errs[1].Error(), "error with string: test")

	// Add formatted error without parameters
	errs.Addf("simple error")
	assert.Equal(t, len(errs), 3)
	assert.Equal(t, errs[2].Error(), "simple error")
}

func TestErrorSetAppend(t *testing.T) {
	var errs1 ErrorSet
	errs1.Add(errors.New("error 1"))
	errs1.Add(errors.New("error 2"))

	var errs2 ErrorSet
	errs2.Add(errors.New("error 3"))
	errs2.Add(errors.New("error 4"))

	// Append errs2 to errs1
	errs1.Append(errs2)
	assert.Equal(t, len(errs1), 4)
	assert.Equal(t, errs1[0].Error(), "error 1")
	assert.Equal(t, errs1[1].Error(), "error 2")
	assert.Equal(t, errs1[2].Error(), "error 3")
	assert.Equal(t, errs1[3].Error(), "error 4")

	// Append empty ErrorSet
	var errs3 ErrorSet
	errs1.Append(errs3)
	assert.Equal(t, len(errs1), 4)
}

func TestErrorSetIsEmpty(t *testing.T) {
	var errs ErrorSet

	// Empty ErrorSet
	assert.Equal(t, errs.IsEmpty(), true)

	// Add an error
	errs.Add(errors.New("error"))
	assert.Equal(t, errs.IsEmpty(), false)

	// Create new ErrorSet with errors
	errs2 := ErrorSet{errors.New("error 1"), errors.New("error 2")}
	assert.Equal(t, errs2.IsEmpty(), false)
}

func TestErrorSetJoin(t *testing.T) {
	var errs ErrorSet

	// Empty ErrorSet
	result := errs.Join(", ")
	assert.Equal(t, result, "")

	// Single error
	errs.Add(errors.New("error 1"))
	result = errs.Join(", ")
	assert.Equal(t, result, "error 1")

	// Multiple errors with comma separator
	errs.Add(errors.New("error 2"))
	errs.Add(errors.New("error 3"))
	result = errs.Join(", ")
	assert.Equal(t, result, "error 1, error 2, error 3")

	// Multiple errors with different separator
	result = errs.Join(" | ")
	assert.Equal(t, result, "error 1 | error 2 | error 3")

	// Multiple errors with newline separator
	result = errs.Join("\n")
	assert.Equal(t, result, "error 1\nerror 2\nerror 3")
}

func TestErrorSetJoinedError(t *testing.T) {
	var errs ErrorSet

	// Empty ErrorSet
	joined := errs.JoinedError(", ")
	assert.Equal(t, joined.Error(), "")

	// Single error
	errs.Add(errors.New("error 1"))
	joined = errs.JoinedError(", ")
	assert.Equal(t, joined.Error(), "error 1")

	// Multiple errors
	errs.Add(errors.New("error 2"))
	errs.Add(errors.New("error 3"))
	joined = errs.JoinedError("; ")
	assert.Equal(t, joined.Error(), "error 1; error 2; error 3")
}

func TestJoinedErrorError(t *testing.T) {
	// Test the Error() method of joinedError directly
	je := joinedError{
		errs:      []error{errors.New("error 1"), errors.New("error 2")},
		separator: " - ",
	}
	assert.Equal(t, je.Error(), "error 1 - error 2")

	// Empty error list
	je2 := joinedError{
		errs:      []error{},
		separator: ", ",
	}
	assert.Equal(t, je2.Error(), "")
}

func TestJoinedErrorUnwrap(t *testing.T) {
	err1 := errors.New("error 1")
	err2 := errors.New("error 2")
	err3 := errors.New("error 3")

	var errs ErrorSet
	errs.Add(err1)
	errs.Add(err2)
	errs.Add(err3)

	joined := errs.JoinedError(", ")

	// Test that errors.Is correctly identifies wrapped errors
	assert.Equal(t, errors.Is(joined, err1), true)
	assert.Equal(t, errors.Is(joined, err2), true)
	assert.Equal(t, errors.Is(joined, err3), true)
	assert.Equal(t, errors.Is(joined, errors.New("different error")), false)
}

func TestErrorSetLogFatalIfError(t *testing.T) {
	// Test with empty ErrorSet - should not exit
	var emptyErrs ErrorSet
	// We can't directly test os.Exit, but we can verify the function doesn't panic
	// on empty ErrorSet. In a real scenario, this would not call os.Exit.
	// Note: We cannot fully test LogFatalIfError as it calls os.Exit(1)
	// For an empty ErrorSet, it should complete without exiting
	testLogFatalWithEmptyErrorSet(emptyErrs)
}

func testLogFatalWithEmptyErrorSet(errs ErrorSet) {
	// This function is a helper to test that LogFatalIfError doesn't panic
	// on empty ErrorSet. We cannot test the actual exit behavior.
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprintf("LogFatalIfError panicked: %v", r))
		}
	}()

	// For empty ErrorSet, this should complete without issues
	if errs.IsEmpty() {
		// We skip calling LogFatalIfError on non-empty sets as it would exit
		errs.LogFatalIfError()
	}
}

func TestErrorSetIntegration(t *testing.T) {
	// Integration test combining multiple methods
	var errs ErrorSet

	// Start empty
	assert.Equal(t, errs.IsEmpty(), true)

	// Add some errors using different methods
	errs.Add(errors.New("error 1"))
	errs.Addf("error %d", 2)

	assert.Equal(t, errs.IsEmpty(), false)
	assert.Equal(t, len(errs), 2)

	// Create another ErrorSet and append
	var moreErrs ErrorSet
	moreErrs.Add(errors.New("error 3"))
	moreErrs.Addf("error %d", 4)

	errs.Append(moreErrs)
	assert.Equal(t, len(errs), 4)

	// Test Join
	result := errs.Join(" | ")
	assert.Equal(t, result, "error 1 | error 2 | error 3 | error 4")

	// Test JoinedError
	joined := errs.JoinedError("; ")
	assert.Equal(t, joined.Error(), "error 1; error 2; error 3; error 4")
}
