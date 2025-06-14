// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// jsonObjectDiff reports a difference between two JSON objects.
// It is used by the internal machinery for JSONObject.AssertResponseBody() in this file.
type jsonObjectDiff struct {
	Message      string
	ActualJSON   string
	ExpectedJSON string
}

// NOTE: Everything in this file is machinery for JSONObject.AssertResponseBody().
// The objective is to take a JSON-encoded message (`actual`) and compare it
// recursively against the `expected` structure inside a JSONObject instance.
//
// The original implementation of JSONObject.AssertResponseBody() serialized
// the `expected` data structure in a way that matched `actual` byte-for-byte,
// but with the introduction of CaptureField(), this is not feasible anymore.
// We need to traverse through `expected` to find and fill the captured fields.
//
// While recursing through the object, we maintain a `path` that identifies
// where we are in the callstack, e.g. when comparing
//
//   actual = { "foo": { "bar": [ 5, 23 ] } }
//   expected = { "foo": { "bar": [ 5, 42 ] } }
//
// we would generate a diff at Path = ".foo.bar[1]". Since diffs are usually rare,
// we only build those strings when we really need them. During recursion,
// `path` is maintained as a sequence of string fragments, most of which are
// constants to keep allocations to a minimum. WARNING: Because the `path`
// slice is heavily reused across nested function calls, it is not safe to
// store references to the `path` slice.

func getDiffsForJSONValue(path string, actual []byte, expected any) (diffs []jsonObjectDiff) {
	// specialized handling for relevant recursible or capturable types
	switch expected := expected.(type) {
	case map[string]any:
		return getDiffsForJSONObject(path, actual, expected)
	case JSONObject:
		return getDiffsForJSONObject(path, actual, expected)
	case []any:
		return getDiffsForJSONArray(path, actual, expected)
	case []JSONObject:
		downcasted := make([]any, len(expected))
		for idx, val := range expected {
			downcasted[idx] = val
		}
		return getDiffsForJSONArray(path, actual, downcasted)
	case capturedField:
		return getDiffsForJSONCapturedField(path, actual, expected)
	case nil:
		// this case needs to be handled separately because the code below
		// cannot deal with reflect.TypeOf(expected) returning nil
		if bytes.Equal(bytes.TrimSpace(actual), []byte("null")) {
			return nil
		} else {
			return []jsonObjectDiff{{
				Message:      "value mismatch at " + path,
				ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
				ExpectedJSON: "null",
			}}
		}
	}

	// generic handling for values or structures that we do not recurse into further:
	// check that `expected` encodes to JSON in an equivalent way to `actual`
	buf, err := json.Marshal(expected)
	if err != nil {
		return []jsonObjectDiff{{
			Message:      "type mismatch at " + path,
			ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
			ExpectedJSON: fmt.Sprintf("not marshalable to JSON, %%#v is %#v", expected),
		}}
	}
	expectedJSON := string(buf)

	// `actual` might have been serialized with a different key order or whitespace formatting than `expected`,
	// so we need to reserialize it through the type of `expected` to get the same serialization behavior
	actualJSON := string(bytes.TrimSpace(actual))
	data := reflect.New(reflect.TypeOf(expected)) // NOTE: data.Type() == reflect.PointerTo(reflect.TypeOf(expected))
	err = json.Unmarshal(actual, data.Interface())
	if err == nil {
		buf, err = json.Marshal(data.Interface())
		if err == nil {
			actualJSON = string(buf)
		}
	}

	if expectedJSON != actualJSON {
		msg := "value mismatch"
		if strings.HasPrefix(actualJSON, "[") || strings.HasPrefix(actualJSON, "{") {
			msg = "type mismatch"
		}
		return []jsonObjectDiff{{
			Message:      fmt.Sprintf("%s at %s", msg, path),
			ActualJSON:   actualJSON,
			ExpectedJSON: expectedJSON,
		}}
	}
	return nil
}

func getDiffsForJSONObject(path string, actual []byte, expected map[string]any) (diffs []jsonObjectDiff) {
	actualTrimmed := bytes.TrimSpace(actual)

	// can only compare field values if `actual` is a JSON object at all (i.e. not an array or a scalar value)
	if !bytes.HasPrefix(actualTrimmed, []byte("{")) {
		return []jsonObjectDiff{{
			Message:      "type mismatch at " + path,
			ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
			ExpectedJSON: serializeExpectedAsJSON(expected),
		}}
	}

	// try to parse `actual` as a JSON object
	var actualMap map[string]json.RawMessage
	err := json.Unmarshal(actual, &actualMap)
	if err != nil {
		return []jsonObjectDiff{{
			Message:      fmt.Sprintf("cannot unmarshal %s: %s", path, err.Error()),
			ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
			ExpectedJSON: serializeExpectedAsJSON(expected),
		}}
	}

	// recurse into all fields
	for _, key := range slices.Sorted(maps.Keys(actualMap)) {
		subpath := strings.Join([]string{strings.TrimSuffix(path, "."), key}, ".")
		expectedValue, exists := expected[key]
		if exists {
			diffs = append(diffs, getDiffsForJSONValue(subpath, []byte(actualMap[key]), expectedValue)...)
		} else {
			diffs = append(diffs, jsonObjectDiff{
				Message:      "value mismatch at " + subpath,
				ActualJSON:   strings.ToValidUTF8(string(actualMap[key]), "\uFFFD"),
				ExpectedJSON: "<missing>",
			})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(expected)) {
		subpath := strings.Join([]string{strings.TrimSuffix(path, "."), key}, ".")
		_, exists := actualMap[key]
		if !exists {
			diffs = append(diffs, jsonObjectDiff{
				Message:      "value mismatch at " + subpath,
				ActualJSON:   "<missing>",
				ExpectedJSON: serializeExpectedAsJSON(expected[key]),
			})
		}
	}

	return diffs
}

func getDiffsForJSONArray(path string, actual []byte, expected []any) (diffs []jsonObjectDiff) {
	actualTrimmed := bytes.TrimSpace(actual)

	// can only compare field values if `actual` is a JSON object at all (i.e. not an array or a scalar value)
	if !bytes.HasPrefix(actualTrimmed, []byte("[")) {
		return []jsonObjectDiff{{
			Message:      "type mismatch at " + path,
			ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
			ExpectedJSON: serializeExpectedAsJSON(expected),
		}}
	}

	// try to parse `actual` as a JSON array
	var actualList []json.RawMessage
	err := json.Unmarshal(actual, &actualList)
	if err != nil {
		return []jsonObjectDiff{{
			Message:      fmt.Sprintf("cannot unmarshal %s: %s", path, err.Error()),
			ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
			ExpectedJSON: serializeExpectedAsJSON(expected),
		}}
	}

	// recurse into all elements
	for idx := range max(len(actualList), len(expected)) {
		subpath := fmt.Sprintf("%s[%d]", path, idx)
		switch {
		case idx >= len(actualList):
			diffs = append(diffs, jsonObjectDiff{
				Message:      "value mismatch at " + subpath,
				ActualJSON:   "<missing>",
				ExpectedJSON: serializeExpectedAsJSON(expected[idx]),
			})
		case idx >= len(expected):
			diffs = append(diffs, jsonObjectDiff{
				Message:      "value mismatch at " + subpath,
				ActualJSON:   strings.ToValidUTF8(string(actualList[idx]), "\uFFFD"),
				ExpectedJSON: "<missing>",
			})
		default:
			diffs = append(diffs, getDiffsForJSONValue(subpath, actualList[idx], expected[idx])...)
		}
	}

	return diffs
}

func getDiffsForJSONCapturedField(path string, actual []byte, expected capturedField) []jsonObjectDiff {
	err := json.Unmarshal(actual, expected.PointerToTarget)
	if err != nil {
		return []jsonObjectDiff{{
			Message:      fmt.Sprintf("cannot unmarshal into captured field at %s: %s", path, err.Error()),
			ActualJSON:   strings.ToValidUTF8(string(actual), "\uFFFD"),
			ExpectedJSON: fmt.Sprintf("<capture slot of type %T>", expected.PointerToTarget),
		}}
	}
	return nil
}

func serializeExpectedAsJSON(expected any) string {
	buf, err := json.Marshal(expected)
	if err != nil {
		return fmt.Sprintf("not marshalable to JSON, %%#v is %#v", expected)
	}
	return string(buf)
}
