// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquidapi

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sapcc/go-api-declarations/liquid"

	"github.com/sapcc/go-bits/logg"
)

func TestLiquidOptionType(t *testing.T) {
	liquidOptionTypes := make(map[reflect.Type]struct{})
	getOptionTypesRecursively(reflect.ValueOf(liquid.ServiceInfo{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.ServiceUsageRequest{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.ServiceUsageReport{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.ServiceCapacityRequest{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.ServiceCapacityReport{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.ServiceQuotaRequest{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.CommitmentChangeRequest{}).Type(), liquidOptionTypes)
	getOptionTypesRecursively(reflect.ValueOf(liquid.CommitmentChangeResponse{}).Type(), liquidOptionTypes)
	registeredOptionTypes := ForeachOptionTypeInLIQUID(getOptionType)
	if slices.Contains(registeredOptionTypes, nil) {
		t.Error("ForeachOptionTypeInLIQUID contains values that are not of type github.com/majewsky/gg/option.Option")
	}
	for optionType := range liquidOptionTypes {
		if !slices.Contains(registeredOptionTypes, optionType) {
			t.Errorf("compare option missing for type Option[%s]", optionType)
		}
	}
}

func getOptionTypesRecursively(t reflect.Type, optionTypes map[reflect.Type]struct{}) {
	switch t.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Array, reflect.Map:
		getOptionTypesRecursively(t.Elem(), optionTypes)
	case reflect.Struct:
		if strings.HasPrefix(t.Name(), "Option") {
			field, ok := t.FieldByName("value")
			if !ok {
				logg.Error(fmt.Sprintf("expected type majewsky/gg/option.Option with field 'value' but got %q", t.Name()))
				return
			}
			optionTypes[field.Type] = struct{}{}
			getOptionTypesRecursively(field.Type, optionTypes)
		} else {
			for idx := range t.NumField() {
				f := t.Field(idx)
				getOptionTypesRecursively(f.Type, optionTypes)
			}
		}
	}
}

func getOptionType(noneValues ...any) reflect.Type {
	if len(noneValues) != 1 || reflect.TypeOf(noneValues[0]).Kind() != reflect.Struct {
		return nil
	}
	field, ok := reflect.TypeOf(noneValues[0]).FieldByName("value")
	if !ok {
		return nil
	}
	return field.Type
}
