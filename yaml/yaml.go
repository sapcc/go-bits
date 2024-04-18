/*******************************************************************************
*
* Copyright 2024 SAP SE
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

// This package is just an aid to migrate from gopkg.in/yaml.v2 to gopkg.in/yaml.v2
// It provides a helper to parse a given yaml document with both major versions and log if there is a difference.
package yaml

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	yaml_v2 "gopkg.in/yaml.v2"
	yaml_v3 "gopkg.in/yaml.v3"

	"github.com/sapcc/go-bits/logg"
)

func Marshal[T any](in T) ([]byte, error) {
	out, err := yaml_v2.Marshal(in)
	if err != nil {
		return nil, err
	}

	outV3 := new(bytes.Buffer)
	dec := yaml_v3.NewEncoder(outV3)
	dec.SetIndent(2)
	err = dec.Encode(in)
	if err != nil {
		logg.Error("gopkg.in/yaml.v3.Marshal() returned an error: %w,", err)
	}

	if !reflect.DeepEqual(out, outV3.Bytes()) {
		logg.Error("gopkg.in/yaml.v2 and gopkg.in/yaml.v3 Marshal() are not equal. Turn on debug logging to see the difference.")
		if logg.ShowDebug {
			fmt.Print(cmp.Diff(out, outV3.Bytes()))
		}
	}

	return out, nil
}

func Unmarshal[T any](in []byte, out *T) error {
	err := yaml_v2.Unmarshal(in, out)
	if err != nil {
		return err
	}

	var outV3 *T
	err = yaml_v3.Unmarshal(in, outV3)
	if err != nil {
		logg.Error("gopkg.in/yaml.v3.Unmarshal() returned an error: %w,", err)
	}

	if !reflect.DeepEqual(out, outV3) {
		logg.Error("gopkg.in/yaml.v2 and gopkg.in/yaml.v3 Unmarshal() are not equal. Turn on debug logging to see the difference.")
		if logg.ShowDebug {
			fmt.Print(cmp.Diff(out, outV3))
		}
	}

	return nil
}

func UnmarshalStrict[T any](in []byte, out *T) error {
	err := yaml_v2.UnmarshalStrict(in, out)
	if err != nil {
		return err
	}

	var outV3 *T
	dec := yaml_v3.NewDecoder(bytes.NewReader(in))
	dec.KnownFields(true)
	err = dec.Decode(&outV3)
	if err != nil {
		logg.Error("gopkg.in/yaml.v3.UnmarshalStrict() returned an error: %w,", err)
	}

	if !reflect.DeepEqual(out, outV3) {
		logg.Error("gopkg.in/yaml.v2 and gopkg.in/yaml.v3 UnmarshalStrict() are not equal. Turn on debug logging to see the difference.")
		if logg.ShowDebug {
			fmt.Print(cmp.Diff(out, outV3))
		}
	}

	return nil
}
