// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package secrets provides convenience functions for working with auth
// credentials.
package secrets

import (
	"github.com/sapcc/go-bits/osext"
)

// FromEnv holds either a plain text value or a key for the environment
// variable from which the value can be retrieved.
// The key has the format: `{ fromEnv: ENVIRONMENT_VARIABLE }`.
type FromEnv string

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (p *FromEnv) UnmarshalYAML(unmarshal func(any) error) error {
	// plain text value
	var plainTextInput string
	err := unmarshal(&plainTextInput)
	if err == nil {
		*p = FromEnv(plainTextInput)
		return nil
	}

	// retrieve value from the given environment variable key
	var envVariableInput struct {
		Key string `yaml:"fromEnv"`
	}
	err = unmarshal(&envVariableInput)
	if err != nil {
		return err
	}

	valFromEnv, err := osext.NeedGetenv(envVariableInput.Key)
	if err != nil {
		return err
	}

	*p = FromEnv(valFromEnv)

	return nil
}
