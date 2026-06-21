// SPDX-FileCopyrightText: 2017 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

// TODO: deprecate all functions in this package (once the majority of uses have been converted)
import gg_assert "go.xyrillian.de/gg/assert"

// Equal checks if the actual and expected value are equal according to == rules, and t.Errors() otherwise.
func Equal[V comparable](t gg_assert.TestingTB, actual, expected V) bool {
	t.Helper()
	return gg_assert.Equal(t, actual, expected)
}

// ErrEqual checks if the actual error matches the expectation.
//
//   - If `expected` is nil, the actual error must be nil.
//   - If `expected` is of type error, the actual error must be exactly equal to it, or contain it in the sense of errors.Is().
//   - If `expected` is of type string, the actual error message must be exactly equal to it.
//   - If `expected` is of type *regexp.Regexp, that regexp must match the actual error message.
func ErrEqual(t gg_assert.TestingTB, actual error, expectedErrorOrMessageOrRegexp any) bool {
	t.Helper()
	return gg_assert.ErrEqual(t, actual, expectedErrorOrMessageOrRegexp)
}

// DeepEqual checks if the actual and expected value are equal as
// determined by reflect.DeepEqual(), and t.Error()s otherwise.
func DeepEqual[V any](t gg_assert.TestingTB, variable string, actual, expected V) bool {
	t.Helper()
	return gg_assert.Equal(t, actual, expected)
}
