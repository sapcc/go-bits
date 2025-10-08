// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"fmt"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

// A mock for *testing.T that implements the assert.TestingT interface.
type MockT struct {
	Errors []string
}

func (mt *MockT) Helper() {}

func (mt *MockT) Errorf(msg string, args ...any) {
	mt.Errors = append(mt.Errors, fmt.Sprintf(msg, args...))
}

// ExpectErrors asserts on the errors collected so far,
// and then clears out the list of collected errors for the next subtest.
func (mt *MockT) ExpectErrors(t *testing.T, expected ...string) {
	t.Helper()
	assert.DeepEqual(t, "collected errors", mt.Errors, expected)
	mt.Errors = nil
}

// ExpectErrors asserts that no errors were collected so far.
func (mt *MockT) ExpectNoErrors(t *testing.T) {
	t.Helper()
	assert.DeepEqual(t, "collected errors", mt.Errors, []string(nil))
}
