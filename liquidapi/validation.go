/*******************************************************************************
*
* Copyright 2025 SAP SE
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
	"fmt"
	"maps"
	"slices"

	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/errext"
)

func validateServiceInfo(srv liquid.ServiceInfo) error {
	var errs errext.ErrorSet
	for _, resName := range slices.Sorted(maps.Keys(srv.Resources)) {
		topology := srv.Resources[resName].Topology
		if !topology.IsValid() {
			errs.Addf("resource %q has invalid topology %q", resName, topology)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("received ServiceInfo is invalid: %s", errs.Join(", "))
	}
	return nil
}

// ValidateCapacityReport checks that the provided report is consistent with the provided request and ServiceInfo.
// Currently, this means that:
//
//   - The report.InfoVersion must match the value in info.Version.
//   - All resources declared in info.Resources with HasCapacity = true must be present (and no others).
//   - Each resource must report exactly for those AZs that its declared topology requires:
//     For FlatResourceTopology, only AvailabilityZoneAny is allowed.
//     For other topologies, all AZs in req.AllAZs must be present (and no others).
//   - All metrics families declared in info.CapacityMetricFamilies must be present (and no others).
//   - The number of labels on each metric must match the declared label set.
//
// Additional validations may be added in the future.
func ValidateCapacityReport(report liquid.ServiceCapacityReport, req liquid.ServiceCapacityRequest, info liquid.ServiceInfo) error {
	if report.InfoVersion != info.Version {
		return fmt.Errorf("capacity report is invalid: expected InfoVersion = %d, but got %d", info.Version, report.InfoVersion)
	}

	var errs errext.ErrorSet
	TODO()

	if len(errs) > 0 {
		return fmt.Errorf("received ServiceCapacityReport is invalid: %s", errs.Join(", "))
	}
	return nil
}

// TODO: ValidateUsageReport

func validateReportTopology[N ~string, V any](perAZReport map[liquid.AvailabilityZone]V, typeName string, name N, topology liquid.ResourceTopology, allAZs []liquid.AvailabilityZone) error {
	// this is specifically written to blow up when we add new topologies
	// and forget to update this function accordingly
	var isAZAware bool
	switch topology {
	case liquid.FlatResourceTopology:
		isAZAware = false
	case liquid.AZAwareResourceTopology, liquid.AZSeparatedResourceTopology:
		isAZAware = true
	default:
		if topology.IsValid() {
			return fmt.Errorf("%s %s has topology %q, but validateReportTopology() has not been updated to understand this value",
				typeName, name, topology)
		} else {
			// it should not be possible to reach this point,
			// callers should already have rejected invalid topology values
			panic(fmt.Sprintf("unreachable: topology = %q", topology))
		}
	}

	ok := true // until proven otherwise
	for az := range perAZReport {
		TODO()
	}

	if !ok {
		return fmt.Errorf("%s %q has PerAZ entries for %#v, which is invalid for topology %q",
			typeName, name, slices.Sorted(maps.Keys(perAZReport)), topology)
	}
	return nil
}
