// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/majewsky/gg/option"
	"github.com/sapcc/go-bits/must"
)

func TestJSONMatchCanonicalizesActualPayload(t *testing.T) {
	testCases := []string{
		// all of these are functionally identical, so they should produce an empty diff
		// against our expectations regardless of key order and whitespace
		`{"data": {"qux":[5,null,15], "foo": 42, "bar": "hello world"}}`,
		`{"data":{"bar":"hello world","foo":42,"qux":[5,null,15]}}`,
		`{
			"data": {
				"bar": "hello world",
				"qux": [
					5,
					null,
					15
				],
				"foo": 42
			}
		}`,
	}

	for _, responseBody := range testCases {
		requestInfo := fmt.Sprintf("%q", responseBody)

		// we test with several variants of `expected` using different underlying
		// types that represent identical JSON payloads, but in different ways
		JSONObject{
			"data": JSONObject{
				"foo": 42,
				"bar": "hello world",
				"qux": []any{5, nil, 15},
			},
		}.AssertResponseBody(t, requestInfo, []byte(responseBody))

		// changing the type of `data` to map[string]any does not change anything at all;
		// using the JSONObject name on this level is mostly syntactic sugar to communicate intent
		JSONObject{
			"data": map[string]any{
				"foo": 42,
				"bar": "hello world",
				"qux": []any{5, nil, 15},
			},
		}.AssertResponseBody(t, requestInfo, []byte(responseBody))

		// this is using subtypes that our logic cannot recurse into
		// (map[opaqueString]any instead of map[string]any and []Option[int] instead of []any);
		// comparison will be less granular and only be able to fail on the level of the opaque subtype, but it will still work
		type opaqueString string
		JSONObject{
			"data": map[opaqueString]any{
				"foo": 42,
				"bar": "hello world",
				"qux": []Option[int]{Some(5), None[int](), Some(15)},
			},
		}.AssertResponseBody(t, requestInfo, []byte(responseBody))

		// this is using a specific struct type instead of a map[string]any, which results in a different serialization
		// (map[string]any serializes with keys sorted alphabetically, but structs serialize with keys sorted by field declaration order)
		JSONObject{
			"data": struct {
				Foo int           `json:"foo"`
				Bar string        `json:"bar"`
				Qux []Option[int] `json:"qux"`
			}{
				Foo: 42,
				Bar: "hello world",
				Qux: []Option[int]{Some(5), None[int](), Some(15)},
			},
		}.AssertResponseBody(t, requestInfo, []byte(responseBody))
	}
}

func TestJSONMatchCapturesFields(t *testing.T) {
	const (
		uuid1 = "2cff2c65-f775-4ed5-8f86-be0998b19781"
		uuid2 = "ce38aa5c-62ed-4367-a2f8-cbe2d73094a8"
	)
	responseBody := fmt.Sprintf(`{"objects":[{"id":"%s","tags":["foo"]},{"id":"%s","tags":["bar"]}]}`, uuid1, uuid2)

	// check that CaptureField() works as intended when contained within one of the supported container types
	type opaqueString string
	var (
		capturedUUID1 string
		capturedUUID2 string
		capturedTag1  opaqueString // check that capturing also works for custom types
	)
	JSONObject{
		"objects": []JSONObject{
			{
				"id":   CaptureField(&capturedUUID1),
				"tags": []string{"foo"},
			},
			{
				"id":   CaptureField(&capturedUUID2),
				"tags": []any{CaptureField(&capturedTag1)},
			},
		},
	}.AssertResponseBody(t, "test", []byte(responseBody))
	DeepEqual(t, "capturedUUID1", capturedUUID1, uuid1)
	DeepEqual(t, "capturedUUID2", capturedUUID2, uuid2)
	DeepEqual(t, "capturedTag1", capturedTag1, "bar")

	// check that CaptureField() does not work when contained within unsupported types
	//
	// This is a restriction that could be lifted in the future, but it would involve using advanced
	// reflection shenanigans that complicate the implementation. The fact that this example uses
	// somewhat contrived types to even be able to place a capture inside another structure shows that
	// this restriction ought not be too problematic in practice.
	capturedUUID1 = "unset"
	capturedUUID2 = "unset"
	capturedTag1 = "unset"
	expected := JSONObject{
		"objects": []struct {
			ID   any   `json:"id"`
			Tags []any `json:"tags"`
		}{
			{
				ID:   CaptureField(&capturedUUID1),
				Tags: []any{"foo"},
			},
			{
				ID:   CaptureField(&capturedUUID2),
				Tags: []any{CaptureField(&capturedTag1)},
			},
		},
	}
	expectDiffs(t, responseBody, expected, jsonObjectDiff{
		Message:      "type mismatch at .objects",
		ActualJSON:   fmt.Sprintf(`[{"id":"%s","tags":["foo"]},{"id":"%s","tags":["bar"]}]`, uuid1, uuid2),
		ExpectedJSON: `[{"id":"unset","tags":["foo"]},{"id":"unset","tags":["unset"]}]`,
	})
}

func TestJSONMatchFailsOnValueMismatch(t *testing.T) {
	responseBody := `{"users": [
		{"id":23,"name":"Alice","tags":[{"name":"admin"},{"name":"senior"}]},
		{"id":42,"name":"Bob","tags":[{"name":"support"}]}
	]}`
	expected := JSONObject{
		"users": []JSONObject{
			{
				"id":     23,
				"name":   "Alicia",                                // should be "Alice"
				"status": "fixing stuff",                          // unexpected field
				"tags":   []JSONObject{{"name": "administrator"}}, // name should be "admin"; second list entry missing
			},
			{
				// "id" field is missing
				"name": nil,                                                       // should be "Bob"
				"tags": []JSONObject{{"name": "support"}, {"name": "postmaster"}}, // unexpected list entry
			},
		},
	}
	expectDiffs(t, responseBody, expected,
		jsonObjectDiff{
			Message:      "value mismatch at .users[0].name",
			ActualJSON:   `"Alice"`,
			ExpectedJSON: `"Alicia"`,
		},
		jsonObjectDiff{
			Message:      "value mismatch at .users[0].tags[0].name",
			ActualJSON:   `"admin"`,
			ExpectedJSON: `"administrator"`,
		},
		jsonObjectDiff{
			Message:      "value mismatch at .users[0].tags[1]",
			ActualJSON:   `{"name":"senior"}`,
			ExpectedJSON: `<missing>`,
		},
		jsonObjectDiff{
			Message:      "value mismatch at .users[0].status",
			ActualJSON:   `<missing>`,
			ExpectedJSON: `"fixing stuff"`,
		},
		jsonObjectDiff{
			Message:      "value mismatch at .users[1].id",
			ActualJSON:   `42`,
			ExpectedJSON: `<missing>`,
		},
		jsonObjectDiff{
			Message:      "value mismatch at .users[1].name",
			ActualJSON:   `"Bob"`,
			ExpectedJSON: `null`,
		},
		jsonObjectDiff{
			Message:      "value mismatch at .users[1].tags[1]",
			ActualJSON:   `<missing>`,
			ExpectedJSON: `{"name":"postmaster"}`,
		},
	)
}

func TestJSONMatchFailsOnTypeMismatch(t *testing.T) {
	// several JSON values with incompatible JSON-level types, paired with their code-level representation
	testCases := []struct {
		JSON string
		Data any
	}{
		{JSON: `42`, Data: 42},
		{JSON: `{"value":42}`, Data: map[string]any{"value": 42}},
		{JSON: `[42]`, Data: []any{42}},
	}

	for idx1, tc1 := range testCases {
		responseBody := fmt.Sprintf(`{"payload":%s}`, tc1.JSON)
		for idx2, tc2 := range testCases {
			expected := JSONObject{"payload": tc2.Data}

			if idx1 == idx2 {
				// if we chose matching JSON and data types, then everything works as intended
				expected.AssertResponseBody(t, "test", []byte(responseBody))
			} else {
				// otherwise we expect a "type mismatch" error
				expectDiffs(t, responseBody, expected, jsonObjectDiff{
					Message:      "type mismatch at .payload",
					ActualJSON:   tc1.JSON,
					ExpectedJSON: string(must.Return(json.Marshal(tc2.Data))),
				})
			}
		}
	}
}

// TODO: marshaling and unmarshaling errors

func expectDiffs(t *testing.T, responseBody string, expected JSONObject, diffs ...jsonObjectDiff) {
	t.Helper()
	DeepEqual(t, "diffs", getDiffsForJSONObject(".", []byte(responseBody), expected), diffs)
}
