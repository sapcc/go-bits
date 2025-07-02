// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package internal

import (
	"github.com/gofrs/uuid/v5"

	"github.com/sapcc/go-bits/must"
)

// GenerateUUID generates an UUID based on random numbers (RFC 4122).
// Failure will result in program termination.
func GenerateUUID() string {
	return must.Return(uuid.NewV4()).String()
}
