/******************************************************************************
*
*  Copyright 2023 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package promquery_test

import (
	"fmt"
	"os"
	"time"

	"github.com/prometheus/common/model"

	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/promquery"
)

type HostName string
type HostFilesystemMetrics struct {
	CapacityBytes int
	UsedBytes     int
}

var keyer = func(sample *model.Sample) HostName {
	return HostName(sample.Metric["hostname"])
}
var queries = []promquery.BulkQuery[HostName, HostFilesystemMetrics]{
	{
		Query:       "sum by (hostname) (filesystem_capacity_bytes)",
		Description: "filesystem capacity data",
		Keyer:       keyer,
		Filler: func(entry *HostFilesystemMetrics, sample *model.Sample) {
			entry.CapacityBytes = int(sample.Value)
		},
	},
	{
		Query:       "sum by (hostname) (filesystem_used_bytes)",
		Description: "filesystem usage data",
		Keyer:       keyer,
		Filler: func(entry *HostFilesystemMetrics, sample *model.Sample) {
			entry.UsedBytes = int(sample.Value)
		},
	},
}

func ExampleBulkQueryCache() {
	client := must.Return(promquery.ConfigFromEnv("PROMETHEUS").Connect())
	cache := promquery.NewBulkQueryCache(queries, 5*time.Minute, client)
	for _, arg := range os.Args[1:] {
		hostName := HostName(arg)
		entry := must.Return(cache.Get(hostName))
		usagePercent := 100 * float64(entry.UsedBytes) / float64(entry.CapacityBytes)
		fmt.Printf("disk usage on %s is %g%%\n", hostName, usagePercent)
	}
}
