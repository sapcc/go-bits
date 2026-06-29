// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httptest_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"go.xyrillian.de/gg/assert"
	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/must"
)

func expectNoDiffs(t *testing.T, diffs []jsonmatch.Diff) {
	t.Helper()
	for _, diff := range diffs {
		t.Error(diff.String())
	}
}

func TestJQModifiableJSONFixtureSimple(t *testing.T) {
	// no transformations
	diffable := httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture with dot modification").Modify(".").Modify(".")
	originalJSON, err := os.ReadFile("fixtures/example.json")
	if err != nil {
		t.Error("could not read original test fixture file")
	}
	expectNoDiffs(t, diffable.DiffAgainst(originalJSON))

	// no modifications at all
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture without modification")
	expectNoDiffs(t, diffable.DiffAgainst(originalJSON))

	// 2 deletions in a row
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture with chained deletions").Modify(
		`del(.remove_me)`, `del(.also_remove)`,
	)
	expected := bytes.Replace(originalJSON, []byte(`  "remove_me": "should be deleted",
`), []byte(""), 1)
	expected = bytes.Replace(expected, []byte(`  "also_remove": "should also be deleted",
`), []byte(""), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// 2 deletions with 2 modifications
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture with chained modifications").Modify(
		`del(.remove_me, .also_remove)`,
		`.string_field = "modified"`,
		`.nested_object.inner_string = "changed"`,
	)
	expected = bytes.Replace(expected, []byte(`  "string_field": "hello"`), []byte(`  "string_field": "modified"`), 1)
	expected = bytes.Replace(expected, []byte(`    "inner_string": "world"`), []byte(`    "inner_string": "changed"`), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// check that the serialization works
	assert.Equal(t, must.ReturnT(json.Marshal(diffable))(t), bytes.ReplaceAll(bytes.ReplaceAll(expected, []byte("\n"), []byte("")), []byte(" "), []byte("")))

	// int overflow
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture with int overflow").Modify(
		`. + {"big": (9999999999999999999 * 9999999999999999999)}`,
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to process fixture file fixtures/example.json: failed to convert modifications to jsonmatch for test "TestJQModifiableJSONFixture with int overflow": received unsupported type from gojq *big.Int): expected <unknown>, but got something`,
	)

	// multiple result statements
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture with multiple results").Modify(
		".string_field, .int_field",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to process fixture file fixtures/example.json: failed to apply modifications for test "TestJQModifiableJSONFixture with multiple results": modifications which produce multiple results are not supported): expected <unknown>, but got something`,
	)

	// non-existent file
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/nonexistent.json", "TestJQModifiableJSONFixture file not found").Modify(
		".",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to read fixture file fixtures/nonexistent.json: open fixtures/nonexistent.json: no such file or directory): expected <unknown>, but got something`,
	)

	// invalid json
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.txt", "TestJQModifiableJSONFixture invalid json").Modify(
		".",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to process fixture file fixtures/example.txt: failed to parse input for test "TestJQModifiableJSONFixture invalid json": invalid character 'H' looking for beginning of value): expected <unknown>, but got something`,
	)

	// invalid jq expression
	diffable = httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableJSONFixture invalid jq").Modify(
		"invalid jq [[[ syntax",
	)
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`fixture processing error (failed to process fixture file fixtures/example.json: failed to parse query for test "TestJQModifiableJSONFixture invalid jq": unexpected token "jq"): expected <unknown>, but got something`,
	)
}

const testString = `
	{
		"string_field": "hello",
		"int_field": 42,
		"float_field": 3.14,
		"bool_true": true,
		"bool_false": false,
		"null_field": null,
		"nested_object": {
			"inner_string": "world",
			"inner_int": 7
		},
		"array_field": [1, "two", true, null, 4.5],
		"nested_array": [[1, 2], [3, 4]],
		"object_in_array": [{"name": "alice"}, {"name": "bob"}],
		"remove_me": "should be deleted",
		"also_remove": "should also be deleted"
	}`

func TestJQModifiableJSONStringSimple(t *testing.T) {
	// These are just simple cases because the code is actually shared
	// no transformations

	diffable := httptest.NewJQModifiableJSONString(testString, "TestJQModifiableJSONString with dot modification").Modify(".").Modify(".")
	originalJSON, err := os.ReadFile("fixtures/example.json")
	if err != nil {
		t.Error("could not read original test fixture file")
	}
	expectNoDiffs(t, diffable.DiffAgainst(originalJSON))

	// no modifications at all
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableJSONString without modifications")
	expectNoDiffs(t, diffable.DiffAgainst(originalJSON))

	// 2 deletions in a row
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableJSONString with chained deletions").Modify(
		`del(.remove_me)`, `del(.also_remove)`,
	)
	expected := bytes.Replace(originalJSON, []byte(`  "remove_me": "should be deleted",
`), []byte(""), 1)
	expected = bytes.Replace(expected, []byte(`  "also_remove": "should also be deleted",
`), []byte(""), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// check that the serialization works
	assert.Equal(t, must.ReturnT(json.Marshal(diffable))(t), bytes.ReplaceAll(bytes.ReplaceAll(expected, []byte("\n"), []byte("")), []byte(" "), []byte("")))
}

func TestJQModifiableContentWithVariables(t *testing.T) {
	// assemble a JSON fixture where we place strings in a few places
	originalJSON, err := os.ReadFile("fixtures/example.json")
	if err != nil {
		t.Error("could not read original test fixture file")
	}
	expected := bytes.Replace(originalJSON, []byte(`"bool_false": false,
`), []byte(`"bool_false": false,
  "bool_false2": false,
`), 1)
	expected = bytes.Replace(expected, []byte(`"inner_string": "world"
`), []byte(`"inner_string": "world",
    "inner_string2": "space"
`), 1)

	diffable := httptest.NewJQModifiableJSONFixture("fixtures/example.json", "TestJQModifiableContentWithVariables deep merge from fixture").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONString(`{"bool_false2": false, "nested_object": {"inner_string2": "space"}}`))
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// add deletions
	diffable = diffable.Modify(`del(.remove_me)`, `del(.also_remove)`)
	expectedWithDeletion := bytes.Replace(expected, []byte(`  "remove_me": "should be deleted",
`), []byte(""), 1)
	expectedWithDeletion = bytes.Replace(expectedWithDeletion, []byte(`  "also_remove": "should also be deleted",
`), []byte(""), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expectedWithDeletion))

	// check when starting from the string and adding a json to it
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables deep merge from string").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/modification.json"))
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// add deletions
	diffable = diffable.Modify(`del(.remove_me)`, `del(.also_remove)`)
	expectNoDiffs(t, diffable.DiffAgainst(expectedWithDeletion))

	// check multiple modifications work (they can be no-op)
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables deep merge from string").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/modification.json")).
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/modification.json")).
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/modification.json"))
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// place the modification a second time within the nested object and add a normal modification
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables deep merge from string").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/modification.json")).
		ModifyWithVariable(".nested_object |= . * $ref", httptest.JQUnmodifiedJSONString(`{"nested_object2":{"inner_string3": "milkyway"}}`)).
		Modify(`del(.remove_me)`, `del(.also_remove)`)
	expected = bytes.Replace(expectedWithDeletion, []byte(`"inner_string2": "space"
`), []byte(`"inner_string2": "space",
    "nested_object2": {
      "inner_string3": "milkyway"
    }
`), 1)
	expectNoDiffs(t, diffable.DiffAgainst(expected))

	// check that the serialization works
	assert.Equal(t, must.ReturnT(json.Marshal(diffable))(t), bytes.ReplaceAll(bytes.ReplaceAll(expected, []byte("\n"), []byte("")), []byte(" "), []byte("")))

	// non-existent file
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables file not found").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/nonexistent.json"))
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`JSON string processing error (failed to read data for $ref #1 for test "TestJQModifiableContentWithVariables file not found": failed to read fixture file fixtures/nonexistent.json: open fixtures/nonexistent.json: no such file or directory): expected <unknown>, but got something`,
	)

	// invalid json
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables invalid json").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/example.txt"))
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`JSON string processing error (failed to read data for $ref #1 for test "TestJQModifiableContentWithVariables invalid json": failed to process fixture file fixtures/example.txt: failed to parse input: invalid character 'H' looking for beginning of value): expected <unknown>, but got something`,
	)

	// invalid json string
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables invalid json string").
		ModifyWithVariable(". * $ref", httptest.JQUnmodifiedJSONString("bla"))
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`JSON string processing error (failed to read data for $ref #1 for test "TestJQModifiableContentWithVariables invalid json string": failed to parse input: invalid character 'b' looking for beginning of value): expected <unknown>, but got something`,
	)

	// wrong number of refs
	diffable = httptest.NewJQModifiableJSONString(testString, "TestJQModifiableContentWithVariables invalid modification").
		ModifyWithVariable(". * $ref * $ref", httptest.JQUnmodifiedJSONFixture("fixtures/modification.json"))
	assert.Equal(t, diffable.DiffAgainst([]byte("something"))[0].String(),
		`JSON string processing error (different number of $ref used than provided for test "TestJQModifiableContentWithVariables invalid modification": 2 used, 1 provided): expected <unknown>, but got something`,
	)
}
