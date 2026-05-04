// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httptest

import (
	"bytes"
	"os"
	"testing"

	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/go-bits/assert"
)

func expectNoDiffs(t *testing.T, diffs []jsonmatch.Diff) {
	t.Helper()
	for _, diff := range diffs {
		t.Error(diff.String())
	}
}

func TestToDiffable(t *testing.T) {
	// no transformations
	diffable := NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableWithDotModification").Modify(".").Modify(".")
	originalJSON, err := os.ReadFile("fixtures/example.json")
	if err != nil {
		t.Error("could not read original test fixture file")
	}
	expectNoDiffs(t, diffable.DiffAgainst(originalJSON))

	// no modifications at all
	diffable = NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableWithoutModifications")
	expectNoDiffs(t, diffable.DiffAgainst(originalJSON))

	// 2 deletions in a row
	diffable = NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableWithChainedDeletions").Modify(
		`del(.remove_me)`, `del(.also_remove)`,
	)
	expected := bytes.Replace(originalJSON, []byte(`,
  "remove_me": "should be deleted",
  "also_remove": "should also be deleted"`), []byte(""), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// 2 deletions with 2 modifications
	diffable = NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableWithChainedModifications").Modify(
		`del(.remove_me, .also_remove)`,
		`.string_field = "modified"`,
		`.nested_object.inner_string = "changed"`,
	)
	expected = bytes.Replace(expected, []byte(`  "string_field": "hello",`), []byte(`  "string_field": "modified",`), 1)
	expected = bytes.Replace(expected, []byte(`    "inner_string": "world",`), []byte(`    "inner_string": "changed",`), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// int overflow
	diffable = NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableWithIntOverflow").Modify(
		`. + {"big": (9999999999999999999 * 9999999999999999999)}`,
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to convert modifications to jsonmatch for test TestToDiffableWithIntOverflow: received unsupported type from gojq *big.Int): expected <unknown>, but got something`,
	)

	// multiple result statements
	diffable = NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableWithMultipleResults").Modify(
		".string_field, .int_field",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (modifications which produce multiple results are not supported): expected <unknown>, but got something`,
	)

	// non-existent file
	diffable = NewJQModifiableJSONFixture("fixtures/nonexistent.json", "TestToDiffableFileNotFound").Modify(
		".",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to read fixture file fixtures/nonexistent.json: open fixtures/nonexistent.json: no such file or directory): expected <unknown>, but got something`,
	)

	// invalid json
	diffable = NewJQModifiableJSONFixture("fixtures/example.txt", "TestToDiffableInvalidJSON").Modify(
		".",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to parse fixture file fixtures/example.txt: invalid character 'H' looking for beginning of value): expected <unknown>, but got something`,
	)

	// invalid jq expression
	diffable = NewJQModifiableJSONFixture("fixtures/example.json", "TestToDiffableInvalidJQ").Modify(
		"invalid jq [[[ syntax",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to parse query for test TestToDiffableInvalidJQ: unexpected token "jq"): expected <unknown>, but got something`,
	)
}
