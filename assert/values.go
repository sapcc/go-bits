/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package assert

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

//StringData provides an implementation of HTTPRequestBody for strings.
type StringData string

//GetRequestBody implements the HTTPRequestBody interface.
func (s StringData) GetRequestBody() (io.Reader, error) {
	return strings.NewReader(string(s)), nil
}

//JSONObject provides an implementation of HTTPRequestBody for JSON objects.
type JSONObject map[string]interface{}

//GetRequestBody implements the HTTPRequestBody interface.
func (o JSONObject) GetRequestBody() (io.Reader, error) {
	buf, err := json.Marshal(o)
	return bytes.NewReader(buf), err
}
