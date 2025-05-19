// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package promquery_test

import (
	"context"
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
		entry := must.Return(cache.Get(context.Background(), hostName))
		usagePercent := 100 * float64(entry.UsedBytes) / float64(entry.CapacityBytes)
		fmt.Printf("disk usage on %s is %g%%\n", hostName, usagePercent)
	}
}
