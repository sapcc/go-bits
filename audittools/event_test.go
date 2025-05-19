// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import "github.com/sapcc/go-bits/gopherpolicy"

// check that *gopherpolicy.Token implements the UserInfo interface
var _ UserInfo = &gopherpolicy.Token{}
