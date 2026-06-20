// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools_test

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"go.xyrillian.de/gg/assert"
	"go.xyrillian.de/gg/testcapture"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/must"
)

type mockUserInfo struct{}

func (mockUserInfo) AsInitiator(_ cadf.Host) cadf.Resource {
	return cadf.Resource{}
}

type mockTarget struct{}

func (mockTarget) Render() cadf.Resource {
	return cadf.Resource{}
}

type mockComplexTarget struct {
	Cores     int `json:"vcpu"`
	MemoryGiB int `json:"ram"`
}

func (t mockComplexTarget) Render() cadf.Resource {
	return cadf.Resource{
		Name:        "dummy-server",
		Attachments: []cadf.Attachment{must.Return(cadf.NewJSONAttachment("payload", t))},
	}
}

func TestMockAuditor(t *testing.T) {
	auditor := audittools.NewMockAuditor()

	// setup some dummy events
	makeEvent := func(status int, target audittools.Target) audittools.Event {
		return audittools.Event{
			Time: time.Now(),
			Request: &http.Request{
				Header: http.Header{},
				URL:    must.ReturnT(url.Parse("http://example.com"))(t),
			},
			User:       mockUserInfo{},
			ReasonCode: status,
			Action:     cadf.CreateAction,
			Target:     target,
		}
	}

	// no events recorded and also no events expected -> success
	result := testcapture.Capture(t.Context(), t.Name(), func(t assert.TestingTB) {
		auditor.ExpectEvents(t, nil...)
	})

	assert.Equal(t, len(result.Messages), 0)

	// no events recorded, but one event expected -> error
	result = testcapture.Capture(t.Context(), t.Name(), func(t assert.TestingTB) {
		auditor.ExpectEvents(t, makeEvent(http.StatusOK, mockTarget{}).ToCADF(cadf.Resource{}))
	})

	assert.Equal(t, len(result.Messages), 1)
	assert.ErrEqual(t, errors.New(result.Messages[0].Message), regexp.MustCompile(`value mismatch at /events/0: expected \{.*\}, but got <missing>`))

	// one event recorded, but no events expected -> error
	auditor.Record(makeEvent(http.StatusOK, mockTarget{}))
	result = testcapture.Capture(t.Context(), t.Name(), func(t assert.TestingTB) {
		auditor.ExpectEvents(t, nil...)
	})

	assert.Equal(t, len(result.Messages), 1)
	assert.ErrEqual(t, errors.New(result.Messages[0].Message), regexp.MustCompile(`value mismatch at /events/0: expected <missing>, but got \{.*\}`))

	// one event recorded, but a different event expected -> error
	auditor.Record(makeEvent(http.StatusOK, mockTarget{}))
	result = testcapture.Capture(t.Context(), t.Name(), func(t assert.TestingTB) {
		auditor.ExpectEvents(t, makeEvent(http.StatusNotFound, mockTarget{}).ToCADF(cadf.Resource{}))
	})

	assert.Equal(t, result.Messages, []testcapture.Message{
		testcapture.Log(`value mismatch at /events/0/outcome: expected "failure", but got "success"`),
		testcapture.Log(`value mismatch at /events/0/reason/reasonCode: expected "404", but got "200"`),
	})

	// one event recorded, and that same event expected -> success
	auditor.Record(makeEvent(http.StatusOK, mockTarget{}))
	result = testcapture.Capture(t.Context(), t.Name(), func(t assert.TestingTB) {
		auditor.ExpectEvents(t, makeEvent(http.StatusOK, mockTarget{}).ToCADF(cadf.Resource{}))
	})

	assert.Equal(t, len(result.Messages), 0)

	// check error reporting for diffs within JSON attachments
	auditor.Record(makeEvent(http.StatusOK, mockComplexTarget{Cores: 2, MemoryGiB: 4}))
	result = testcapture.Capture(t.Context(), t.Name(), func(t assert.TestingTB) {
		auditor.ExpectEvents(t, makeEvent(http.StatusOK, mockComplexTarget{Cores: 3, MemoryGiB: 6}).ToCADF(cadf.Resource{}))
	})

	assert.Equal(t, result.Messages, []testcapture.Message{
		testcapture.Log(`value mismatch at /events/0/target/attachments/0/content/ram: expected 6, but got 4`),
		testcapture.Log(`value mismatch at /events/0/target/attachments/0/content/vcpu: expected 3, but got 2`),
	})
}
