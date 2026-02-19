// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package easypg

import (
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestURLFrom(t *testing.T) {
	// replace os.Hostname() with a test double
	osHostname = func() (string, error) {
		return "testhostname", nil
	}

	// check a URL with everything set
	url, err := URLFrom(URLParts{
		HostName:          "localhost",
		Port:              "5432",
		UserName:          "foouser",
		Password:          "foopass",
		ConnectionOptions: "sslmode=disable",
		DatabaseName:      "foodb",
	})
	if err != nil {
		t.Fatal(err.Error())
	}
	expected := "postgres://foouser:foopass@localhost:5432/foodb?application_name=go-bits%40testhostname&sslmode=disable" //nolint:gosec // test fixture
	assert.Equal(t, url.String(), expected)

	// check a URL with optional parts omitted
	url, err = URLFrom(URLParts{
		HostName:     "localhost",
		UserName:     "foouser",
		DatabaseName: "foodb",
	})
	if err != nil {
		t.Fatal(err.Error())
	}
	expected = "postgres://foouser@localhost/foodb?application_name=go-bits%40testhostname"
	assert.Equal(t, url.String(), expected)
}
