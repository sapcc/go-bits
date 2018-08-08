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

//Package backoff contains a helper function that creates a retry loop with an
//exponential backoff.
package backoff

import "time"

//Retry takes a function (action) that returns an error, and two int64 values (x, y) as
//parameters and creates a retry loop with an exponential backoff such that on failure (error return),
//the action is called again after x seconds and this is incremented by a factor of 2 until y minutes
//then it is keeps on repeating after y minutes till action succeeds (no error).
func Retry(action func() error, x, y time.Duration) {
	duration := time.Second
	for {
		err := action()
		if err != nil {
			duration *= x
			if duration > y*time.Minute {
				duration = y * time.Minute
			}
			time.Sleep(duration)
			continue
		}
		break
	}
}
