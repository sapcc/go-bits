// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package pluggable

import "testing"

type testPlugin interface {
	Plugin
	ExampleData() int
}

type fooPlugin struct{}

func (p fooPlugin) PluginTypeID() string { return "foo" }
func (p fooPlugin) ExampleData() int     { return 42 }

type barPlugin struct{}

func (p barPlugin) PluginTypeID() string { return "bar" }
func (p barPlugin) ExampleData() int     { return 23 }

func TestRegistry(t *testing.T) {
	var r Registry[testPlugin]
	r.Add(func() testPlugin { return fooPlugin{} })
	r.Add(func() testPlugin { return barPlugin{} })

	testcases := []struct {
		PluginTypeID string
		ExampleData  int
	}{
		{"foo", 42},
		{"bar", 23},
	}

	// check that known plugins are constructed correctly
	for _, tc := range testcases {
		instance := r.Instantiate(tc.PluginTypeID)
		if instance == nil {
			t.Errorf("expected to be able to construct a %q plugin, but got instance = nil", tc.PluginTypeID)
		}
		if instance.PluginTypeID() != tc.PluginTypeID {
			t.Errorf("expected PluginTypeID = %q, but got %q", tc.PluginTypeID, instance.PluginTypeID())
		}
		if instance.ExampleData() != tc.ExampleData {
			t.Errorf("expected ExampleData = %d, but got %d", tc.ExampleData, instance.ExampleData())
		}
	}

	// check that unknown plugin type ID yields a nil
	instance := r.Instantiate("something-else")
	if instance != nil {
		t.Errorf("expected a nil instance, but got %#v", instance)
	}
}
