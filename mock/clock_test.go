// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
)

func TestClock(t *testing.T) {
	// clock should start at zero
	c := NewClock()
	assert.DeepEqual(t, "Clock.Now as Unix timestamp", c.Now().Unix(), int64(0))

	// clock should not advance on its own
	assert.DeepEqual(t, "Clock.Now as Unix timestamp", c.Now().Unix(), int64(0))

	// clock should advance when asked to
	c.StepBy(5 * time.Minute)
	assert.DeepEqual(t, "Clock.Now as Unix timestamp", c.Now().Unix(), int64(300))

	// adding a listener should invoke the callback immediately
	currentTime := int64(-1)
	c.AddListener(func(t time.Time) {
		currentTime = t.Unix()
	})
	assert.DeepEqual(t, "Unix timestamp from callback", currentTime, int64(300))

	// advancing the clock should invoke the callback
	c.StepBy(time.Second)
	assert.DeepEqual(t, "Unix timestamp from callback", currentTime, int64(301))
}
