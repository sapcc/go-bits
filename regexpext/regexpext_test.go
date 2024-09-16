/*******************************************************************************
*
* Copyright 2022 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package regexpext

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	yaml_v2 "gopkg.in/yaml.v2"
	yaml_v3 "gopkg.in/yaml.v3"

	"github.com/sapcc/go-bits/assert"
)

type testDocument struct {
	Text    string        `yaml:"text" json:"text"`
	Plain   PlainRegexp   `yaml:"plain" json:"plain"`
	Bounded BoundedRegexp `yaml:"bounded" json:"bounded"`
}

type fixture struct {
	JSON string
	YAML string
}

func pickYAML(f fixture) string { return f.YAML }
func pickJSON(f fixture) string { return f.JSON }

type protocol struct {
	ID        string
	Pick      func(fixture) string
	Marshal   func(any) ([]byte, error)
	Unmarshal func([]byte, any) error
}

var protocols = []protocol{
	{"json", pickJSON, json.Marshal, json.Unmarshal},
	{"yaml_v2", pickYAML, yaml_v2.Marshal, yaml_v2.Unmarshal},
	{"yaml_v3", pickYAML, yaml_v3.Marshal, yaml_v3.Unmarshal},
}

var (
	testGood = fixture{
		JSON: `{"text":"hello","plain":"hel*o","bounded":"hey?llo"}`,
		YAML: "text: hello\nplain: hel*o\nbounded: hey?llo\n",
	}
	testBad = fixture{
		JSON: `{"plain":"*hello","bounded":"hey?*llo"}`,
		YAML: "plain: '*hello'\nbounded: 'hey?*llo'\n",
	}
	testEmpty = fixture{
		JSON: `{"text":"","plain":"","bounded":""}`,
		YAML: "text: \"\"\nplain: \"\"\nbounded: \"\"\n",
	}
	testOmitEmpty = fixture{
		JSON: `{}`,
		YAML: "{}\n",
	}
	expectedError = "\"*hello\" is not a valid regexp: error parsing regexp: missing argument to repetition operator: `*`"
)

func TestUnmarshalGood(t *testing.T) {
	for _, proto := range protocols {
		t.Logf("testing proto = %s", proto.ID)

		var td testDocument
		err := proto.Unmarshal([]byte(proto.Pick(testGood)), &td)
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "td.Text", td.Text, "hello")

		rx, err := td.Plain.Regexp()
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "td.Plain", rx.String(), "hel*o")

		rx, err = td.Bounded.Regexp()
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "td.Bounded", rx.String(), "^(?:hey?llo)$")

		// test behavior of shortcut methods
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString("hello"), true)
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString("helko"), false)
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString("--hello--"), true)
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString("--hello"), true)
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString("hello--"), true)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString("hello"), true)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString("helko"), false)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString("--hello--"), false)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString("--hello"), false)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString("hello--"), false)

		assert.DeepEqual(t, "FindStringSubmatch result", td.Plain.FindStringSubmatch("hello"), []string{"hello"})
		assert.DeepEqual(t, "FindStringSubmatch result", td.Plain.FindStringSubmatch("helko"), []string(nil))
		assert.DeepEqual(t, "FindStringSubmatch result", td.Plain.FindStringSubmatch("--hello--"), []string{"hello"})
		assert.DeepEqual(t, "FindStringSubmatch result", td.Bounded.FindStringSubmatch("hello"), []string{"hello"})
		assert.DeepEqual(t, "FindStringSubmatch result", td.Bounded.FindStringSubmatch("helko"), []string(nil))
		assert.DeepEqual(t, "FindStringSubmatch result", td.Bounded.FindStringSubmatch("--hello--"), []string(nil))
	}
}

func TestUnmarshalBad(t *testing.T) {
	for _, proto := range protocols {
		t.Logf("testing proto = %s", proto.ID)

		var td testDocument
		err := proto.Unmarshal([]byte(proto.Pick(testBad)), &td)
		if err == nil {
			t.Fatal("expected Unmarshal() to fail, but succeeded")
		}
		assert.DeepEqual(t, "err.Error()", err.Error(), expectedError)
	}
}

func TestUnmarshalEmpty(t *testing.T) {
	for _, proto := range protocols {
		t.Logf("testing proto = %s", proto.ID)

		var td testDocument
		err := proto.Unmarshal([]byte(proto.Pick(testEmpty)), &td)
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "td.Text", td.Text, "")
		assert.DeepEqual(t, "td.Plain", td.Plain, PlainRegexp(""))
		assert.DeepEqual(t, "td.Bounded", td.Bounded, BoundedRegexp(""))

		// test behavior of shortcut methods
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString(""), true)
		assert.DeepEqual(t, "MatchString result", td.Plain.MatchString("foo"), true)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString(""), true)
		assert.DeepEqual(t, "MatchString result", td.Bounded.MatchString("foo"), false)

		assert.DeepEqual(t, "FindStringSubmatch result", td.Plain.FindStringSubmatch(""), []string{""})
		assert.DeepEqual(t, "FindStringSubmatch result", td.Plain.FindStringSubmatch("foo"), []string{""})
		assert.DeepEqual(t, "FindStringSubmatch result", td.Bounded.FindStringSubmatch(""), []string{""})
		assert.DeepEqual(t, "FindStringSubmatch result", td.Bounded.FindStringSubmatch("foo"), []string(nil))
	}
}

func TestMarshalGood(t *testing.T) {
	td := testDocument{
		Text:    "hello",
		Plain:   PlainRegexp("hel*o"),
		Bounded: BoundedRegexp("hey?llo"),
	}

	for _, proto := range protocols {
		t.Logf("testing proto = %s", proto.ID)

		buf, err := proto.Marshal(td)
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "Marshal", string(buf), proto.Pick(testGood))
	}
}

func TestMarshalEmpty(t *testing.T) {
	td := testDocument{
		Text:    "",
		Plain:   PlainRegexp(""),
		Bounded: BoundedRegexp(""),
	}

	for _, proto := range protocols {
		t.Logf("testing proto = %s", proto.ID)

		buf, err := proto.Marshal(td)
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "Marshal", string(buf), proto.Pick(testEmpty))
	}
}

func TestMarshalOmitEmpty(t *testing.T) {
	td := struct {
		Plain   PlainRegexp   `json:"plain,omitempty" yaml:"plain,omitempty"`
		Bounded BoundedRegexp `json:"bounded,omitempty" yaml:"bounded,omitempty"`
	}{}

	for _, proto := range protocols {
		t.Logf("testing proto = %s", proto.ID)

		buf, err := proto.Marshal(td)
		if err != nil {
			t.Fatal(err.Error())
		}
		assert.DeepEqual(t, "Marshal", string(buf), proto.Pick(testOmitEmpty))
	}
}

func TestIsLiteral(t *testing.T) {
	// To test the implementation of isLiteral(), we show it every single
	// printable ASCII character. Characters are not literals if
	// regexp.QuoteMeta() escapes them; isLiteral() must agree with this.
	for codepoint := uint8(32); codepoint < 127; codepoint++ {
		text := fmt.Sprintf("a%cb", rune(codepoint)) // e.g. "a b", "a*b", etc.
		expected := regexp.QuoteMeta(text) == text
		actual := isLiteral(text)
		assert.DeepEqual(t, fmt.Sprintf("verdict for %q", text), actual, expected)
	}
}
