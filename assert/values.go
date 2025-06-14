// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/sapcc/go-bits/osext"
)

// ByteData implements the HTTPRequestBody and HTTPResponseBody for plain bytestrings.
type ByteData []byte

// GetRequestBody implements the HTTPRequestBody interface.
func (b ByteData) GetRequestBody() (io.Reader, error) {
	return bytes.NewReader([]byte(b)), nil
}

func logDiff(t *testing.T, expected, actual string) {
	t.Helper()

	if osext.GetenvBool("GOBITS_PRETTY_DIFF") {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(fmt.Sprintf("%q\n", expected), fmt.Sprintf("%q\n", actual), false)
		t.Log(dmp.DiffPrettyText(diffs))
	} else {
		t.Logf("\texpected = %q\n", expected)
		t.Logf("\t  actual = %q\n", actual)
	}
}

// AssertResponseBody implements the HTTPResponseBody interface.
func (b ByteData) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	if !bytes.Equal([]byte(b), responseBody) {
		t.Error(requestInfo + ": got unexpected response body")
		logDiff(t, string(b), string(responseBody))
		return false
	}

	return true
}

// StringData implements HTTPRequestBody and HTTPResponseBody for plain strings.
type StringData string

// GetRequestBody implements the HTTPRequestBody interface.
func (s StringData) GetRequestBody() (io.Reader, error) {
	return strings.NewReader(string(s)), nil
}

// AssertResponseBody implements the HTTPResponseBody interface.
func (s StringData) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	responseStr := string(responseBody)
	if responseStr != string(s) {
		t.Errorf("%s: got unexpected response body", requestInfo)
		logDiff(t, string(s), responseStr)
		return false
	}

	return true
}

// JSONObject implements HTTPRequestBody and HTTPResponseBody for JSON objects.
type JSONObject map[string]any

// GetRequestBody implements the HTTPRequestBody interface.
func (o JSONObject) GetRequestBody() (io.Reader, error) {
	buf, err := json.Marshal(o)
	return bytes.NewReader(buf), err
}

// AssertResponseBody implements the HTTPResponseBody interface.
func (o JSONObject) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	diffs := getDiffsForJSONObject(".", responseBody, o)
	for _, diff := range diffs {
		t.Errorf("in responseBody of %s: %s", requestInfo, diff.Message)
		t.Logf("\texpected = %s\n", diff.ExpectedJSON)
		t.Logf("\t  actual = %s\n", diff.ActualJSON)
	}

	return len(diffs) == 0
}

// CaptureField can be slotted into a JSONObject instance to capture specific
// generated values from an HTTP response body without having to unmarshal
// the entire response into a structured type. For example:
//
//	// create an object and capture the generated UUID
//	var uuid string
//	assert.HTTPRequest {
//		Method: http.StatusPost,
//		Path:   "/v1/objects/new",
//		Body:   assert.JSONObject{
//			Value: "hello world",
//		},
//		ExpectStatus: http.StatusCreated,
//		ExpectBody:   assert.JSONObject{
//			UUID:  assert.CaptureField(&uuid),
//			Value: "hello world",
//		},
//	}.Check(t, handler)
//
//	// test deleting that object
//	assert.HTTPRequest {
//		Method:       http.StatusDelete,
//		Path:         "/v1/objects/" + uuid,
//		ExpectStatus: http.StatusNoContent,
//	}.Check(t, handler)
//
// Captured fields only work inside an assert.JSONObject or slice thereof, or
// within map[string]any or []any. If any level above the captured field is not
// one of these four types, the recursion will not be able to find and fill it.
func CaptureField[T any](target *T) any {
	// NOTE: The public interface is using generics because that allows enforcing
	// that `target` is passed as pointer. But the internal representation holds
	// `target` as `any` because not having type arguments on the capturedField
	// type makes it easier to reflect on that type.
	return capturedField{target}
}

type capturedField struct {
	PointerToTarget any
}

// MarshalJSON implements the json.Marshaler interface.
//
// This implementation ensures that `capturedField` looks like its payload
// when serialized for a "type mismatch" or "value mismatch" error message.
func (f capturedField) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.PointerToTarget)
}

// JSONFixtureFile implements HTTPResponseBody by locating the expected JSON
// response body in the given file.
type JSONFixtureFile string

// AssertResponseBody implements the HTTPResponseBody interface.
func (f JSONFixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	var buf bytes.Buffer
	err := json.Indent(&buf, responseBody, "", "  ")
	if err != nil {
		t.Logf("Response body: %s", responseBody)
		t.Fatal(err)
		return false
	}
	buf.WriteByte('\n')
	return FixtureFile(f).AssertResponseBody(t, requestInfo, buf.Bytes())
}

// FixtureFile implements HTTPResponseBody by locating the expected
// plain-text response body in the given file.
type FixtureFile string

// AssertResponseBody implements the HTTPResponseBody interface.
func (f FixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	// write actual content to file to make it easy to copy the computed result over
	// to the fixture path when a new test is added or an existing one is modified
	fixturePathAbs, err := filepath.Abs(string(f))
	if err != nil {
		t.Fatal(err)
		return false
	}
	actualPathAbs := fixturePathAbs + ".actual"
	err = os.WriteFile(actualPathAbs, responseBody, 0o666)
	if err != nil {
		t.Fatal(err)
		return false
	}

	cmd := exec.Command("diff", "-u", fixturePathAbs, actualPathAbs)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		t.Errorf("%s: body does not match: %s", requestInfo, err.Error())
	}

	return err == nil
}
