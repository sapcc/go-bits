// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpext

import (
	"context"
	"testing"
	"time"
)

func TestListenAndServeContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	err := ListenAndServeContext(ctx, "localhost:8080", nil)
	if err != nil {
		t.Errorf("expected a nil error, got: %s", err.Error())
	}
	cancel()
}
