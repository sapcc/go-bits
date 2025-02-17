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

package liquidapi

import (
	"testing"

	"github.com/sapcc/go-api-declarations/liquid"

	"github.com/sapcc/go-bits/assert"
)

// NOTE: Additional test coverage for DistributeFairly() is implicit as part of datamodel.ApplyComputedProjectQuota() in Limes.

func TestDistributeFairlyWithLargeNumbers(t *testing.T) {
	// This tests how DistributeFairly() deals with very large numbers
	// (as can occur e.g. for Swift capacity measured in bytes).
	// We used to have a crash here because of an overflowing uint64 multiplication.
	total := uint64(200000000000000)
	requested := map[uint16]uint64{
		401: total / 2,
		402: total / 2,
		403: total / 2,
		404: total / 2,
	}
	result := DistributeFairly(total, requested)
	assert.DeepEqual(t, "output of DistributeFairly", result, map[uint16]uint64{
		401: total / 4,
		402: total / 4,
		403: total / 4,
		404: total / 4,
	})

	// Even after having made the above testcases green, this one occurred in the wild.
	total = uint64(60)
	requested = map[uint16]uint64{
		401: 0x8000000000000000,
		402: 0x8000000000000000,
		403: 0x8000000000000000,
	}
	result = DistributeFairly(total, requested)
	assert.DeepEqual(t, "output of DistributeFairly", result, map[uint16]uint64{
		401: total / 3,
		402: total / 3,
		403: total / 3,
	})
}

func TestDistributeDemandFairlyWithJustBalance(t *testing.T) {
	// no demand, just balance
	total := uint64(400)
	demands := map[string]liquid.ResourceDemandInAZ{
		"foo": {},
		"bar": {},
	}
	balance := map[string]float64{
		"foo": 2,
		"bar": 1,
	}
	result := DistributeDemandFairly(total, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"foo": 267,
		"bar": 133,
	})
}

func TestDistributeDemandFairlyWithIncreasingCapacity(t *testing.T) {
	// This test uses the same demands and balance throughout, but capacity
	// increases over time to test how different types of demand are considered
	// in order.
	demands := map[string]liquid.ResourceDemandInAZ{
		"first": {
			Usage:              500,
			UnusedCommitments:  50,
			PendingCommitments: 10,
		},
		"second": {
			Usage:              300,
			UnusedCommitments:  200,
			PendingCommitments: 20,
		},
		"third": {
			Usage:              0,
			UnusedCommitments:  100,
			PendingCommitments: 70,
		},
	}
	balance := map[string]float64{
		"first":  0,
		"second": 1,
		"third":  1,
	}

	// usage cannot be covered
	result := DistributeDemandFairly(200, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  125,
		"second": 75,
		"third":  0,
	})

	// usage is exactly covered
	result = DistributeDemandFairly(800, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  500,
		"second": 300,
		"third":  0,
	})

	// unused commitments cannot be covered
	result = DistributeDemandFairly(900, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  514,
		"second": 357,
		"third":  29,
	})

	// unused commitments are exactly covered
	result = DistributeDemandFairly(1150, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  550,
		"second": 500,
		"third":  100,
	})

	// pending commitments cannot be covered
	result = DistributeDemandFairly(1160, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  551,
		"second": 502,
		"third":  107,
	})

	// unused commitments are exactly covered
	result = DistributeDemandFairly(1250, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  560,
		"second": 520,
		"third":  170,
	})

	// extra capacity is distributed according to balance
	result = DistributeDemandFairly(2250, demands, balance)
	assert.DeepEqual(t, "output of DistributeDemandFairly", result, map[string]uint64{
		"first":  560,
		"second": 1020,
		"third":  670,
	})
}
