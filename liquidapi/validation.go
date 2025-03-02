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

// TODO: move this into go-api-declarations/liquid (probably an unprecedented amount of active code for a declaration package, but useful to have there in order to have protocol extensions and their respective validation code in the same PR)
// TODO: check all "must" and "shall" wording in go-api-declarations/liquid for additional checks that we could do here

import (
	"fmt"
	"maps"
	"slices"

	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/errext"
)

// ValidateServiceInfo checks that the provided ServiceInfo is valid.
// Currently, this means that:
//
//   - Each resource is declared with a valid topology.
//
// Additional validations may be added in the future.
func ValidateServiceInfo(srv liquid.ServiceInfo) error {
	errs := validateServiceInfoImpl(srv)
	if len(errs) > 0 {
		return fmt.Errorf("received ServiceInfo is invalid: %s", errs.Join(", "))
	}
	return nil
}

func validateServiceInfoImpl(srv liquid.ServiceInfo) (errs errext.ErrorSet) {
	for resName, resInfo := range srv.Resources {
		if !resInfo.Topology.IsValid() {
			errs.Addf(".Resources[%q] has invalid topology %q", resName, resInfo.Topology)
		}
	}
	// TODO: RateUsageReport.PerAZ is documented as being based on topology, but RateInfo does not have a Topology field
	/*
		for rateName, rateInfo := range srv.Rates {
			if !rateInfo.Topology.IsValid() {
				errs.Addf(".Rates[%q] has invalid topology %q", rateName, rateInfo.Topology)
			}
		}
	*/

	return errs
}

// ValidateCapacityReport checks that the provided report is consistent with the provided request and ServiceInfo.
// Currently, this means that:
//
//   - The report.InfoVersion must match the value in info.Version.
//     (This is a hard error here. If the caller wants to be lenient about version mismatches, it may reload the ServiceInfo prior to validation.)
//   - All resources declared in info.Resources with HasCapacity = true must be present (and no others).
//   - Each resource must report exactly for those AZs that its declared topology requires:
//     For FlatResourceTopology, only AvailabilityZoneAny is allowed.
//     For other topologies, all AZs in req.AllAZs must be present (and possibly AvailabilityZoneUnknown, but no others).
//   - All metrics families declared in info.CapacityMetricFamilies must be present (and no others).
//   - The number of labels on each metric must match the declared label set.
//
// Additional validations may be added in the future.
func ValidateCapacityReport(report liquid.ServiceCapacityReport, req liquid.ServiceCapacityRequest, info liquid.ServiceInfo) error {
	errs := validateCapacityReportImpl(report, req, info)
	if len(errs) > 0 {
		return fmt.Errorf("received ServiceCapacityReport is invalid: %s", errs.Join(", "))
	}
	return nil
}

// This is the function that the unit tests call. An ErrorSet is easier to compare against fixtures than the final stringified error.
func validateCapacityReportImpl(report liquid.ServiceCapacityReport, req liquid.ServiceCapacityRequest, info liquid.ServiceInfo) (errs errext.ErrorSet) {
	if report.InfoVersion != info.Version {
		errs.Addf("received ServiceCapacityReport is invalid: expected .InfoVersion = %d, but got %d", info.Version, report.InfoVersion)
		// assume that all other errors would be aftereffects of the version mismatch, and skip finding them
		return errs
	}

	// validate metrics
	errs.Append(validateMetrics(report.Metrics, info.CapacityMetricFamilies, ".CapacityMetricFamilies"))

	// validate resource reports
	for resName, resInfo := range info.Resources {
		if resInfo.HasCapacity && !hasKey(report.Resources, resName) {
			errs.Addf("missing value for .Resources[%q] (resource was declared with HasCapacity = true)", resName)
		}
	}
	for resName, res := range report.Resources {
		resInfo, exists := info.Resources[resName]
		if !exists {
			errs.Addf("unexpected value for .Resources[%q] (resource was not declared)", resName)
			continue
		}
		if !resInfo.HasCapacity {
			errs.Addf("unexpected value for .Resources[%q] (resource was declared with HasCapacity = false)", resName)
			continue
		}
		errs.Add(validatePerAZAgainstTopology(res.PerAZ, resInfo.Topology, ".Resources", resName, req.AllAZs))
	}

	return errs
}

// ValidateUsageReport checks that the provided report is consistent with the provided request and ServiceInfo.
// Currently, this means that:
//
//   - The report.InfoVersion must match the value in info.Version.
//     (This is a hard error here. If the caller wants to be lenient about version mismatches, it may reload the ServiceInfo prior to validation.)
//   - All resources declared in info.Resources must be present (and no others).
//   - Each resource must report usage exactly for those AZs that its declared topology requires:
//     For FlatResourceTopology, only AvailabilityZoneAny is allowed.
//     For other topologies, all AZs in req.AllAZs must be present (and possibly AvailabilityZoneUnknown, but no others).
//   - All resources declared with HasQuota = true must report quota (and no others).
//   - Each resource reporting quota must report it in the way that its declared topology requires:
//     For AZSeparatedResourceTopology, quota must be reported only on the AZ level, and only for real AZs (not for AvailabilityZoneUnknown).
//     For all other topologies, quota must be reported only on the resource level.
//   - All rates declared in info.Rates must be present (and no others).
//   - Each rate must report usage exactly for those AZs that its declared topology requires:
//     For FlatRateTopology, only AvailabilityZoneAny is allowed.
//     For other topologies, all AZs in req.AllAZs must be present (and possibly AvailabilityZoneUnknown, but no others).
//   - All metrics families declared in info.UsageMetricFamilies must be present (and no others).
//   - The number of labels on each metric must match the declared label set.
//
// Additional validations may be added in the future.
func ValidateUsageReport(report liquid.ServiceUsageReport, req liquid.ServiceUsageRequest, info liquid.ServiceInfo) error {
	errs := validateUsageReportImpl(report, req, info)
	if len(errs) > 0 {
		return fmt.Errorf("received ServiceUsageReport is invalid: %s", errs.Join(", "))
	}
	return nil
}

// This is the function that the unit tests call. An ErrorSet is easier to compare against fixtures than the final stringified error.
func validateUsageReportImpl(report liquid.ServiceUsageReport, req liquid.ServiceUsageRequest, info liquid.ServiceInfo) (errs errext.ErrorSet) {
	if report.InfoVersion != info.Version {
		errs.Addf("received ServiceUsageReport is invalid: expected .InfoVersion = %d, but got %d", info.Version, report.InfoVersion)
		// assume that all other errors would be aftereffects of the version mismatch, and skip finding them
		return errs
	}

	// validate metrics
	errs.Append(validateMetrics(report.Metrics, info.UsageMetricFamilies, ".UsageMetricFamilies"))

	TODO("validate resource reports")
	TODO("validate rate reports")

	return errs
}

func validatePerAZAgainstTopology[N ~string, V any](perAZ map[liquid.AvailabilityZone]V, topology liquid.ResourceTopology, path string, name N, allAZs []liquid.AvailabilityZone) error {
	// this is specifically written to blow up when we add new topologies
	// and forget to update this function accordingly
	var isFlat bool
	switch topology {
	case liquid.FlatResourceTopology:
		isFlat = true
	case liquid.AZAwareResourceTopology, liquid.AZSeparatedResourceTopology:
		isFlat = false
	default:
		if topology.IsValid() {
			return fmt.Errorf("%s[%q] has topology %q, but validatePerAZAgainstTopology() has not been updated to understand this value",
				path, name, topology)
		} else {
			// it should not be possible to reach this point,
			// callers should already have rejected invalid topology values
			panic(fmt.Sprintf("unreachable: topology = %q", topology))
		}
	}

	valid := true // until proven otherwise
	if isFlat {
		// FlatResourceTopology requires "any" and allows nothing else
		if len(perAZ) != 1 {
			valid = false
		}
		for az := range perAZ {
			if az != liquid.AvailabilityZoneAny {
				valid = false
			}
		}
	} else {
		// other topologies require each AZ from `allAZs` to be present, and then optionally allow "unknown", but nothing else
		for az := range perAZ {
			if az != liquid.AvailabilityZoneUnknown && !slices.Contains(allAZs, az) {
				valid = false
			}
		}
		for _, az := range allAZs {
			if !hasKey(perAZ, az) {
				valid = false
			}
		}
	}

	if !valid {
		return fmt.Errorf("%s[%q].PerAZ has entries for %#v, which is invalid for topology %q",
			path, name, slices.Sorted(maps.Keys(perAZ)), topology)
	}
	return nil
}

func validateMetrics(allMetrics map[liquid.MetricName][]liquid.Metric, families map[liquid.MetricName]liquid.MetricFamilyInfo, path string) (errs errext.ErrorSet) {
	for familyName := range families {
		if !hasKey(allMetrics, familyName) {
			errs.Addf("missing value for .Metrics[%q] (declared in %s)", familyName, path)
		}
	}

	for familyName, metrics := range allMetrics {
		familyInfo, exists := families[familyName]
		if !exists {
			errs.Addf("unexpected value for .Metrics[%q] (not declared in %s)", familyName, path)
			continue
		}
		for idx, metric := range metrics {
			if len(metric.LabelValues) != len(familyInfo.LabelKeys) {
				errs.Addf("malformed value for .Metrics[%q][%d].LabelValues (expected %d, but got %d entries)",
					familyName, idx, len(familyInfo.LabelKeys), len(metric.LabelValues))
			}
		}
	}

	return errs
}

func hasKey[M ~map[K]V, K comparable, V any](m M, key K) bool {
	_, exists := m[key]
	return exists
}
