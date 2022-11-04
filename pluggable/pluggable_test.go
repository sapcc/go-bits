/******************************************************************************
*
*  Copyright 2022 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

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

	//check that known plugins are constructed correctly
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

	//check that unknown plugin type ID yields a nil
	instance := r.Instantiate("something-else")
	if instance != nil {
		t.Errorf("expected a nil instance, but got %#v", instance)
	}
}
